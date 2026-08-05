package consent

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
)

func TestOptOutPolicyUsesExactNormalisedCommands(t *testing.T) {
	policy := DefaultOptOutPolicy()
	for _, value := range []string{"stop", " STOP! ", "opt   out", "unsubscribe."} {
		if !policy.Recognises(value) {
			t.Fatalf("expected %q to be recognised", value)
		}
	}
	for _, value := range []string{"do not stop", "please stop sending eventually", "stopping", ""} {
		if policy.Recognises(value) {
			t.Fatalf("did not expect %q to be recognised", value)
		}
	}
}

func TestOptOutProcessorCreatesIdempotentGlobalSuppression(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	recipient := delivery.Recipient{ID: "recipient-1", ContactID: "00000000-0000-4000-8000-000000000010", Status: delivery.StatusDelivered, UpdatedAt: now}
	deliveries := delivery.NewService(delivery.NewMemoryRepository(recipient))
	ledger := NewLedgerService(NewMemoryLedgerRepository())
	processor := &OptOutProcessor{Deliveries: deliveries, Ledger: ledger, Clock: func() time.Time { return now }}

	first, err := processor.Process(context.Background(), recipient.ID, "evt-stop-1", "STOP", "gateway:evt-stop-1")
	if err != nil || !first.Recognised || first.Replayed || first.Suppression.Scope != SuppressionGlobal {
		t.Fatalf("first result=%+v err=%v", first, err)
	}
	replay, err := processor.Process(context.Background(), recipient.ID, "evt-stop-1", " stop ", "gateway:evt-stop-1")
	if err != nil || !replay.Recognised || !replay.Replayed || replay.Suppression.ID != first.Suppression.ID {
		t.Fatalf("replay result=%+v err=%v", replay, err)
	}
}
