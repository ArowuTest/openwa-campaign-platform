package retention

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidPolicyCursor = errors.New("invalid retention-policy pagination cursor")

type PolicyPage struct {
	Items      []Policy `json:"items"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

type policyCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type policyPageStore interface {
	ListPolicyPage(context.Context, Status, int, *time.Time, string) ([]Policy, error)
}

func decodePolicyCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidPolicyCursor
	}
	var c policyCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return nil, "", ErrInvalidPolicyCursor
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) || c.CreatedAt.IsZero() || strings.TrimSpace(c.ID) == "" {
		return nil, "", ErrInvalidPolicyCursor
	}
	at := c.CreatedAt.UTC()
	return &at, strings.TrimSpace(c.ID), nil
}
func encodePolicyCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(policyCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (a *Administration) ListPage(ctx context.Context, status Status, limit int, cursor string) (PolicyPage, error) {
	if a == nil || a.Store == nil {
		return PolicyPage{}, errors.New("retention store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := a.Store.(policyPageStore)
	if !ok {
		return PolicyPage{}, errors.New("retention-policy pagination is unavailable")
	}
	before, beforeID, err := decodePolicyCursor(cursor)
	if err != nil {
		return PolicyPage{}, err
	}
	items, err := store.ListPolicyPage(ctx, status, limit+1, before, beforeID)
	if err != nil {
		return PolicyPage{}, err
	}
	page := PolicyPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodePolicyCursor(last.CreatedAt, last.ID)
	return page, err
}
