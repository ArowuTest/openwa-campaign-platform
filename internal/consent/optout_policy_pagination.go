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

var ErrInvalidOptOutPolicyCursor = errors.New("invalid opt-out-policy pagination cursor")

type OptOutPolicyPage struct {
	Items      []GovernedOptOutPolicy `json:"items"`
	NextCursor string                 `json:"nextCursor,omitempty"`
}
type optOutPolicyCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type optOutPolicyPageStore interface {
	ListOptOutPolicyPage(context.Context, int, *time.Time, string) ([]GovernedOptOutPolicy, error)
}

func decodeOptOutPolicyCursor(v string) (*time.Time, string, error) {
	if strings.TrimSpace(v) == "" {
		return nil, "", nil
	}
	raw, e := base64.RawURLEncoding.DecodeString(v)
	if e != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidOptOutPolicyCursor
	}
	var c optOutPolicyCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return nil, "", ErrInvalidOptOutPolicyCursor
	}
	var x any
	if e = d.Decode(&x); !errors.Is(e, io.EOF) || c.CreatedAt.IsZero() || strings.TrimSpace(c.ID) == "" {
		return nil, "", ErrInvalidOptOutPolicyCursor
	}
	t := c.CreatedAt.UTC()
	return &t, strings.TrimSpace(c.ID), nil
}
func encodeOptOutPolicyCursor(t time.Time, id string) (string, error) {
	raw, e := json.Marshal(optOutPolicyCursor{CreatedAt: t.UTC(), ID: strings.TrimSpace(id)})
	if e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *OptOutPolicyAdministration) ListPage(ctx context.Context, limit int, cursor string) (OptOutPolicyPage, error) {
	if s == nil || s.Store == nil {
		return OptOutPolicyPage{}, errors.New("opt-out policy store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	r, ok := s.Store.(optOutPolicyPageStore)
	if !ok {
		return OptOutPolicyPage{}, errors.New("opt-out-policy pagination is unavailable")
	}
	before, bid, e := decodeOptOutPolicyCursor(cursor)
	if e != nil {
		return OptOutPolicyPage{}, e
	}
	items, e := r.ListOptOutPolicyPage(ctx, limit+1, before, bid)
	if e != nil {
		return OptOutPolicyPage{}, e
	}
	p := OptOutPolicyPage{Items: items}
	if len(items) <= limit {
		return p, nil
	}
	p.Items = items[:limit]
	last := p.Items[len(p.Items)-1]
	p.NextCursor, e = encodeOptOutPolicyCursor(last.CreatedAt, last.ID)
	return p, e
}
