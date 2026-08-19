package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/jobs"
	"campaign-platform/internal/metacloud"
)

type preflightFailureGateway struct {
	err       error
	sendCalls int
}

func (g *preflightFailureGateway) Preflight(context.Context, GatewayRequest) error { return g.err }
func (g *preflightFailureGateway) Send(context.Context, GatewayRequest) (GatewayResult, error) {
	g.sendCalls++
	return GatewayResult{}, errors.New("send must not run after permanent preflight failure")
}
func (g *preflightFailureGateway) SendPrepared(context.Context, GatewayRequest) (GatewayResult, error) {
	g.sendCalls++
	return GatewayResult{}, errors.New("prepared send must not run after permanent preflight failure")
}

type submittingAdvanceRepository struct {
	delivery.Repository
	onSubmitting func()
}

func (r *submittingAdvanceRepository) ApplyEvent(ctx context.Context, id string, event delivery.Event) (delivery.Recipient, bool, error) {
	if event.Type == delivery.EventSubmitting && r.onSubmitting != nil {
		r.onSubmitting()
	}
	return r.Repository.ApplyEvent(ctx, id, event)
}

func TestPermanentPreflightFailureTerminalizesWithoutSubmitting(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	gateway := &preflightFailureGateway{err: GatewayError{Code: "LOCAL_REQUEST_INVALID", Safety: FailurePermanent, Err: errors.New("invalid governed request")}}
	h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligible(), Gateway: gateway}
	if err := h.Handle(context.Background(), jobFixture()); err != nil {
		t.Fatalf("permanent preflight should be durably terminalized and acknowledged, got %v", err)
	}
	value, err := ledger.Get(context.Background(), "recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != delivery.StatusFailedPermanent || value.AttemptCount != 0 || gateway.sendCalls != 0 {
		t.Fatalf("permanent preflight status=%s attempts=%d sendCalls=%d", value.Status, value.AttemptCount, gateway.sendCalls)
	}
}

func TestPreparedMetaSendDoesNotRecheckRetryableWindowAfterSubmitting(t *testing.T) {
	now := time.Date(2026, 8, 15, 15, 0, 0, 0, time.UTC)
	deadline := now.Add(30 * time.Second)
	gatewayClock := now
	baseRepo := delivery.NewMemoryRepository(recipientFixture())
	repo := &submittingAdvanceRepository{Repository: baseRepo, onSubmitting: func() { gatewayClock = deadline.Add(time.Second) }}
	ledger := delivery.NewService(repo)
	client := &metaClientStub{result: metacloud.MessageResult{MessageID: "wamid.prepared-boundary"}}
	loader := loaderFunc(func(context.Context, delivery.Recipient) (Material, error) {
		return Material{
			Provider: "META", Engine: "CLOUD_API", RouteReference: "campaign-1:recipient-1",
			MetaSenderID: "sender-1", MetaSenderVersion: 1, MetaCredentialKey: "cred-1",
			MetaGraphAPIVersion: "v23.0", MetaPhoneNumberID: "phone-1",
			MetaRepresentation: MetaRepresentationFreeForm, MetaFreeFormEligibleUntil: deadline,
			RecipientE164: "+2348012345678", MessageType: "text", Body: "boundary-authorised free form",
		}, nil
	})
	h := &Handler{
		Ledger: ledger, Materials: loader, Eligibility: eligible(), Clock: func() time.Time { return now },
		Gateway: &TransportRouter{Meta: &MetaGateway{Client: client, Clock: func() time.Time { return gatewayClock }}},
	}
	if err := h.Handle(context.Background(), jobFixture()); err != nil {
		var retry jobs.RetryableError
		if errors.As(err, &retry) {
			t.Fatalf("readiness was rechecked after SUBMITTING: %v", err)
		}
		t.Fatal(err)
	}
	value, err := ledger.Get(context.Background(), "recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != delivery.StatusGatewayAccepted || value.AttemptCount != 1 || client.calls != 1 {
		t.Fatalf("prepared submission status=%s attempts=%d clientCalls=%d", value.Status, value.AttemptCount, client.calls)
	}
}
