package platformpolicy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidEventCursor = errors.New("invalid platform-policy event pagination cursor")

type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

type eventCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

type configurationEventPageStore interface {
	ListConfigurationEventPage(context.Context, string, int, *time.Time, string) ([]Event, error)
}
type maintenanceEventPageStore interface {
	ListMaintenanceEventPage(context.Context, string, int, *time.Time, string) ([]Event, error)
}

func decodeEventCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidEventCursor
	}
	var cursor eventCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidEventCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.OccurredAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidEventCursor
	}
	at := cursor.OccurredAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}
func encodeEventCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(eventCursor{OccurredAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func eventPage(items []Event, limit int) (EventPage, error) {
	page := EventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	var err error
	page.NextCursor, err = encodeEventCursor(last.OccurredAt, last.ID)
	return page, err
}
func (a *ConfigurationAdministration) EventsPage(ctx context.Context, id string, limit int, cursor string) (EventPage, error) {
	if a == nil || a.Store == nil {
		return EventPage{}, errors.New("configuration store is required")
	}
	store, ok := a.Store.(configurationEventPageStore)
	if !ok {
		return EventPage{}, errors.New("configuration-event pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before, beforeID, err := decodeEventCursor(cursor)
	if err != nil {
		return EventPage{}, err
	}
	items, err := store.ListConfigurationEventPage(ctx, strings.TrimSpace(id), limit+1, before, beforeID)
	if err != nil {
		return EventPage{}, err
	}
	return eventPage(items, limit)
}
func (a *MaintenanceAdministration) EventsPage(ctx context.Context, id string, limit int, cursor string) (EventPage, error) {
	if a == nil || a.Store == nil {
		return EventPage{}, errors.New("maintenance store is required")
	}
	store, ok := a.Store.(maintenanceEventPageStore)
	if !ok {
		return EventPage{}, errors.New("maintenance-event pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before, beforeID, err := decodeEventCursor(cursor)
	if err != nil {
		return EventPage{}, err
	}
	items, err := store.ListMaintenanceEventPage(ctx, strings.TrimSpace(id), limit+1, before, beforeID)
	if err != nil {
		return EventPage{}, err
	}
	return eventPage(items, limit)
}
