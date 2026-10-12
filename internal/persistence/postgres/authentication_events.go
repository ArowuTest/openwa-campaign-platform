package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"campaign-platform/internal/identity"
)

// The audit-reader/export query deliberately selects only the fields that may
// cross the authentication-event read boundary. Sensitive email hashes, source
// IPs, user agents and metadata remain unavailable to the export worker role.
const authenticationEventSelect = `SELECT id::text,coalesce(user_id::text,''),event_type,correlation_id,occurred_at FROM authentication_events`

func (r *AuthenticationEventRecorder) SearchAuthenticationEvents(ctx context.Context, query identity.AuthenticationEventQuery) (identity.AuthenticationEventPage, error) {
	if r == nil || r.DB == nil {
		return identity.AuthenticationEventPage{}, errors.New("database is required")
	}
	if query.Limit <= 0 {
		query.Limit = 100
	}
	if query.Limit > 500 {
		return identity.AuthenticationEventPage{}, identity.ErrInvalidAuthenticationEventQuery
	}
	if err := identity.ValidateAuthenticationEventQuery(query); err != nil {
		return identity.AuthenticationEventPage{}, err
	}
	clauses := []string{"1=1"}
	args := make([]any, 0, 8)
	add := func(condition string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(condition, len(args)))
	}
	if query.After != nil {
		if query.After.OccurredAt.IsZero() || query.After.ID == "" {
			return identity.AuthenticationEventPage{}, identity.ErrInvalidAuthenticationEventQuery
		}
		args = append(args, query.After.OccurredAt.UTC(), query.After.ID)
		clauses = append(clauses, fmt.Sprintf("(occurred_at,id) < ($%d,$%d::uuid)", len(args)-1, len(args)))
	}
	if value := strings.TrimSpace(query.UserID); value != "" {
		add("user_id=$%d::uuid", value)
	}
	if value := strings.TrimSpace(string(query.EventType)); value != "" {
		add("event_type=upper($%d)", value)
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
	args = append(args, query.Limit+1)
	statement := authenticationEventSelect + " WHERE " + strings.Join(clauses, " AND ") + fmt.Sprintf(" ORDER BY occurred_at DESC,id DESC LIMIT $%d", len(args))
	rows, err := r.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return identity.AuthenticationEventPage{}, err
	}
	defer rows.Close()
	items := make([]identity.AuthenticationEvent, 0, query.Limit+1)
	for rows.Next() {
		var event identity.AuthenticationEvent
		var eventType string
		if err := rows.Scan(&event.ID, &event.UserID, &eventType, &event.CorrelationID, &event.OccurredAt); err != nil {
			return identity.AuthenticationEventPage{}, err
		}
		event.Type = identity.AuthenticationEventType(eventType)
		event.OccurredAt = event.OccurredAt.UTC()
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return identity.AuthenticationEventPage{}, err
	}
	if len(items) <= query.Limit {
		return identity.AuthenticationEventPage{Items: items}, nil
	}
	items = items[:query.Limit]
	next, err := identity.EncodeAuthenticationEventCursor(identity.AuthenticationEventCursor{OccurredAt: items[len(items)-1].OccurredAt, ID: items[len(items)-1].ID})
	if err != nil {
		return identity.AuthenticationEventPage{}, err
	}
	return identity.AuthenticationEventPage{Items: items, NextCursor: next}, nil
}
