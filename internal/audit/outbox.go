package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const OutboxEventType = "AUDIT_EVENT_PENDING"

type outboxExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// EnqueueTx prepares the exact audit event that will later enter the hash chain
// and persists it in the caller's transaction. A committed business mutation
// therefore always has durable audit evidence even if chain publication is down.
func EnqueueTx(ctx context.Context, tx outboxExecer, input Input) (Event, error) {
	if tx == nil {
		return Event{}, fmt.Errorf("audit outbox transaction is required")
	}
	event, err := New(input)
	if err != nil {
		return Event{}, err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return Event{}, fmt.Errorf("marshal audit outbox event: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO transactional_outbox(
 id,deduplication_key,aggregate_type,aggregate_id,event_type,payload,status,available_at,created_at,updated_at
) VALUES($1::uuid,$2,'AUDIT',$1::uuid,$3,$4::jsonb,'PENDING',$5,$5,$5)`, event.ID, "audit:"+event.ID, OutboxEventType, string(payload), event.OccurredAt)
	if err != nil {
		return Event{}, fmt.Errorf("enqueue audit evidence: %w", err)
	}
	return event, nil
}
