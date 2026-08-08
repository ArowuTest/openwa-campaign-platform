package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var ErrInvalidEventCursor = errors.New("invalid provider-event pagination cursor")

type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}
type eventPageCursor struct {
	ID int64 `json:"id"`
}
type eventPageStore interface {
	ListEventPage(context.Context, string, int, int64) ([]Event, error)
}

func decodeEventPageCursor(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 256 {
		return 0, ErrInvalidEventCursor
	}
	var c eventPageCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return 0, ErrInvalidEventCursor
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) || c.ID <= 0 {
		return 0, ErrInvalidEventCursor
	}
	return c.ID, nil
}
func encodeEventPageCursor(id int64) (string, error) {
	raw, err := json.Marshal(eventPageCursor{ID: id})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *Service) EventsPage(ctx context.Context, definitionID string, limit int, cursor string) (EventPage, error) {
	if s == nil || s.Store == nil {
		return EventPage{}, errors.New("provider capability store is required")
	}
	store, ok := s.Store.(eventPageStore)
	if !ok {
		return EventPage{}, errors.New("provider-event pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before, err := decodeEventPageCursor(cursor)
	if err != nil {
		return EventPage{}, err
	}
	items, err := store.ListEventPage(ctx, strings.TrimSpace(definitionID), limit+1, before)
	if err != nil {
		return EventPage{}, err
	}
	page := EventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	page.NextCursor, err = encodeEventPageCursor(page.Items[len(page.Items)-1].ID)
	return page, err
}
