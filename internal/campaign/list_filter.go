package campaign

import (
	"context"
	"errors"
	"strings"
	"time"
)

type ListFilter struct {
	OrganisationID string
	Status         Status
}

type filteredPageRepository interface {
	ListFilteredPage(context.Context, ListFilter, int, *time.Time, string) ([]Campaign, error)
}

func (s *Service) ListFiltered(ctx context.Context, filter ListFilter, limit int, cursor string) (Page, error) {
	if s == nil || s.repository == nil {
		return Page{}, errors.New("campaign repository is required")
	}
	filter.OrganisationID = strings.TrimSpace(filter.OrganisationID)
	if filter.Status != "" && !validCampaignListStatus(filter.Status) {
		return Page{}, errors.New("invalid campaign status filter")
	}
	repository, ok := s.repository.(filteredPageRepository)
	if !ok {
		return Page{}, errors.New("filtered campaign inventory is unavailable")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	before, beforeID, err := decodeCampaignPageCursor(cursor)
	if err != nil {
		return Page{}, err
	}
	items, err := repository.ListFilteredPage(ctx, filter, limit+1, before, beforeID)
	if err != nil {
		return Page{}, err
	}
	return campaignPageFromItems(items, limit)
}

func validCampaignListStatus(status Status) bool {
	switch status {
	case StatusDraft, StatusConsentReviewPending, StatusConsentApproved, StatusAudienceBuilding,
		StatusAudienceValidated, StatusMessageReviewPending, StatusMessageApproved, StatusCommercialApproved,
		StatusFinalApprovalPending, StatusScheduled, StatusDispatching, StatusPaused, StatusCompleted,
		StatusCompletedWithExceptions, StatusCancelled:
		return true
	default:
		return false
	}
}
