package organisation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidPolicyListCursor = errors.New("invalid organisation-policy pagination cursor")

type PolicyPage struct {
	Items      []Policy `json:"items"`
	NextCursor string   `json:"nextCursor,omitempty"`
}
type policyListCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type policyListPageStore interface {
	ListPolicyPage(context.Context, string, int, *time.Time, string) ([]Policy, error)
}

func decodePolicyListCursor(v string) (*time.Time, string, error) {
	if strings.TrimSpace(v) == "" {
		return nil, "", nil
	}
	raw, e := base64.RawURLEncoding.DecodeString(v)
	if e != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidPolicyListCursor
	}
	var c policyListCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return nil, "", ErrInvalidPolicyListCursor
	}
	var x any
	if e = d.Decode(&x); !errors.Is(e, io.EOF) || c.CreatedAt.IsZero() || strings.TrimSpace(c.ID) == "" {
		return nil, "", ErrInvalidPolicyListCursor
	}
	t := c.CreatedAt.UTC()
	return &t, strings.TrimSpace(c.ID), nil
}
func encodePolicyListCursor(t time.Time, id string) (string, error) {
	raw, e := json.Marshal(policyListCursor{CreatedAt: t.UTC(), ID: strings.TrimSpace(id)})
	if e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (a *PolicyAdministration) ListPage(ctx context.Context, org string, limit int, cursor string) (PolicyPage, error) {
	if a == nil || a.Store == nil {
		return PolicyPage{}, errors.New("organisation policy store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	s, ok := a.Store.(policyListPageStore)
	if !ok {
		return PolicyPage{}, errors.New("organisation-policy pagination is unavailable")
	}
	before, bid, e := decodePolicyListCursor(cursor)
	if e != nil {
		return PolicyPage{}, e
	}
	items, e := s.ListPolicyPage(ctx, org, limit+1, before, bid)
	if e != nil {
		return PolicyPage{}, e
	}
	p := PolicyPage{Items: items}
	if len(items) <= limit {
		return p, nil
	}
	p.Items = items[:limit]
	last := p.Items[len(p.Items)-1]
	p.NextCursor, e = encodePolicyListCursor(last.CreatedAt, last.ID)
	return p, e
}
