package jobs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidJobCursor = errors.New("invalid job pagination cursor")

type Page struct {
	Items      []Job  `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type jobPageCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type administrationPageRepository interface {
	ListPage(context.Context, Query, *time.Time, string) ([]Job, error)
}

func encodeJobCursor(value jobPageCursor) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeJobCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidJobCursor
	}
	var cursor jobPageCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidJobCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, "", ErrInvalidJobCursor
	}
	cursor.ID = strings.TrimSpace(cursor.ID)
	if cursor.CreatedAt.IsZero() || cursor.ID == "" {
		return nil, "", ErrInvalidJobCursor
	}
	valueTime := cursor.CreatedAt.UTC()
	return &valueTime, cursor.ID, nil
}

func (s *AdministrationService) ListPage(ctx context.Context, query Query, cursor string) (Page, error) {
	if s == nil || s.Repository == nil {
		return Page{}, errors.New("job administration repository is required")
	}
	repository, ok := s.Repository.(administrationPageRepository)
	if !ok {
		return Page{}, errors.New("job pagination is unavailable")
	}
	if query.Limit <= 0 || query.Limit > 500 {
		query.Limit = 100
	}
	before, beforeID, err := decodeJobCursor(cursor)
	if err != nil {
		return Page{}, err
	}
	fetch := query
	fetch.Limit = query.Limit + 1
	items, err := repository.ListPage(ctx, fetch, before, beforeID)
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: items}
	if len(items) <= query.Limit {
		return page, nil
	}
	page.Items = items[:query.Limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeJobCursor(jobPageCursor{CreatedAt: last.CreatedAt.UTC(), ID: last.ID})
	return page, err
}

var ErrInvalidAdministrationEventCursor = errors.New("invalid job administration-event pagination cursor")

type AdministrationEventPage struct {
	Items      []AdministrationEvent `json:"items"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

type administrationEventCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

type administrationEventPageRepository interface {
	ListAdministrationEventPage(context.Context, string, int, *time.Time, string) ([]AdministrationEvent, error)
}

func encodeAdministrationEventCursor(value administrationEventCursor) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeAdministrationEventCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidAdministrationEventCursor
	}
	var cursor administrationEventCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidAdministrationEventCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, "", ErrInvalidAdministrationEventCursor
	}
	cursor.ID = strings.TrimSpace(cursor.ID)
	if cursor.OccurredAt.IsZero() || cursor.ID == "" {
		return nil, "", ErrInvalidAdministrationEventCursor
	}
	at := cursor.OccurredAt.UTC()
	return &at, cursor.ID, nil
}

func (s *AdministrationService) EventsPage(ctx context.Context, jobID string, limit int, cursor string) (AdministrationEventPage, error) {
	if s == nil || s.Repository == nil {
		return AdministrationEventPage{}, errors.New("job administration repository is required")
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return AdministrationEventPage{}, errors.New("job id is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	repository, ok := s.Repository.(administrationEventPageRepository)
	if !ok {
		return AdministrationEventPage{}, errors.New("job administration-event pagination is unavailable")
	}
	before, beforeID, err := decodeAdministrationEventCursor(cursor)
	if err != nil {
		return AdministrationEventPage{}, err
	}
	items, err := repository.ListAdministrationEventPage(ctx, jobID, limit+1, before, beforeID)
	if err != nil {
		return AdministrationEventPage{}, err
	}
	page := AdministrationEventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeAdministrationEventCursor(administrationEventCursor{OccurredAt: last.OccurredAt.UTC(), ID: last.ID})
	return page, err
}
