package outbox

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"campaign-platform/internal/audit"
)

func TestAuditOutboxPublisherIsIdempotentAcrossReplay(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	event, err := audit.New(audit.Input{
		ActorType: "USER", ActorID: "00000000-0000-4000-8000-000000000001",
		Action: "TASK6_OUTBOX_AUDIT", ObjectType: "TASK6", ObjectID: "audit-outbox",
		Outcome: "SUCCESS", Sensitivity: "HIGH", CorrelationID: "task6-audit-outbox",
		OccurredAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	auditRepo := audit.NewMemoryRepository()
	publisher := &Publisher{Outbox: NewMemoryRepository(), Audit: audit.NewRecorder(auditRepo)}
	record := Record{AggregateType: "AUDIT", AggregateID: event.ID, EventType: audit.OutboxEventType, Payload: payload}
	if err := publisher.Publish(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	page, err := auditRepo.Search(context.Background(), audit.Query{CorrelationID: event.CorrelationID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != event.ID {
		t.Fatalf("audit outbox replay duplicated or changed event: %+v", page.Items)
	}
	bad := record
	bad.AggregateID = "00000000-0000-4000-8000-000000000002"
	if err := publisher.Publish(context.Background(), bad); err == nil {
		t.Fatal("audit outbox identity mismatch was accepted")
	}
}
