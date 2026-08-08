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

var ErrInvalidReportingPrivacyListCursor = errors.New("invalid reporting-privacy pagination cursor")

type ReportingPrivacyPage struct {
	Items      []ReportingPrivacyPolicy `json:"items"`
	NextCursor string                   `json:"nextCursor,omitempty"`
}
type reportingPrivacyListCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}
type reportingPrivacyListPageStore interface {
	ListReportingPrivacyPage(context.Context, string, int, *time.Time, string) ([]ReportingPrivacyPolicy, error)
}

func decodeReportingPrivacyListCursor(value string) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 || len(raw) > 1024 {
		return nil, "", ErrInvalidReportingPrivacyListCursor
	}
	var c reportingPrivacyListCursor
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return nil, "", ErrInvalidReportingPrivacyListCursor
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) || c.CreatedAt.IsZero() || strings.TrimSpace(c.ID) == "" {
		return nil, "", ErrInvalidReportingPrivacyListCursor
	}
	at := c.CreatedAt.UTC()
	return &at, strings.TrimSpace(c.ID), nil
}
func encodeReportingPrivacyListCursor(at time.Time, id string) (string, error) {
	raw, err := json.Marshal(reportingPrivacyListCursor{CreatedAt: at.UTC(), ID: strings.TrimSpace(id)})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *ReportingPrivacyAdministration) ListPage(ctx context.Context, org string, limit int, cursor string) (ReportingPrivacyPage, error) {
	if s == nil || s.Store == nil {
		return ReportingPrivacyPage{}, errors.New("reporting privacy service is not configured")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	store, ok := s.Store.(reportingPrivacyListPageStore)
	if !ok {
		return ReportingPrivacyPage{}, errors.New("reporting-privacy pagination is unavailable")
	}
	before, beforeID, err := decodeReportingPrivacyListCursor(cursor)
	if err != nil {
		return ReportingPrivacyPage{}, err
	}
	items, err := store.ListReportingPrivacyPage(ctx, strings.TrimSpace(org), limit+1, before, beforeID)
	if err != nil {
		return ReportingPrivacyPage{}, err
	}
	page := ReportingPrivacyPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeReportingPrivacyListCursor(last.CreatedAt, last.ID)
	return page, err
}
