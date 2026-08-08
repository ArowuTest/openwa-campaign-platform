package consent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidConsentEventCursor = errors.New("invalid consent-event pagination cursor")

type ConsentEventPage struct {
	Items      []ConsentEvent `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type consentEventCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

type consentEventPageRepository interface {
	EventPage(context.Context, string, int, *time.Time, string) ([]ConsentEvent, error)
}

func encodeConsentEventCursor(value consentEventCursor) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeConsentEventCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidConsentEventCursor
	}
	var cursor consentEventCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidConsentEventCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, "", ErrInvalidConsentEventCursor
	}
	cursor.ID = strings.TrimSpace(cursor.ID)
	if cursor.OccurredAt.IsZero() || cursor.ID == "" {
		return nil, "", ErrInvalidConsentEventCursor
	}
	at := cursor.OccurredAt.UTC()
	return &at, cursor.ID, nil
}

func (s *LedgerService) EventsPage(ctx context.Context, contactID string, limit int, cursor string) (ConsentEventPage, error) {
	if s == nil || s.repository == nil {
		return ConsentEventPage{}, errors.New("consent ledger repository is required")
	}
	repository, ok := s.repository.(consentEventPageRepository)
	if !ok {
		return ConsentEventPage{}, errors.New("consent-event pagination is unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	before, beforeID, err := decodeConsentEventCursor(cursor)
	if err != nil {
		return ConsentEventPage{}, err
	}
	items, err := repository.EventPage(ctx, strings.TrimSpace(contactID), limit+1, before, beforeID)
	if err != nil {
		return ConsentEventPage{}, err
	}
	page := ConsentEventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeConsentEventCursor(consentEventCursor{OccurredAt: last.OccurredAt.UTC(), ID: last.ID})
	return page, err
}
