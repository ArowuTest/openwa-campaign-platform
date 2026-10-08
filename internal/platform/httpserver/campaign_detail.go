package httpserver

import (
	"errors"
	"net/http"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/shared/httpx"
	"campaign-platform/internal/shared/id"
)

// getCampaign reads saved campaign evidence without rebinding sources or authorising release.
func (s *Server) getCampaign(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identifier := id.CanonicalUUID(r.PathValue("id"))
	if !id.IsUUID(identifier) {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_CAMPAIGN_ID", "The campaign ID must be a canonical UUID.", nil)
		return
	}
	if s.deps.Campaigns == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CAMPAIGNS_UNAVAILABLE", "Campaign services are unavailable.", nil)
		return
	}
	entity, err := s.deps.Campaigns.Get(r.Context(), identifier)
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	// Persistence normalises these timestamps; retain the UTC response contract for
	// any repository implementation, including historical offset timestamps.
	entity.CreatedAt = entity.CreatedAt.UTC()
	entity.UpdatedAt = entity.UpdatedAt.UTC()
	if entity.RequestedStartAt != nil {
		value := entity.RequestedStartAt.UTC()
		entity.RequestedStartAt = &value
	}
	if entity.CompletionDeadlineAt != nil {
		value := entity.CompletionDeadlineAt.UTC()
		entity.CompletionDeadlineAt = &value
	}
	httpx.WriteJSON(w, http.StatusOK, entity)
}
