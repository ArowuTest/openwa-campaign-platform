package commercial

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidListCursor = errors.New("invalid commercial pagination cursor")

type Page struct {
	Items      []Record `json:"items"`
	NextCursor string   `json:"nextCursor,omitempty"`
}
type listCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type listPageStore interface {
	ListPage(context.Context, string, int, *time.Time, string) ([]Record, error)
}

func decodeListCursor(v string) (*time.Time, string, error) {
	if strings.TrimSpace(v) == "" {
		return nil, "", nil
	}
	raw, e := base64.RawURLEncoding.DecodeString(v)
	if e != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidListCursor
	}
	var c listCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return nil, "", ErrInvalidListCursor
	}
	var x any
	if e = d.Decode(&x); !errors.Is(e, io.EOF) || c.CreatedAt.IsZero() || strings.TrimSpace(c.ID) == "" {
		return nil, "", ErrInvalidListCursor
	}
	t := c.CreatedAt.UTC()
	return &t, strings.TrimSpace(c.ID), nil
}
func encodeListCursor(t time.Time, id string) (string, error) {
	raw, e := json.Marshal(listCursor{CreatedAt: t.UTC(), ID: strings.TrimSpace(id)})
	if e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *Service) ListPage(ctx context.Context, org string, limit int, cursor string) (Page, error) {
	if s == nil || s.Store == nil {
		return Page{}, errors.New("commercial store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	r, ok := s.Store.(listPageStore)
	if !ok {
		return Page{}, errors.New("commercial pagination is unavailable")
	}
	before, bid, e := decodeListCursor(cursor)
	if e != nil {
		return Page{}, e
	}
	items, e := r.ListPage(ctx, strings.TrimSpace(org), limit+1, before, bid)
	if e != nil {
		return Page{}, e
	}
	p := Page{Items: items}
	if len(items) <= limit {
		return p, nil
	}
	p.Items = items[:limit]
	last := p.Items[len(p.Items)-1]
	p.NextCursor, e = encodeListCursor(last.CreatedAt, last.ID)
	return p, e
}
