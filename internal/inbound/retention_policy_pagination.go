package inbound

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidRetentionPolicyCursor = errors.New("invalid inbound-retention-policy pagination cursor")

type RetentionPolicyPage struct {
	Items      []RetentionPolicy `json:"items"`
	NextCursor string            `json:"nextCursor,omitempty"`
}
type retentionPolicyCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type retentionPolicyPageStore interface {
	ListRetentionPolicyPage(context.Context, int, *time.Time, string) ([]RetentionPolicy, error)
}

func decodeRetentionPolicyCursor(v string) (*time.Time, string, error) {
	if strings.TrimSpace(v) == "" {
		return nil, "", nil
	}
	raw, e := base64.RawURLEncoding.DecodeString(v)
	if e != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidRetentionPolicyCursor
	}
	var c retentionPolicyCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return nil, "", ErrInvalidRetentionPolicyCursor
	}
	var x any
	if e = d.Decode(&x); !errors.Is(e, io.EOF) || c.CreatedAt.IsZero() || strings.TrimSpace(c.ID) == "" {
		return nil, "", ErrInvalidRetentionPolicyCursor
	}
	t := c.CreatedAt.UTC()
	return &t, strings.TrimSpace(c.ID), nil
}
func encodeRetentionPolicyCursor(t time.Time, id string) (string, error) {
	raw, e := json.Marshal(retentionPolicyCursor{CreatedAt: t.UTC(), ID: strings.TrimSpace(id)})
	if e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (a *RetentionPolicyAdministration) ListPage(ctx context.Context, limit int, cursor string) (RetentionPolicyPage, error) {
	if a == nil || a.Store == nil {
		return RetentionPolicyPage{}, errors.New("inbound retention policy store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	s, ok := a.Store.(retentionPolicyPageStore)
	if !ok {
		return RetentionPolicyPage{}, errors.New("inbound-retention-policy pagination is unavailable")
	}
	before, bid, e := decodeRetentionPolicyCursor(cursor)
	if e != nil {
		return RetentionPolicyPage{}, e
	}
	items, e := s.ListRetentionPolicyPage(ctx, limit+1, before, bid)
	if e != nil {
		return RetentionPolicyPage{}, e
	}
	p := RetentionPolicyPage{Items: items}
	if len(items) <= limit {
		return p, nil
	}
	p.Items = items[:limit]
	last := p.Items[len(p.Items)-1]
	p.NextCursor, e = encodeRetentionPolicyCursor(last.CreatedAt, last.ID)
	return p, e
}
