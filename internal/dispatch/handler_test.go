package dispatch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/jobs"
)

type loaderFunc func(context.Context, delivery.Recipient) (Material, error)

func (f loaderFunc) Load(c context.Context, r delivery.Recipient) (Material, error) { return f(c, r) }

type eligibilityFunc func(context.Context, delivery.Recipient, time.Time) (EligibilityDecision, error)

func (f eligibilityFunc) Check(c context.Context, r delivery.Recipient, t time.Time) (EligibilityDecision, error) {
	return f(c, r, t)
}

type gatewayStub struct {
	mu     sync.Mutex
	calls  int
	result GatewayResult
	err    error
}

func (g *gatewayStub) Send(context.Context, GatewayRequest) (GatewayResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	return g.result, g.err
}

func recipientFixture() delivery.Recipient {
	return delivery.Recipient{ID: "recipient-1", CampaignID: "campaign-1", ContactID: "contact-1", MessageVersionID: "message-1", IdempotencyKey: "idem-1234567890123456", Status: delivery.StatusAuthorised, UpdatedAt: time.Now().UTC()}
}
func jobFixture() jobs.Job {
	job, _ := jobs.NewJob(jobs.EnqueueInput{Type: JobType, DedupKey: "dispatch:1", Payload: JobPayload{CampaignRecipientID: "recipient-1"}}, time.Now().UTC())
	job.AttemptCount = 1
	return job
}
func validLoader() loaderFunc {
	return func(context.Context, delivery.Recipient) (Material, error) {
		return Material{GatewayPoolID: "pool-1", SessionID: "session-1", RecipientE164: "+2348012345678", MessageType: "text", Body: "hello"}, nil
	}
}
func eligible() eligibilityFunc {
	return func(context.Context, delivery.Recipient, time.Time) (EligibilityDecision, error) {
		return EligibilityDecision{Eligible: true}, nil
	}
}

func TestSuccessfulDispatchIsIdempotentAcrossJobReplay(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	gateway := &gatewayStub{result: GatewayResult{Accepted: true, ProviderMessageID: "provider-1", AcceptedAt: time.Now().UTC()}}
	h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligible(), Gateway: gateway}
	job := jobFixture()
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if gateway.calls != 1 {
		t.Fatalf("gateway called %d times", gateway.calls)
	}
	value, _ := ledger.Get(context.Background(), "recipient-1")
	if value.Status != delivery.StatusGatewayAccepted || value.AttemptCount != 1 {
		t.Fatalf("unexpected ledger: %#v", value)
	}
}

func TestUnknownOutcomeIsNotAutomaticallyRetried(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	gateway := &gatewayStub{err: errors.New("connection lost after write")}
	h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligible(), Gateway: gateway}
	if err := h.Handle(context.Background(), jobFixture()); err != nil {
		t.Fatalf("unknown should be recorded, not returned for retry: %v", err)
	}
	value, _ := ledger.Get(context.Background(), "recipient-1")
	if value.Status != delivery.StatusUnknown {
		t.Fatalf("expected UNKNOWN, got %s", value.Status)
	}
}

func TestFinalWithdrawalSuppressesBeforeGateway(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	gateway := &gatewayStub{}
	h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligibilityFunc(func(context.Context, delivery.Recipient, time.Time) (EligibilityDecision, error) {
		return EligibilityDecision{Eligible: false, Reason: "WITHDRAWN"}, nil
	}), Gateway: gateway}
	if err := h.Handle(context.Background(), jobFixture()); err != nil {
		t.Fatal(err)
	}
	if gateway.calls != 0 {
		t.Fatal("gateway must not be called")
	}
	value, _ := ledger.Get(context.Background(), "recipient-1")
	if value.Status != delivery.StatusSuppressedBeforeSend {
		t.Fatalf("got %s", value.Status)
	}
}

func TestExplicitSafeFailureReturnsRetryable(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	gateway := &gatewayStub{err: GatewayError{Code: "CONNECT_FAILED", Safety: FailureSafeToRetry, Err: errors.New("failed before write")}}
	h := &Handler{Ledger: delivery.NewService(repo), Materials: validLoader(), Eligibility: eligible(), Gateway: gateway}
	var retry jobs.RetryableError
	if err := h.Handle(context.Background(), jobFixture()); !errors.As(err, &retry) {
		t.Fatalf("expected retryable error, got %v", err)
	}
}

func TestHandlerDoesNotRetryPermanentMaterialFailure(t *testing.T) {
	now := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "r-permanent-material", CampaignID: "c", ContactID: "ct", MessageVersionID: "m", IdempotencyKey: "idem-1234567890123456", Status: delivery.StatusQueued, UpdatedAt: now}
	ledger := delivery.NewService(delivery.NewMemoryRepository(recipient))
	handler := &Handler{
		Ledger:      ledger,
		Eligibility: eligible(),
		Materials: loaderFunc(func(context.Context, delivery.Recipient) (Material, error) {
			return Material{}, PermanentMaterialError{Err: errors.New("unsupported approved message")}
		}),
		Gateway: &gatewayStub{}, Clock: func() time.Time { return now },
	}
	job, err := jobs.NewJob(jobs.EnqueueInput{Type: JobType, DedupKey: "dispatch:permanent", Payload: JobPayload{CampaignRecipientID: recipient.ID}}, now)
	if err != nil {
		t.Fatal(err)
	}
	job.AttemptCount = 1
	err = handler.Handle(context.Background(), job)
	var retry jobs.RetryableError
	if err == nil || errors.As(err, &retry) {
		t.Fatalf("permanent material error must not be retried: %T %v", err, err)
	}
}
