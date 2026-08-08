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

var ErrInvalidMappingListCursor = errors.New("invalid mapping pagination cursor")

type MappingPage struct {
	Items      []MappingDefinition `json:"items"`
	NextCursor string              `json:"nextCursor,omitempty"`
}
type mappingListCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type mappingListPageStore interface {
	ListMappingPage(context.Context, string, string, int, *time.Time, string) ([]MappingDefinition, error)
}

func decodeMappingListCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidMappingListCursor
	}
	var c mappingListCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return nil, "", ErrInvalidMappingListCursor
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) || c.CreatedAt.IsZero() || strings.TrimSpace(c.ID) == "" {
		return nil, "", ErrInvalidMappingListCursor
	}
	at := c.CreatedAt.UTC()
	return &at, strings.TrimSpace(c.ID), nil
}
func encodeMappingListCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(mappingListCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *MappingAdministration) ListPage(ctx context.Context, org, source string, limit int, cursor string) (MappingPage, error) {
	if s == nil || s.Store == nil {
		return MappingPage{}, errors.New("mapping administration unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := s.Store.(mappingListPageStore)
	if !ok {
		return MappingPage{}, errors.New("mapping pagination is unavailable")
	}
	before, beforeID, err := decodeMappingListCursor(cursor)
	if err != nil {
		return MappingPage{}, err
	}
	items, err := store.ListMappingPage(ctx, strings.TrimSpace(org), strings.TrimSpace(source), limit+1, before, beforeID)
	if err != nil {
		return MappingPage{}, err
	}
	page := MappingPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeMappingListCursor(last.CreatedAt, last.ID)
	return page, err
}
