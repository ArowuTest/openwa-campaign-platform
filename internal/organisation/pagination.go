package organisation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var ErrInvalidEventCursor = errors.New("invalid organisation-event pagination cursor")

type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

type eventCursor struct {
	Version int64 `json:"version"`
}

type eventPageRepository interface {
	ListEventPage(context.Context, string, int, int64) ([]Event, error)
}

func decodeEventCursor(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 256 {
		return 0, ErrInvalidEventCursor
	}
	var cursor eventCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return 0, ErrInvalidEventCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.Version <= 0 {
		return 0, ErrInvalidEventCursor
	}
	return cursor.Version, nil
}

func encodeEventCursor(version int64) (string, error) {
	raw, err := json.Marshal(eventCursor{Version: version})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) ListEventsPage(ctx context.Context, organisationID string, limit int, cursor string) (EventPage, error) {
	if s == nil || s.repository == nil {
		return EventPage{}, errors.New("organisation repository is required")
	}
	organisationID = strings.TrimSpace(organisationID)
	if organisationID == "" {
		return EventPage{}, ErrNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	repository, ok := s.repository.(eventPageRepository)
	if !ok {
		return EventPage{}, errors.New("organisation-event pagination is unavailable")
	}
	afterVersion, err := decodeEventCursor(cursor)
	if err != nil {
		return EventPage{}, err
	}
	items, err := repository.ListEventPage(ctx, organisationID, limit+1, afterVersion)
	if err != nil {
		return EventPage{}, err
	}
	page := EventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	page.NextCursor, err = encodeEventCursor(page.Items[len(page.Items)-1].Version)
	return page, err
}
