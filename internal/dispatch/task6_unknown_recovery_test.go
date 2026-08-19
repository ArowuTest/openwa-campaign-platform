package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/jobs"
)

func TestTask6AlreadyUnknownRecipientIsNeverResubmitted(t *testing.T) {
	recipient := recipientFixture()
	recipient.Status = delivery.StatusUnknown
	recipient.AttemptCount = 1
	repo := delivery.NewMemoryRepository(recipient)
	ledger := delivery.NewService(repo)
	gateway := &gatewayStub{result: GatewayResult{Accepted: true, ProviderMessageID: "unsafe-resubmit"}}
	h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligible(), Gateway: gateway}

	if err := h.Handle(context.Background(), jobFixture()); err != nil {
		t.Fatal(err)
	}
	if gateway.calls != 0 {
		t.Fatalf("UNKNOWN recipient was resubmitted to gateway %d time(s)", gateway.calls)
	}
	stored, err := ledger.Get(context.Background(), recipient.ID)
	if err != nil || stored.Status != delivery.StatusUnknown || stored.AttemptCount != 1 {
		t.Fatalf("UNKNOWN evidence changed: recipient=%+v err=%v", stored, err)
	}
}

type task6SubmissionGuardRepository struct {
	*delivery.MemoryRepository
}

func (r *task6SubmissionGuardRepository) ApplyEvent(ctx context.Context, id string, event delivery.Event) (delivery.Recipient, bool, error) {
	if event.Type == delivery.EventSubmitting {
		return delivery.Recipient{}, false, delivery.ErrCampaignNotDispatchable
	}
	return r.MemoryRepository.ApplyEvent(ctx, id, event)
}

func TestTask6CancellationWinningBeforeSubmissionCompletesJobWithoutSend(t *testing.T) {
	recipient := recipientFixture()
	recipient.Status = delivery.StatusClaimed
	repo := &task6SubmissionGuardRepository{MemoryRepository: delivery.NewMemoryRepository(recipient)}
	ledger := delivery.NewService(repo)
	gateway := &gatewayStub{result: GatewayResult{Accepted: true, ProviderMessageID: "must-not-send"}}
	checks := 0
	eligibility := eligibilityFunc(func(context.Context, delivery.Recipient, time.Time) (EligibilityDecision, error) {
		checks++
		if checks == 1 {
			return EligibilityDecision{Eligible: true}, nil
		}
		return EligibilityDecision{Eligible: false, Reason: "CAMPAIGN_CANCELLED"}, nil
	})
	h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligibility, Gateway: gateway}
	if err := h.Handle(context.Background(), jobFixture()); err != nil {
		t.Fatalf("cancellation race must complete cleanly: %v", err)
	}
	if gateway.calls != 0 {
		t.Fatalf("gateway called %d time(s) after cancellation won", gateway.calls)
	}
	stored, err := ledger.Get(context.Background(), recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != delivery.StatusCancelled {
		t.Fatalf("recipient status=%s want CANCELLED", stored.Status)
	}
	if checks != 2 {
		t.Fatalf("eligibility checks=%d want 2", checks)
	}
}

func TestTask6StaleSubmittingRecipientBecomesUnknownWithoutResend(t *testing.T) {
	recipient := recipientFixture()
	recipient.Status = delivery.StatusSubmitting
	recipient.AttemptCount = 1
	repo := delivery.NewMemoryRepository(recipient)
	ledger := delivery.NewService(repo)
	gateway := &gatewayStub{result: GatewayResult{Accepted: true, ProviderMessageID: "unsafe-retry"}}
	h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligible(), Gateway: gateway}

	if err := h.Handle(context.Background(), jobFixture()); err != nil {
		t.Fatal(err)
	}
	if gateway.calls != 0 {
		t.Fatalf("stale SUBMITTING recipient was resent %d time(s)", gateway.calls)
	}
	stored, err := ledger.Get(context.Background(), recipient.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != delivery.StatusUnknown || stored.AttemptCount != 1 || !stored.ReconciliationRequired {
		t.Fatalf("stale submission not quarantined as UNKNOWN: %+v", stored)
	}
}

type task6ThrottleFunc func(context.Context, string) error

func (f task6ThrottleFunc) Wait(ctx context.Context, sessionID string) error {
	return f(ctx, sessionID)
}

func TestTask6WorkerCancellationBeforeSubmissionNeverCallsGateway(t *testing.T) {
	recipient := recipientFixture()
	recipient.Status = delivery.StatusClaimed
	repo := delivery.NewMemoryRepository(recipient)
	ledger := delivery.NewService(repo)
	gateway := &gatewayStub{result: GatewayResult{Accepted: true, ProviderMessageID: "must-not-send"}}
	h := &Handler{
		Ledger: ledger, Materials: validLoader(), Eligibility: eligible(), Gateway: gateway,
		Throttle: task6ThrottleFunc(func(ctx context.Context, _ string) error { return ctx.Err() }),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := h.Handle(ctx, jobFixture())
	var retry jobs.RetryableError
	if !errors.As(err, &retry) || retry.Code != "THROTTLE_WAIT_INTERRUPTED" {
		t.Fatalf("worker cancellation must remain pre-submit retryable, got %v", err)
	}
	if gateway.calls != 0 {
		t.Fatalf("gateway called %d time(s) after pre-submit cancellation", gateway.calls)
	}
	stored, getErr := ledger.Get(context.Background(), recipient.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if stored.Status != delivery.StatusClaimed || stored.AttemptCount != 0 {
		t.Fatalf("pre-submit cancellation mutated recipient: %+v", stored)
	}
}

func TestTask6CampaignCancellationPreservesAcceptedAndUnknownEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status delivery.Status
		want   delivery.Status
	}{
		{"queued", delivery.StatusQueued, delivery.StatusCancelled},
		{"claimed", delivery.StatusClaimed, delivery.StatusCancelled},
		{"gateway-accepted", delivery.StatusGatewayAccepted, delivery.StatusGatewayAccepted},
		{"unknown", delivery.StatusUnknown, delivery.StatusUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recipient := recipientFixture()
			recipient.Status = tc.status
			repo := delivery.NewMemoryRepository(recipient)
			ledger := delivery.NewService(repo)
			gateway := &gatewayStub{result: GatewayResult{Accepted: true, ProviderMessageID: "must-not-send"}}
			eligibility := eligibilityFunc(func(context.Context, delivery.Recipient, time.Time) (EligibilityDecision, error) {
				return EligibilityDecision{Eligible: false, Reason: "CAMPAIGN_CANCELLED"}, nil
			})
			h := &Handler{Ledger: ledger, Materials: validLoader(), Eligibility: eligibility, Gateway: gateway}
			if err := h.Handle(context.Background(), jobFixture()); err != nil {
				t.Fatal(err)
			}
			if gateway.calls != 0 {
				t.Fatalf("gateway called %d time(s) for %s recipient", gateway.calls, tc.status)
			}
			stored, err := ledger.Get(context.Background(), recipient.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != tc.want {
				t.Fatalf("status=%s want=%s", stored.Status, tc.want)
			}
		})
	}
}
