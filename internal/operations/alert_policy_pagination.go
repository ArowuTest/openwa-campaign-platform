package operations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrInvalidAlertPolicyCursor = errors.New("invalid alert-policy pagination cursor")

type AlertPolicyPage struct {
	Items      []AlertPolicy `json:"items"`
	NextCursor string        `json:"nextCursor,omitempty"`
}
type alertPolicyCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type alertPolicyPageStore interface {
	ListAlertPolicyPage(context.Context, AlertPolicyStatus, int, *time.Time, string) ([]AlertPolicy, error)
}

func decodeAlertPolicyCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidAlertPolicyCursor
	}
	var c alertPolicyCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return nil, "", ErrInvalidAlertPolicyCursor
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) || c.CreatedAt.IsZero() || strings.TrimSpace(c.ID) == "" {
		return nil, "", ErrInvalidAlertPolicyCursor
	}
	at := c.CreatedAt.UTC()
	return &at, strings.TrimSpace(c.ID), nil
}
func encodeAlertPolicyCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(alertPolicyCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (a *AlertAdministration) ListPoliciesPage(ctx context.Context, status AlertPolicyStatus, limit int, cursor string) (AlertPolicyPage, error) {
	if a == nil || a.Store == nil {
		return AlertPolicyPage{}, errors.New("alert store is required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := a.Store.(alertPolicyPageStore)
	if !ok {
		return AlertPolicyPage{}, errors.New("alert-policy pagination is unavailable")
	}
	before, beforeID, err := decodeAlertPolicyCursor(cursor)
	if err != nil {
		return AlertPolicyPage{}, err
	}
	items, err := store.ListAlertPolicyPage(ctx, status, limit+1, before, beforeID)
	if err != nil {
		return AlertPolicyPage{}, err
	}
	page := AlertPolicyPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeAlertPolicyCursor(last.CreatedAt, last.ID)
	return page, err
}
