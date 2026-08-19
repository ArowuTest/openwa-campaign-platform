package dispatch

import (
	"context"
	"errors"
	"testing"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/jobs"
)

func TestUnavailableConfiguredTransportIsRetryableBeforeSubmitting(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	loader := loaderFunc(func(context.Context, delivery.Recipient) (Material, error) {
		return Material{
			Provider: "META", Engine: "CLOUD_API", RouteReference: "campaign-1:recipient-1",
			MetaSenderID: "sender-1", MetaSenderVersion: 1, MetaCredentialKey: "cred-1",
			MetaGraphAPIVersion: "v23.0", MetaPhoneNumberID: "phone-1", MetaRepresentation: MetaRepresentationTemplate,
			MetaTemplateName: "approved_template", MetaTemplateLanguage: "en",
			RecipientE164: "+2348012345678", MessageType: "text", Body: "hello",
		}, nil
	})
	h := &Handler{Ledger: ledger, Materials: loader, Eligibility: eligible(), Gateway: &TransportRouter{OpenWA: &gatewayStub{}}}
	var retry jobs.RetryableError
	if err := h.Handle(context.Background(), jobFixture()); !errors.As(err, &retry) {
		t.Fatalf("missing Meta transport must be retryable before submit, got %T %v", err, err)
	}
	value, err := ledger.Get(context.Background(), "recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != delivery.StatusAuthorised {
		t.Fatalf("missing Meta transport mutated recipient to %s", value.Status)
	}
	if value.AttemptCount != 0 {
		t.Fatalf("missing Meta transport incremented attempt count to %d", value.AttemptCount)
	}
}
