package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/jobs"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/metacloud"
)

func TestInvalidMetaRepresentationFailsBeforeSubmitting(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	client := &metaClientStub{}
	loader := loaderFunc(func(context.Context, delivery.Recipient) (Material, error) {
		return Material{
			Provider: "META", Engine: "CLOUD_API", RouteReference: "campaign-1:recipient-1",
			MetaSenderID: "sender-1", MetaSenderVersion: 1, MetaCredentialKey: "cred-1",
			MetaGraphAPIVersion: "v23.0", MetaPhoneNumberID: "phone-1",
			MetaRepresentation: MetaRepresentationTemplate, MetaTemplateName: "template-1", MetaTemplateLanguage: "en_US",
			MetaComponents: []MetaTemplateComponent{{Type: "BODY", Index: 1, Parameters: []MetaTemplateParameter{{Type: "TEXT", Text: "Ada"}}}},
			RecipientE164:  "+2348012345678", MessageType: "text", Body: "hello",
		}, nil
	})
	h := &Handler{
		Ledger: ledger, Materials: loader, Eligibility: eligible(),
		Gateway: &TransportRouter{Meta: &MetaGateway{Client: client}},
	}
	if err := h.Handle(context.Background(), jobFixture()); err != nil {
		t.Fatalf("permanent preflight should be durably terminalized and acknowledged: %v", err)
	}
	value, getErr := ledger.Get(context.Background(), "recipient-1")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if value.Status != delivery.StatusFailedPermanent || value.AttemptCount != 0 || client.calls != 0 {
		t.Fatalf("invalid Meta representation was not terminalized before submit: status=%s attempts=%d clientCalls=%d", value.Status, value.AttemptCount, client.calls)
	}
}

func TestHandlerAcceptsValidMetaFreeFormMaterial(t *testing.T) {
	now := time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC)
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	client := &metaClientStub{result: metacloud.MessageResult{MessageID: "wamid.handler.free"}}
	loader := loaderFunc(func(context.Context, delivery.Recipient) (Material, error) {
		return Material{
			Provider: "META", Engine: "CLOUD_API", RouteReference: "campaign-1:recipient-1",
			MetaSenderID: "sender-1", MetaSenderVersion: 1, MetaCredentialKey: "cred-1",
			MetaGraphAPIVersion: "v23.0", MetaPhoneNumberID: "phone-1", MetaRepresentation: MetaRepresentationFreeForm,
			MetaFreeFormEligibleUntil: now.Add(time.Hour),
			RecipientE164:             "+2348012345678", MessageType: "text", Body: "hello free form",
		}, nil
	})
	h := &Handler{Ledger: ledger, Materials: loader, Eligibility: eligible(), Clock: func() time.Time { return now }, Gateway: &TransportRouter{Meta: &MetaGateway{Client: client, Clock: func() time.Time { return now }}}}
	if err := h.Handle(context.Background(), jobFixture()); err != nil {
		t.Fatal(err)
	}
	value, err := ledger.Get(context.Background(), "recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != delivery.StatusGatewayAccepted || value.ProviderMessageID != "wamid.handler.free" || client.calls != 1 {
		t.Fatalf("valid Meta free-form did not traverse handler: status=%s providerID=%s calls=%d", value.Status, value.ProviderMessageID, client.calls)
	}
}

func TestTransportRouterDelegatesMetaRequestPreflight(t *testing.T) {
	client := &metaClientStub{}
	request := GatewayRequest{
		Provider: "META", Engine: "CLOUD_API",
		MetaSenderID: "sender-1", MetaSenderVersion: 1, MetaCredentialKey: "cred-1",
		MetaGraphAPIVersion: "v23.0", MetaPhoneNumberID: "phone-1",
		MetaRepresentation: MetaRepresentationTemplate,
		RecipientE164:      "+2348012345678", MessageType: "text", Body: "hello",
	}
	err := (&TransportRouter{Meta: &MetaGateway{Client: client}}).Preflight(context.Background(), request)
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailurePermanent {
		t.Fatalf("invalid Meta request was not rejected by delegated preflight: %T %v", err, err)
	}
	if client.calls != 0 {
		t.Fatalf("preflight reached Meta client %d times", client.calls)
	}
}

type pacingAdvanceFunc func()

func (f pacingAdvanceFunc) Wait(context.Context, delivery.Recipient, Material, time.Time) error {
	f()
	return nil
}

func TestMetaFreeFormWindowExpiringDuringPacingFailsBeforeSubmitting(t *testing.T) {
	now := time.Date(2026, 8, 15, 14, 30, 0, 0, time.UTC)
	eligibleUntil := now.Add(30 * time.Second)
	gatewayClock := now
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	client := &metaClientStub{result: metacloud.MessageResult{MessageID: "wamid.must-not-send"}}
	loader := loaderFunc(func(context.Context, delivery.Recipient) (Material, error) {
		return Material{
			Provider: "META", Engine: "CLOUD_API", RouteReference: "campaign-1:recipient-1",
			MetaSenderID: "sender-1", MetaSenderVersion: 1, MetaCredentialKey: "cred-1",
			MetaGraphAPIVersion: "v23.0", MetaPhoneNumberID: "phone-1",
			MetaRepresentation: MetaRepresentationFreeForm, MetaFreeFormEligibleUntil: eligibleUntil,
			RecipientE164: "+2348012345678", MessageType: "text", Body: "window-bound free form",
		}, nil
	})
	h := &Handler{
		Ledger: ledger, Materials: loader, Eligibility: eligible(), Clock: func() time.Time { return now },
		Pacing:  pacingAdvanceFunc(func() { gatewayClock = eligibleUntil.Add(time.Second) }),
		Gateway: &TransportRouter{Meta: &MetaGateway{Client: client, Clock: func() time.Time { return gatewayClock }}},
	}
	var retry jobs.RetryableError
	if err := h.Handle(context.Background(), jobFixture()); !errors.As(err, &retry) {
		t.Fatalf("expired free-form window must be retryable before submit, got %T %v", err, err)
	}
	value, err := ledger.Get(context.Background(), "recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != delivery.StatusAuthorised || value.AttemptCount != 0 || client.calls != 0 {
		t.Fatalf("expired free-form crossed submit boundary: status=%s attempts=%d clientCalls=%d", value.Status, value.AttemptCount, client.calls)
	}
}
