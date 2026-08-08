package operations

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidReportingPrivacyEventCursor = errors.New("invalid reporting-privacy-event pagination cursor")
	ErrInvalidAlertPolicyEventCursor      = errors.New("invalid alert-policy-event pagination cursor")
)

type ReportingPrivacyEventPage struct {
	Items      []ReportingPrivacyEvent `json:"items"`
	NextCursor string                  `json:"nextCursor,omitempty"`
}

type AlertPolicyEventPage struct {
	Items      []AlertPolicyEvent `json:"items"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

type governanceEventCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}
type reportingPrivacyEventPageStore interface {
	ListReportingPrivacyEventPage(context.Context, string, int, *time.Time, string) ([]ReportingPrivacyEvent, error)
}

type alertPolicyEventPageStore interface {
	ListAlertPolicyEventPage(context.Context, string, int, *time.Time, string) ([]AlertPolicyEvent, error)
}

func decodeGovernanceEventCursor(value string, invalid error) (*time.Time, string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, "", nil
	}
	var decoded governanceEventCursor
	if err := decodeOperationsCursor(value, &decoded, invalid); err != nil || decoded.OccurredAt.IsZero() || strings.TrimSpace(decoded.ID) == "" {
		return nil, "", invalid
	}
	at := decoded.OccurredAt.UTC()
	return &at, strings.TrimSpace(decoded.ID), nil
}

func encodeGovernanceEventCursor(at time.Time, id string) (string, error) {
	return encodeOperationsCursor(governanceEventCursor{OccurredAt: at.UTC(), ID: strings.TrimSpace(id)})
}
func (s *ReportingPrivacyAdministration) EventsPage(ctx context.Context, id string, limit int, cursor string) (ReportingPrivacyEventPage, error) {
	if s == nil || s.Store == nil {
		return ReportingPrivacyEventPage{}, errors.New("reporting privacy service is not configured")
	}
	store, ok := s.Store.(reportingPrivacyEventPageStore)
	if !ok {
		return ReportingPrivacyEventPage{}, errors.New("reporting-privacy event pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before, beforeID, err := decodeGovernanceEventCursor(cursor, ErrInvalidReportingPrivacyEventCursor)
	if err != nil {
		return ReportingPrivacyEventPage{}, err
	}
	items, err := store.ListReportingPrivacyEventPage(ctx, strings.TrimSpace(id), limit+1, before, beforeID)
	if err != nil {
		return ReportingPrivacyEventPage{}, err
	}
	page := ReportingPrivacyEventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeGovernanceEventCursor(last.OccurredAt, last.ID)
	return page, err
}
func (a *AlertAdministration) PolicyEventsPage(ctx context.Context, policyID string, limit int, cursor string) (AlertPolicyEventPage, error) {
	if a == nil || a.Store == nil {
		return AlertPolicyEventPage{}, errors.New("alert store is required")
	}
	store, ok := a.Store.(alertPolicyEventPageStore)
	if !ok {
		return AlertPolicyEventPage{}, errors.New("alert-policy event pagination is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	before, beforeID, err := decodeGovernanceEventCursor(cursor, ErrInvalidAlertPolicyEventCursor)
	if err != nil {
		return AlertPolicyEventPage{}, err
	}
	items, err := store.ListAlertPolicyEventPage(ctx, strings.TrimSpace(policyID), limit+1, before, beforeID)
	if err != nil {
		return AlertPolicyEventPage{}, err
	}
	page := AlertPolicyEventPage{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeGovernanceEventCursor(last.OccurredAt, last.ID)
	return page, err
}
