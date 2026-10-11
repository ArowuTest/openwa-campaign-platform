package httpserver

import (
	"campaign-platform/internal/campaign"
	"campaign-platform/internal/campaignpreparation"
	"campaign-platform/internal/shared/httpx"
	"campaign-platform/internal/shared/id"
	"errors"
	"net/http"
)

func (s *Server) getCampaignReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identifier := id.CanonicalUUID(r.PathValue("id"))
	if !id.IsUUID(identifier) {
		httpx.WriteError(w, r, 400, "INVALID_CAMPAIGN_ID", "The campaign ID must be a canonical UUID.", nil)
		return
	}
	value, err := s.deps.CampaignPreparation.Get(r.Context(), identifier)
	switch {
	case errors.Is(err, campaignpreparation.ErrUnavailable):
		httpx.WriteError(w, r, 503, "CAMPAIGN_READINESS_UNAVAILABLE", "Campaign readiness services are unavailable.", nil)
	case errors.Is(err, campaignpreparation.ErrStageUnsupported):
		httpx.WriteError(w, r, 409, "CAMPAIGN_READINESS_STAGE_UNSUPPORTED", "Use the existing execution evidence for this campaign stage.", nil)
	case errors.Is(err, campaign.ErrNotFound):
		httpx.WriteError(w, r, 404, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
	case errors.Is(err, campaign.ErrConflict):
		httpx.WriteError(w, r, 409, "CAMPAIGN_VERSION_CONFLICT", "The campaign changed during assessment. Reload its saved detail.", nil)
	case err != nil:
		s.internalError(w, r, err)
	default:
		httpx.WriteJSON(w, http.StatusOK, value)
	}
}
