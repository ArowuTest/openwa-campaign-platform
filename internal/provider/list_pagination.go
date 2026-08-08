package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidDefinitionListCursor = errors.New("invalid provider-definition pagination cursor")

type DefinitionPage struct {
	Items      []Definition `json:"items"`
	NextCursor string       `json:"nextCursor,omitempty"`
}
type definitionListCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type definitionListPageStore interface {
	ListDefinitionPage(context.Context, int, *time.Time, string) ([]Definition, error)
}

func decodeDefinitionListCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidDefinitionListCursor
	}
	var c definitionListCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return nil, "", ErrInvalidDefinitionListCursor
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) || c.CreatedAt.IsZero() || strings.TrimSpace(c.ID) == "" {
		return nil, "", ErrInvalidDefinitionListCursor
	}
	at := c.CreatedAt.UTC()
	return &at, strings.TrimSpace(c.ID), nil
}
func encodeDefinitionListCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(definitionListCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *Service) ListPage(ctx context.Context, limit int, cursor string) (DefinitionPage, error) {
	if s == nil || s.Store == nil {
		return DefinitionPage{}, errors.New("provider capability store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := s.Store.(definitionListPageStore)
	if !ok {
		return DefinitionPage{}, errors.New("provider-definition pagination is unavailable")
	}
	before, beforeID, err := decodeDefinitionListCursor(cursor)
	if err != nil {
		return DefinitionPage{}, err
	}
	items, err := store.ListDefinitionPage(ctx, limit+1, before, beforeID)
	if err != nil {
		return DefinitionPage{}, err
	}
	page := DefinitionPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeDefinitionListCursor(last.CreatedAt, last.ID)
	return page, err
}
