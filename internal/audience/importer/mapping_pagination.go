package importer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidMappingEventCursor = errors.New("invalid mapping-event pagination cursor")

type MappingEventPage struct {
	Items      []MappingEvent `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type mappingEventCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

type mappingEventPageStore interface {
	ListMappingEventPage(context.Context, string, int, *time.Time, string) ([]MappingEvent, error)
}

func decodeMappingEventCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidMappingEventCursor
	}
	var cursor mappingEventCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, "", ErrInvalidMappingEventCursor
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || cursor.OccurredAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return nil, "", ErrInvalidMappingEventCursor
	}
	at := cursor.OccurredAt.UTC()
	return &at, strings.TrimSpace(cursor.ID), nil
}

func encodeMappingEventCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(mappingEventCursor{OccurredAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *MappingAdministration) EventsPage(ctx context.Context, id string, limit int, cursor string) (MappingEventPage, error) {
	if s == nil || s.Store == nil {
		return MappingEventPage{}, errors.New("mapping store is required")
	}
	store, ok := s.Store.(mappingEventPageStore)
	if !ok {
		return MappingEventPage{}, errors.New("mapping-event pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before, beforeID, err := decodeMappingEventCursor(cursor)
	if err != nil {
		return MappingEventPage{}, err
	}
	items, err := store.ListMappingEventPage(ctx, strings.TrimSpace(id), limit+1, before, beforeID)
	if err != nil {
		return MappingEventPage{}, err
	}
	page := MappingEventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeMappingEventCursor(last.OccurredAt, last.ID)
	return page, err
}
