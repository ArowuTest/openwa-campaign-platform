package delivery

import (
	"testing"
	"time"
)

func event(key string, typ EventType, at time.Time) Event {
	return Event{DeduplicationKey: key, Type: typ, OccurredAt: at}
}

func TestOutOfOrderSentDoesNotDowngradeDelivered(t *testing.T) {
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	recipient := Recipient{Status: StatusGatewayAccepted}
	recipient, changed, err := Apply(recipient, event("evt-delivered", EventDelivered, now))
	if err != nil || !changed || recipient.Status != StatusDelivered {
		t.Fatalf("delivery event failed: %+v %v", recipient, err)
	}
	recipient, changed, err = Apply(recipient, event("evt-sent", EventSent, now.Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if !changed || recipient.Status != StatusDelivered {
		t.Fatalf("late sent event downgraded state: %+v", recipient)
	}
}

func TestDuplicateSubmittingEventIsIdempotent(t *testing.T) {
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	recipient := Recipient{Status: StatusClaimed}
	first := event("attempt-1-submitting", EventSubmitting, now)
	recipient, changed, err := Apply(recipient, first)
	if err != nil || !changed || recipient.AttemptCount != 1 {
		t.Fatalf("first event: %+v err=%v", recipient, err)
	}
	recipient, changed, err = Apply(recipient, first)
	if err != nil || changed || recipient.AttemptCount != 1 {
		t.Fatalf("duplicate was not idempotent: %+v changed=%v err=%v", recipient, changed, err)
	}
}

func TestLatePermanentFailureDoesNotEraseDeliveredEvidence(t *testing.T) {
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	recipient := Recipient{Status: StatusDelivered, ProviderMessageID: "provider-1"}
	late := Event{
		DeduplicationKey: "late-failure", Type: EventFailedPermanent,
		ProviderMessageID: "provider-1", ErrorCode: "REMOTE_FAILURE", OccurredAt: now,
	}
	recipient, changed, err := Apply(recipient, late)
	if err != nil || !changed {
		t.Fatalf("late event: changed=%v err=%v", changed, err)
	}
	if recipient.Status != StatusDelivered || !recipient.ReconciliationRequired || recipient.ContradictoryEventCount != 1 {
		t.Fatalf("delivery evidence was corrupted: %+v", recipient)
	}
}

func TestProviderMessageMismatchRequiresReconciliation(t *testing.T) {
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	recipient := Recipient{Status: StatusGatewayAccepted, ProviderMessageID: "provider-1"}
	recipient, _, err := Apply(recipient, Event{
		DeduplicationKey: "mismatch", Type: EventSent, ProviderMessageID: "provider-2", OccurredAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !recipient.ReconciliationRequired || recipient.Status != StatusGatewayAccepted {
		t.Fatalf("mismatch was not quarantined: %+v", recipient)
	}
}

func TestIdempotencyKeyIsDeterministic(t *testing.T) {
	first, _ := NewIdempotencyKey("campaign", "contact", "message")
	second, _ := NewIdempotencyKey("campaign", "contact", "message")
	if first == "" || first != second {
		t.Fatal("idempotency key is not deterministic")
	}
}

func TestMetricsExposeGatewayAcceptedSeparatelyFromSubmitting(t *testing.T) {
	metrics := Metrics{}.Move("", StatusSubmitting)
	if metrics.SubmittedTotal != 1 || metrics.GatewayAcceptedTotal != 0 {
		t.Fatalf("submitting metrics=%+v", metrics)
	}
	metrics = metrics.Move(StatusSubmitting, StatusGatewayAccepted)
	if metrics.SubmittedTotal != 0 || metrics.GatewayAcceptedTotal != 1 {
		t.Fatalf("gateway accepted metrics=%+v", metrics)
	}
}

func TestMetricsMoveUsesCurrentStateNotEventTotals(t *testing.T) {
	metrics := Metrics{GatewayAcceptedTotal: 1}
	metrics = metrics.Move(StatusGatewayAccepted, StatusDelivered)
	if metrics.GatewayAcceptedTotal != 0 || metrics.DeliveredTotal != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestLateSubmittingDoesNotDowngradeRead(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	recipient := Recipient{Status: StatusRead, UpdatedAt: now}
	recipient, changed, err := Apply(recipient, event("late-submitting", EventSubmitting, now.Add(time.Second)))
	if err != nil || !changed {
		t.Fatalf("late submitting: changed=%v err=%v", changed, err)
	}
	if recipient.Status != StatusRead || recipient.AttemptCount != 0 {
		t.Fatalf("late progress event corrupted recipient: %+v", recipient)
	}
}

func TestLateQueuedDoesNotReopenUnknown(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	recipient := Recipient{Status: StatusUnknown, UpdatedAt: now}
	recipient, _, err := Apply(recipient, event("late-queued", EventQueued, now.Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if recipient.Status != StatusUnknown {
		t.Fatalf("unknown outcome was reopened automatically: %+v", recipient)
	}
}

func TestAcknowledgementAfterPermanentFailureRequiresReconciliation(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	recipient := Recipient{Status: StatusFailedPermanent, UpdatedAt: now}
	recipient, _, err := Apply(recipient, Event{DeduplicationKey: "late-delivered", Type: EventDelivered, ProviderMessageID: "p1", OccurredAt: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if recipient.Status != StatusDelivered || !recipient.ReconciliationRequired || recipient.ContradictoryEventCount != 1 {
		t.Fatalf("late acknowledgement not reconciled correctly: %+v", recipient)
	}
}

func TestUnknownAfterGatewayAcceptancePreservesHighestAcknowledgement(t *testing.T) {
	now := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	recipient := Recipient{ID: "r", Status: StatusSubmitting, UpdatedAt: now}
	accepted, _, err := Apply(recipient, Event{DeduplicationKey: "accepted-event-key-0001", Type: EventGatewayAccepted, ProviderMessageID: "provider-1", OccurredAt: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	unknown, _, err := Apply(accepted, Event{DeduplicationKey: "unknown-event-key-000001", Type: EventUnknown, ErrorCode: "TIMEOUT", OccurredAt: now.Add(2 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Status != StatusUnknown || unknown.HighestAcknowledgement != StatusGatewayAccepted {
		t.Fatalf("uncertainty erased acknowledgement evidence: %+v", unknown)
	}
	lateSuppression, _, err := Apply(unknown, Event{DeduplicationKey: "suppression-event-key-1", Type: EventSuppressed, OccurredAt: now.Add(3 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if lateSuppression.Status != StatusUnknown || !lateSuppression.ReconciliationRequired {
		t.Fatalf("late suppression rewrote accepted send evidence: %+v", lateSuppression)
	}
}

func TestEventFingerprintSeparatesProviderEventAndMessageIdentity(t *testing.T) {
	occurred := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	base := Event{Type: EventDelivered, ProviderEventID: "callback-1", ProviderMessageID: "message-1", OccurredAt: occurred}
	changedEvent := base
	changedEvent.ProviderEventID = "callback-2"
	changedMessage := base
	changedMessage.ProviderMessageID = "message-2"
	if Fingerprint(base) == Fingerprint(changedEvent) || Fingerprint(base) == Fingerprint(changedMessage) {
		t.Fatal("provider event and message identifiers are not independently bound into the fingerprint")
	}
}

func TestUnknownCannotBecomeRetryableOrRestart(t *testing.T) {
	now := time.Date(2026, 8, 15, 22, 50, 0, 0, time.UTC)
	recipient := Recipient{Status: StatusSubmitting, UpdatedAt: now}
	unknown, _, err := Apply(recipient, event("unknown-sticky", EventUnknown, now.Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	afterRetryable, _, err := Apply(unknown, event("late-retryable", EventFailedRetryable, now.Add(2*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if afterRetryable.Status != StatusUnknown || !afterRetryable.ReconciliationRequired {
		t.Fatalf("UNKNOWN was reopened by retryable failure: %+v", afterRetryable)
	}
	afterQueued, _, err := Apply(afterRetryable, event("late-requeue", EventQueued, now.Add(3*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if afterQueued.Status != StatusUnknown {
		t.Fatalf("UNKNOWN restarted pre-send progression: %+v", afterQueued)
	}
}

func TestTerminalFailureCannotBecomeRetryable(t *testing.T) {
	now := time.Date(2026, 8, 15, 22, 51, 0, 0, time.UTC)
	recipient := Recipient{Status: StatusClaimed, UpdatedAt: now}
	terminalRecipient, _, err := Apply(recipient, event("permanent", EventFailedPermanent, now.Add(time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	afterRetryable, _, err := Apply(terminalRecipient, event("late-retryable-terminal", EventFailedRetryable, now.Add(2*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if afterRetryable.Status != StatusFailedPermanent || afterRetryable.CompletedAt == nil {
		t.Fatalf("terminal decision was resurrected: %+v", afterRetryable)
	}
}

func TestRetryableAfterGatewayAcceptanceCannotReopenSend(t *testing.T) {
	now := time.Date(2026, 8, 15, 22, 52, 0, 0, time.UTC)
	recipient := Recipient{Status: StatusSubmitting, UpdatedAt: now}
	accepted, _, err := Apply(recipient, Event{DeduplicationKey: "gateway-accepted-retry", Type: EventGatewayAccepted, ProviderMessageID: "provider-old", OccurredAt: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	afterRetryable, _, err := Apply(accepted, event("retryable-after-gateway-accept", EventFailedRetryable, now.Add(2*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if afterRetryable.Status != StatusGatewayAccepted || afterRetryable.HighestAcknowledgement != StatusGatewayAccepted || afterRetryable.ProviderMessageID != "provider-old" {
		t.Fatalf("retryable failure reopened an accepted send: %+v", afterRetryable)
	}
	if !afterRetryable.ReconciliationRequired {
		t.Fatalf("contradictory retryable failure was not retained for reconciliation: %+v", afterRetryable)
	}
}

func TestLastEventAtDoesNotMoveBackwardsForIgnoredLateEvent(t *testing.T) {
	now := time.Date(2026, 8, 15, 22, 53, 0, 0, time.UTC)
	recipient := Recipient{Status: StatusSent, HighestAcknowledgement: StatusSent, UpdatedAt: now}
	last := now
	recipient.LastEventAt = &last
	late, _, err := Apply(recipient, event("late-queued-time", EventQueued, now.Add(-time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if late.LastEventAt == nil || !late.LastEventAt.Equal(now) {
		t.Fatalf("last event occurrence regressed: %+v", late.LastEventAt)
	}
}
