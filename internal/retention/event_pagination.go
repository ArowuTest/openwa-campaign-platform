package retention

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidEventCursor = errors.New("invalid retention-event pagination cursor")

type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}
type retentionEventCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}
type eventPageStore interface {
	ListEventPage(context.Context, string, int, *time.Time, string) ([]Event, error)
}

func decodeRetentionEventCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidEventCursor
	}
	var c retentionEventCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return nil, "", ErrInvalidEventCursor
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) || c.OccurredAt.IsZero() || strings.TrimSpace(c.ID) == "" {
		return nil, "", ErrInvalidEventCursor
	}
	at := c.OccurredAt.UTC()
	return &at, strings.TrimSpace(c.ID), nil
}
func encodeRetentionEventCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(retentionEventCursor{OccurredAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (a *Administration) EventsPage(ctx context.Context, id string, limit int, cursor string) (EventPage, error) {
	if a == nil || a.Store == nil {
		return EventPage{}, errors.New("retention store is required")
	}
	store, ok := a.Store.(eventPageStore)
	if !ok {
		return EventPage{}, errors.New("retention-event pagination is unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	before, beforeID, err := decodeRetentionEventCursor(cursor)
	if err != nil {
		return EventPage{}, err
	}
	items, err := store.ListEventPage(ctx, strings.TrimSpace(id), limit+1, before, beforeID)
	if err != nil {
		return EventPage{}, err
	}
	page := EventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeRetentionEventCursor(last.OccurredAt, last.ID)
	return page, err
}
