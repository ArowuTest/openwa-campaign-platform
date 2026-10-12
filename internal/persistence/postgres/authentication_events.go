package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"campaign-platform/internal/identity"
)

const authenticationEventSelect = `SELECT id::text,coalesce(user_id::text,''),coalesce(encode(email_hash,'hex'),''),event_type,coalesce(host(source_ip),''),coalesce(user_agent,''),correlation_id,occurred_at,metadata FROM authentication_events`

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
		var metadata []byte
		if err := rows.Scan(&event.ID, &event.UserID, &event.EmailHash, &eventType, &event.SourceIP, &event.UserAgent, &event.CorrelationID, &event.OccurredAt, &metadata); err != nil {
			return identity.AuthenticationEventPage{}, err
		}
		event.Type = identity.AuthenticationEventType(eventType)
		if len(metadata) > 0 {
			if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
				return identity.AuthenticationEventPage{}, fmt.Errorf("decode authentication metadata: %w", err)
			}
		}
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
