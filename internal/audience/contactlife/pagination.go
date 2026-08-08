package contactlife

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidLifecycleEventCursor = errors.New("invalid contact-lifecycle event cursor")

type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

type lifecycleEventCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

type eventPageRepository interface {
	ListEventPage(context.Context, string, int, *time.Time, string) ([]Event, error)
}

func decodeLifecycleEventCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidLifecycleEventCursor
	}
	var cursor lifecycleEventCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidLifecycleEventCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.OccurredAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidLifecycleEventCursor
	}
	at := cursor.OccurredAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeLifecycleEventCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(lifecycleEventCursor{OccurredAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) EventsPage(ctx context.Context, contactID string, limit int, cursor string) (EventPage, error) {
	if s == nil || s.Repository == nil {
		return EventPage{}, errors.New("contact lifecycle service is not configured")
	}
	contactID = strings.TrimSpace(contactID)
	if contactID == "" {
		return EventPage{}, ErrNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	repository, ok := s.Repository.(eventPageRepository)
	if !ok {
		return EventPage{}, errors.New("contact-lifecycle pagination is unavailable")
	}
	before, beforeID, err := decodeLifecycleEventCursor(cursor)
	if err != nil {
		return EventPage{}, err
	}
	items, err := repository.ListEventPage(ctx, contactID, limit+1, before, beforeID)
	if err != nil {
		return EventPage{}, err
	}
	page := EventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeLifecycleEventCursor(last.OccurredAt, last.ID)
	return page, err
}
