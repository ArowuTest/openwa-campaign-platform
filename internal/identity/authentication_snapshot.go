package identity

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// AuthenticationEventSnapshotRepository captures a complete, bounded redacted
// set in one consistent read. Implementations must not assemble this set using
// independent page reads: late commits and backdated events could then enter it.
type AuthenticationEventSnapshotRepository interface {
	SnapshotAuthenticationEvents(context.Context, AuthenticationEventQuery, int) ([]AuthenticationEvent, error)
}

const MaxAuthenticationEventSnapshotRows = 100000

var ErrAuthenticationEventSnapshotTooLarge = errors.New("authentication event snapshot row limit exceeded")

func ValidateAuthenticationEventSnapshotQuery(query AuthenticationEventQuery, maximum int) error {
	if query.After != nil || maximum <= 0 || maximum > MaxAuthenticationEventSnapshotRows {
		return ErrInvalidAuthenticationEventQuery
	}
	query.Limit = 1
	return ValidateAuthenticationEventQuery(query)
}

func (r *MemoryEventRecorder) SnapshotAuthenticationEvents(ctx context.Context, query AuthenticationEventQuery, maximum int) ([]AuthenticationEvent, error) {
	if err := ValidateAuthenticationEventSnapshotQuery(query, maximum); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]AuthenticationEvent, 0)
	for _, event := range r.events {
		if query.UserID != "" && !strings.EqualFold(event.UserID, strings.TrimSpace(query.UserID)) ||
			query.EventType != "" && event.Type != query.EventType ||
			query.CorrelationID != "" && event.CorrelationID != strings.TrimSpace(query.CorrelationID) ||
			query.From != nil && event.OccurredAt.Before(query.From.UTC()) ||
			query.To != nil && !event.OccurredAt.Before(query.To.UTC()) {
			continue
		}
		if len(items) == maximum {
			return nil, ErrAuthenticationEventSnapshotTooLarge
		}
		items = append(items, AuthenticationEvent{
			ID: event.ID, UserID: event.UserID, Type: event.Type,
			CorrelationID: event.CorrelationID, OccurredAt: event.OccurredAt.UTC(),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return strings.ToLower(items[i].ID) > strings.ToLower(items[j].ID)
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
	return items, ctx.Err()
}
