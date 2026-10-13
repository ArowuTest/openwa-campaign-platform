package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"campaign-platform/internal/shared/id"
)

var ErrInvalidAuthenticationEventQuery = errors.New("invalid authentication event query")

// AuthenticationEventQuery constrains the audit-reader view of authentication evidence.
type AuthenticationEventQuery struct {
	After         *AuthenticationEventCursor
	UserID        string
	EventType     AuthenticationEventType
	CorrelationID string
	From          *time.Time
	To            *time.Time
	Limit         int
}

// AuthenticationEventCursor is an opaque keyset position ordered by occurrence time and event ID.
type AuthenticationEventCursor struct {
	OccurredAt time.Time
	ID         string
}

type AuthenticationEventPage struct {
	Items      []AuthenticationEvent
	NextCursor string
}

type AuthenticationEventQueryRepository interface {
	SearchAuthenticationEvents(context.Context, AuthenticationEventQuery) (AuthenticationEventPage, error)
}

func EncodeAuthenticationEventCursor(cursor AuthenticationEventCursor) (string, error) {
	cursor.OccurredAt = cursor.OccurredAt.UTC()
	if cursor.OccurredAt.IsZero() || !id.IsUUID(cursor.ID) {
		return "", ErrInvalidAuthenticationEventQuery
	}
	payload, err := json.Marshal(struct {
		OccurredAt time.Time
		ID         string
	}{cursor.OccurredAt, strings.ToLower(cursor.ID)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeAuthenticationEventCursor(raw string) (AuthenticationEventCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AuthenticationEventCursor{}, nil
	}
	if len(raw) > 512 {
		return AuthenticationEventCursor{}, ErrInvalidAuthenticationEventQuery
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return AuthenticationEventCursor{}, ErrInvalidAuthenticationEventQuery
	}
	var value struct {
		OccurredAt time.Time
		ID         string
	}
	if err := json.Unmarshal(payload, &value); err != nil || value.OccurredAt.IsZero() || !id.IsUUID(value.ID) {
		return AuthenticationEventCursor{}, ErrInvalidAuthenticationEventQuery
	}
	value.ID = strings.ToLower(value.ID)
	return AuthenticationEventCursor{OccurredAt: value.OccurredAt.UTC(), ID: value.ID}, nil
}

func ValidateAuthenticationEventQuery(query AuthenticationEventQuery) error {
	if query.Limit <= 0 {
		query.Limit = 100
	}
	if query.Limit > 500 {
		return ErrInvalidAuthenticationEventQuery
	}
	if value := strings.TrimSpace(query.UserID); value != "" && !id.IsUUID(value) {
		return ErrInvalidAuthenticationEventQuery
	}
	if query.EventType != "" && !validAuthenticationEventType(query.EventType) {
		return ErrInvalidAuthenticationEventQuery
	}
	if query.After != nil && (!id.IsUUID(strings.TrimSpace(query.After.ID)) || query.After.OccurredAt.IsZero()) {
		return ErrInvalidAuthenticationEventQuery
	}
	if query.From != nil && query.To != nil && query.From.After(*query.To) {
		return ErrInvalidAuthenticationEventQuery
	}
	return nil
}

func validAuthenticationEventType(value AuthenticationEventType) bool {
	switch value {
	case EventLoginSucceeded, EventLoginFailed, EventMFASucceeded, EventMFAFailed,
		EventAccountLocked, EventSessionRevoked, EventAllSessionsRevoked,
		EventPermissionDenied, EventStepUpRequired, EventNetworkDenied:
		return true
	default:
		return false
	}
}

func cloneAuthenticationEvent(event AuthenticationEvent) AuthenticationEvent {
	event.Metadata = cloneMetadata(event.Metadata)
	return event
}

func (r *MemoryEventRecorder) SearchAuthenticationEvents(_ context.Context, query AuthenticationEventQuery) (AuthenticationEventPage, error) {
	if query.Limit <= 0 {
		query.Limit = 100
	}
	if err := ValidateAuthenticationEventQuery(query); err != nil {
		return AuthenticationEventPage{}, err
	}
	r.mu.Lock()
	items := make([]AuthenticationEvent, 0, len(r.events))
	for _, event := range r.events {
		if query.UserID != "" && strings.ToLower(event.UserID) != strings.ToLower(query.UserID) {
			continue
		}
		if query.EventType != "" && event.Type != query.EventType {
			continue
		}
		if query.CorrelationID != "" && event.CorrelationID != query.CorrelationID {
			continue
		}
		if query.From != nil && event.OccurredAt.Before(query.From.UTC()) {
			continue
		}
		if query.To != nil && !event.OccurredAt.Before(query.To.UTC()) {
			continue
		}
		if query.After != nil {
			occurred := event.OccurredAt.UTC()
			if occurred.After(query.After.OccurredAt.UTC()) || (occurred.Equal(query.After.OccurredAt.UTC()) && strings.ToLower(event.ID) >= strings.ToLower(query.After.ID)) {
				continue
			}
		}
		items = append(items, cloneAuthenticationEvent(event))
	}
	r.mu.Unlock()
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.OccurredAt.Equal(right.OccurredAt) {
			return strings.ToLower(left.ID) > strings.ToLower(right.ID)
		}
		return left.OccurredAt.After(right.OccurredAt)
	})
	if len(items) <= query.Limit {
		return AuthenticationEventPage{Items: items}, nil
	}
	items = items[:query.Limit]
	next, err := EncodeAuthenticationEventCursor(AuthenticationEventCursor{OccurredAt: items[len(items)-1].OccurredAt, ID: items[len(items)-1].ID})
	if err != nil {
		return AuthenticationEventPage{}, err
	}
	return AuthenticationEventPage{Items: items, NextCursor: next}, nil
}
