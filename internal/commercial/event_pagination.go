package commercial

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"campaign-platform/internal/shared/id"
)

var ErrInvalidEventCursor = errors.New("invalid commercial-event pagination cursor")

type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

type eventCursor struct {
	RecordVersion int64  `json:"recordVersion"`
	ID            string `json:"id"`
}

type eventPageStore interface {
	EventPage(context.Context, string, int, int64, string) ([]Event, error)
}

func encodeEventCursor(value eventCursor) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeEventCursor(value string) (int64, string, error) {
	if strings.TrimSpace(value) == "" {
		return 0, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return 0, "", ErrInvalidEventCursor
	}
	var cursor eventCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return 0, "", ErrInvalidEventCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return 0, "", ErrInvalidEventCursor
	}
	cursor.ID = strings.TrimSpace(cursor.ID)
	if cursor.RecordVersion < 1 || !id.IsUUID(cursor.ID) {
		return 0, "", ErrInvalidEventCursor
	}
	return cursor.RecordVersion, cursor.ID, nil
}

func (s *Service) EventsPage(ctx context.Context, identifier string, limit int, cursor string) (EventPage, error) {
	if s == nil || s.Store == nil {
		return EventPage{}, errors.New("commercial store is required")
	}
	store, ok := s.Store.(eventPageStore)
	if !ok {
		return EventPage{}, errors.New("commercial-event pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	afterVersion, afterID, err := decodeEventCursor(cursor)
	if err != nil {
		return EventPage{}, err
	}
	items, err := store.EventPage(ctx, strings.TrimSpace(identifier), limit+1, afterVersion, afterID)
	if err != nil {
		return EventPage{}, err
	}
	page := EventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeEventCursor(eventCursor{RecordVersion: last.RecordVersion, ID: last.ID})
	return page, err
}
