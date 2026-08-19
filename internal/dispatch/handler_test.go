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
func (g *gatewayStub) Preflight(context.Context, GatewayRequest) error { return nil }
func (g *gatewayStub) SendPrepared(ctx context.Context, request GatewayRequest) (GatewayResult, error) {
	return g.Send(ctx, request)
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
		now := time.Now().UTC()
		return Material{Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", GatewayPoolID: "pool-1", GatewayPoolVersion: 1, GatewayAdapterVersion: "0.13.0", GatewayNodeID: "node-1", GatewayNodeVersion: 1, SessionID: "session-1", SessionLeaseVersion: 1, SessionConfigurationVersion: 1, AuthorityExpiresAt: now.Add(10 * time.Minute), RouteReference: "campaign-1:recipient-1", RecipientE164: "+2348012345678", MessageType: "text", Body: "hello"}, nil
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
	if err != nil {
		t.Fatalf("permanent material failure should terminalize the ledger without retry: %v", err)
	}
	value, getErr := ledger.Get(context.Background(), recipient.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if value.Status != delivery.StatusFailedPermanent {
		t.Fatalf("permanent material failure left recipient non-terminal: %#v", value)
	}
}

func TestTransientEligibilityDoesNotTerminallyExcludeRecipient(t *testing.T) {
	for _, reason := range []string{
		"CAMPAIGN_PAUSED",
		"CAMPAIGN_NOT_STARTED",
		"CAMPAIGN_QUIET_HOURS",
	} {
		t.Run(reason, func(t *testing.T) {
			repo := delivery.NewMemoryRepository(recipientFixture())
			ledger := delivery.NewService(repo)
			gateway := &gatewayStub{}
			h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligibilityFunc(func(context.Context, delivery.Recipient, time.Time) (EligibilityDecision, error) {
				return EligibilityDecision{Eligible: false, Reason: reason}, nil
			}), Gateway: gateway}
			var retry jobs.RetryableError
			if err := h.Handle(context.Background(), jobFixture()); !errors.As(err, &retry) {
				t.Fatalf("expected retryable transient eligibility error, got %T %v", err, err)
			}
			if gateway.calls != 0 {
				t.Fatal("gateway must not be called while eligibility is transiently blocked")
			}
			value, err := ledger.Get(context.Background(), "recipient-1")
			if err != nil {
				t.Fatal(err)
			}
			if value.Status != delivery.StatusAuthorised {
				t.Fatalf("transient eligibility became terminal: got %s", value.Status)
			}
		})
	}
}

func TestFrequencyCapRemainsFinalEligibilityExclusion(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	gateway := &gatewayStub{}
	h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligibilityFunc(func(context.Context, delivery.Recipient, time.Time) (EligibilityDecision, error) {
		return EligibilityDecision{Eligible: false, Reason: "FREQUENCY_CAPPED"}, nil
	}), Gateway: gateway}
	if err := h.Handle(context.Background(), jobFixture()); err != nil {
		t.Fatal(err)
	}
	if gateway.calls != 0 {
		t.Fatal("gateway must not be called for frequency-capped recipient")
	}
	value, err := ledger.Get(context.Background(), "recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != delivery.StatusSuppressedBeforeSend {
		t.Fatalf("frequency cap must remain a final exclusion, got %s", value.Status)
	}
}

func TestSafeToRetryOutcomeCanSubmitAgain(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	gateway := &gatewayStub{err: GatewayError{Code: "RATE_LIMITED", Safety: FailureSafeToRetry, Err: errors.New("provider rejected before acceptance")}}
	h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligible(), Gateway: gateway}
	job := jobFixture()
	var retry jobs.RetryableError
	if err := h.Handle(context.Background(), job); !errors.As(err, &retry) {
		t.Fatalf("expected safe failure to return retryable error, got %T %v", err, err)
	}
	first, err := ledger.Get(context.Background(), "recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != delivery.StatusFailedRetryable {
		t.Fatalf("safe retry must leave FAILED_RETRYABLE, got %s", first.Status)
	}
	gateway.mu.Lock()
	gateway.err = nil
	gateway.result = GatewayResult{Accepted: true, ProviderMessageID: "provider-retry-accepted", AcceptedAt: time.Now().UTC()}
	gateway.mu.Unlock()
	job.AttemptCount++
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("retry after safe failure: %v", err)
	}
	if gateway.calls != 2 {
		t.Fatalf("gateway calls=%d want 2", gateway.calls)
	}
	final, err := ledger.Get(context.Background(), "recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != delivery.StatusGatewayAccepted {
		t.Fatalf("safe retry did not progress to accepted: %s", final.Status)
	}
}

func TestTemporaryMetaHealthMaterialFailureIsRetryableWithoutLedgerMutation(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	gateway := &gatewayStub{}
	h := &Handler{
		Ledger: delivery.NewService(repo),
		Materials: loaderFunc(func(context.Context, delivery.Recipient) (Material, error) {
			return Material{}, errMetaSenderUnavailable
		}),
		Eligibility: eligible(), Gateway: gateway,
	}
	var retry jobs.RetryableError
	if err := h.Handle(context.Background(), jobFixture()); !errors.As(err, &retry) {
		t.Fatalf("expected retryable Meta health hold, got %T %v", err, err)
	}
	if gateway.calls != 0 {
		t.Fatalf("gateway called %d times during Meta health hold", gateway.calls)
	}
	value, err := h.Ledger.Get(context.Background(), "recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != delivery.StatusAuthorised {
		t.Fatalf("Meta health hold mutated recipient status to %s", value.Status)
	}
}

func TestHandlerTerminalizesInvalidLoadedMaterial(t *testing.T) {
	now := time.Date(2026, 8, 15, 23, 10, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "r-invalid-loaded-material", CampaignID: "c", ContactID: "ct", MessageVersionID: "m", IdempotencyKey: "idem-invalid-loaded-material", Status: delivery.StatusQueued, UpdatedAt: now}
	ledger := delivery.NewService(delivery.NewMemoryRepository(recipient))
	gateway := &gatewayStub{}
	handler := &Handler{
		Ledger: ledger, Eligibility: eligible(), Gateway: gateway, Clock: func() time.Time { return now },
		Materials: loaderFunc(func(context.Context, delivery.Recipient) (Material, error) {
			return Material{Provider: "BROKEN", RouteReference: "c:r", RecipientE164: "+2348012345678", MessageType: "text", Body: "hello"}, nil
		}),
	}
	job, err := jobs.NewJob(jobs.EnqueueInput{Type: JobType, DedupKey: "dispatch:invalid-loaded-material", Payload: JobPayload{CampaignRecipientID: recipient.ID}}, now)
	if err != nil {
		t.Fatal(err)
	}
	job.AttemptCount = 1
	if err := handler.Handle(context.Background(), job); err != nil {
		t.Fatalf("invalid loaded material should terminalize without retry: %v", err)
	}
	value, err := ledger.Get(context.Background(), recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != delivery.StatusFailedPermanent || gateway.calls != 0 {
		t.Fatalf("invalid material result=%#v gatewayCalls=%d", value, gateway.calls)
	}
}

func TestHandlerFailsClosedBeforeSubmittingWhenGatewayLacksPreparedContract(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	calls := 0
	gateway := gatewayFunc(func(context.Context, GatewayRequest) (GatewayResult, error) {
		calls++
		return GatewayResult{Accepted: true, ProviderMessageID: "must-not-send"}, nil
	})
	h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligible(), Gateway: gateway}
	var retry jobs.RetryableError
	if err := h.Handle(context.Background(), jobFixture()); !errors.As(err, &retry) || retry.Code != "GATEWAY_PREPARED_CONTRACT_REQUIRED" {
		t.Fatalf("missing prepared contract must fail closed before submit, got %T %v", err, err)
	}
	value, err := ledger.Get(context.Background(), "recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != delivery.StatusAuthorised || value.AttemptCount != 0 || calls != 0 {
		t.Fatalf("unprepared gateway crossed submission boundary: recipient=%#v calls=%d", value, calls)
	}
}

type pacingFunc func(context.Context, delivery.Recipient, Material, time.Time) error

func (f pacingFunc) Wait(ctx context.Context, recipient delivery.Recipient, material Material, now time.Time) error {
	return f(ctx, recipient, material, now)
}

func TestHandlerRechecksEligibilityAfterPacingBeforeSubmitting(t *testing.T) {
	repo := delivery.NewMemoryRepository(recipientFixture())
	ledger := delivery.NewService(repo)
	gateway := &gatewayStub{result: GatewayResult{Accepted: true, ProviderMessageID: "must-not-send"}}
	checks := 0
	h := &Handler{
		Ledger: ledger, Materials: validLoader(), Gateway: gateway,
		Eligibility: eligibilityFunc(func(context.Context, delivery.Recipient, time.Time) (EligibilityDecision, error) {
			checks++
			if checks == 1 {
				return EligibilityDecision{Eligible: true}, nil
			}
			return EligibilityDecision{Eligible: false, Reason: "CAMPAIGN_PAUSED"}, nil
		}),
		Pacing: pacingFunc(func(context.Context, delivery.Recipient, Material, time.Time) error { return nil }),
	}
	var retry jobs.RetryableError
	if err := h.Handle(context.Background(), jobFixture()); !errors.As(err, &retry) || retry.Code != "CAMPAIGN_PAUSED" {
		t.Fatalf("final eligibility hold should be retryable, got %T %v", err, err)
	}
	value, err := ledger.Get(context.Background(), "recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	if checks != 2 || gateway.calls != 0 || value.Status != delivery.StatusAuthorised || value.AttemptCount != 0 {
		t.Fatalf("eligibility was not rechecked before submit: checks=%d gateway=%d recipient=%#v", checks, gateway.calls, value)
	}
}
