package privacy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidEventCursor = errors.New("invalid privacy-event pagination cursor")

type EventPage struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}
type LegalHoldEventPage struct {
	Items      []LegalHoldEvent `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
}
type privacyEventCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

type caseEventPageRepository interface {
	ListEventPage(context.Context, string, int, *time.Time, string) ([]Event, error)
}
type legalHoldEventPageRepository interface {
	ListLegalHoldEventPage(context.Context, string, int, *time.Time, string) ([]LegalHoldEvent, error)
}

func decodePrivacyEventCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidEventCursor
	}
	var c privacyEventCursor
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
func encodePrivacyEventCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(privacyEventCursor{OccurredAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *Service) EventsPage(ctx context.Context, id string, limit int, cursor string) (EventPage, error) {
	if s == nil || s.Repository == nil {
		return EventPage{}, errors.New("privacy service is not configured")
	}
	repo, ok := s.Repository.(caseEventPageRepository)
	if !ok {
		return EventPage{}, errors.New("privacy-case event pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before, beforeID, err := decodePrivacyEventCursor(cursor)
	if err != nil {
		return EventPage{}, err
	}
	items, err := repo.ListEventPage(ctx, strings.TrimSpace(id), limit+1, before, beforeID)
	if err != nil {
		return EventPage{}, err
	}
	page := EventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodePrivacyEventCursor(last.OccurredAt, last.ID)
	return page, err
}
func (s *Service) LegalHoldEventsPage(ctx context.Context, id string, limit int, cursor string) (LegalHoldEventPage, error) {
	if s == nil || s.Repository == nil {
		return LegalHoldEventPage{}, errors.New("privacy service is not configured")
	}
	repo, ok := s.Repository.(legalHoldEventPageRepository)
	if !ok {
		return LegalHoldEventPage{}, errors.New("privacy legal-hold event pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before, beforeID, err := decodePrivacyEventCursor(cursor)
	if err != nil {
		return LegalHoldEventPage{}, err
	}
	items, err := repo.ListLegalHoldEventPage(ctx, strings.TrimSpace(id), limit+1, before, beforeID)
	if err != nil {
		return LegalHoldEventPage{}, err
	}
	page := LegalHoldEventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodePrivacyEventCursor(last.OccurredAt, last.ID)
	return page, err
}
