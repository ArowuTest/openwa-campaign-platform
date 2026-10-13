package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"campaign-platform/internal/identity"
)

// SnapshotAuthenticationEvents uses one SELECT statement and therefore one
// PostgreSQL MVCC snapshot. Sequence allocation, occurrence times and UUID
// ordering cannot prove commit membership; the returned rows are the frozen set.
func (r *AuthenticationEventRecorder) SnapshotAuthenticationEvents(ctx context.Context, query identity.AuthenticationEventQuery, maximum int) ([]identity.AuthenticationEvent, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if err := identity.ValidateAuthenticationEventSnapshotQuery(query, maximum); err != nil {
		return nil, err
	}
	clauses := []string{"1=1"}
	args := make([]any, 0, 6)
	add := func(condition string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(condition, len(args)))
	}
	if value := strings.TrimSpace(query.UserID); value != "" {
		add("user_id=$%d::uuid", value)
	}
	if query.EventType != "" {
		add("event_type=$%d", string(query.EventType))
	}
	if value := strings.TrimSpace(query.CorrelationID); value != "" {
		add("correlation_id=$%d", value)
	}
	if query.From != nil {
		add("occurred_at >= $%d", query.From.UTC())
	}
	if query.To != nil {
		add("occurred_at < $%d", query.To.UTC())
	}
	args = append(args, maximum+1)
	statement := authenticationEventSelect + " WHERE " + strings.Join(clauses, " AND ") + fmt.Sprintf(" ORDER BY occurred_at DESC,id DESC LIMIT $%d", len(args))
	rows, err := r.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]identity.AuthenticationEvent, 0)
	for rows.Next() {
		if len(items) == maximum {
			return nil, identity.ErrAuthenticationEventSnapshotTooLarge
		}
		var event identity.AuthenticationEvent
		var eventType string
		if err := rows.Scan(&event.ID, &event.UserID, &eventType, &event.CorrelationID, &event.OccurredAt); err != nil {
			return nil, err
		}
		event.Type = identity.AuthenticationEventType(eventType)
		event.OccurredAt = event.OccurredAt.UTC()
		items = append(items, event)
	}
	return items, rows.Err()
}
