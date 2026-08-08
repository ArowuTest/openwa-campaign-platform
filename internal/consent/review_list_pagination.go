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

var ErrInvalidReviewListCursor = errors.New("invalid consent-review pagination cursor")

type ReviewPage struct {
	Items      []Review `json:"items"`
	NextCursor string   `json:"nextCursor,omitempty"`
}
type reviewListCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type reviewListPageRepository interface {
	ListReviewPage(context.Context, string, int, *time.Time, string) ([]Review, error)
}

func decodeReviewListCursor(v string) (*time.Time, string, error) {
	if strings.TrimSpace(v) == "" {
		return nil, "", nil
	}
	raw, e := base64.RawURLEncoding.DecodeString(v)
	if e != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidReviewListCursor
	}
	var c reviewListCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return nil, "", ErrInvalidReviewListCursor
	}
	var x any
	if e = d.Decode(&x); !errors.Is(e, io.EOF) || c.CreatedAt.IsZero() || strings.TrimSpace(c.ID) == "" {
		return nil, "", ErrInvalidReviewListCursor
	}
	t := c.CreatedAt.UTC()
	return &t, strings.TrimSpace(c.ID), nil
}
func encodeReviewListCursor(t time.Time, id string) (string, error) {
	raw, e := json.Marshal(reviewListCursor{CreatedAt: t.UTC(), ID: strings.TrimSpace(id)})
	if e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *Service) ListPage(ctx context.Context, org string, limit int, cursor string) (ReviewPage, error) {
	if s == nil || s.repository == nil {
		return ReviewPage{}, errors.New("consent review repository is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	r, ok := s.repository.(reviewListPageRepository)
	if !ok {
		return ReviewPage{}, errors.New("consent-review pagination is unavailable")
	}
	before, bid, e := decodeReviewListCursor(cursor)
	if e != nil {
		return ReviewPage{}, e
	}
	items, e := r.ListReviewPage(ctx, strings.TrimSpace(org), limit+1, before, bid)
	if e != nil {
		return ReviewPage{}, e
	}
	p := ReviewPage{Items: items}
	if len(items) <= limit {
		return p, nil
	}
	p.Items = items[:limit]
	last := p.Items[len(p.Items)-1]
	p.NextCursor, e = encodeReviewListCursor(last.CreatedAt, last.ID)
	return p, e
}
