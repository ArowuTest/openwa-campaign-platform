package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/shared/httpx"
	"campaign-platform/internal/shared/id"
)

// The request intentionally excludes actor, state, approval and frozen provider
// authority. It is a full replacement of editable preparation fields only.
type CampaignDraftSaveRequest struct {
	ExpectedVersion             *int64                 `json:"expectedVersion"`
	Reason                      string                 `json:"reason"`
	Name                        string                 `json:"name"`
	OrganisationID              string                 `json:"organisationId"`
	PurposeID                   string                 `json:"purposeId"`
	ConsentReviewID             string                 `json:"consentReviewId"`
	MaximumUniqueRecipients     *int64                 `json:"maximumUniqueRecipients"`
	MaximumMessagesPerRecipient *int                   `json:"maximumMessagesPerRecipient"`
	RequestedStartAt            *time.Time             `json:"requestedStartAt"`
	CompletionDeadlineAt        *time.Time             `json:"completionDeadlineAt"`
	Timezone                    string                 `json:"timezone"`
	QuietHoursStart             draftOptionalClock     `json:"quietHoursStart"`
	QuietHoursEnd               draftOptionalClock     `json:"quietHoursEnd"`
	Transport                   *DraftTransportRequest `json:"transport"`
}
type DraftTransportRequest struct {
	Channel                 string                `json:"channel"`
	Provider                campaign.Provider     `json:"provider"`
	Engine                  campaign.Engine       `json:"engine"`
	RoutingMode             campaign.RoutingMode  `json:"routingMode"`
	GatewayPoolID           string                `json:"gatewayPoolId"`
	SenderPoolID            string                `json:"senderPoolId"`
	AdapterVersion          string                `json:"adapterVersion"`
	RoutingPolicyVersion    string                `json:"routingPolicyVersion"`
	CapacityEvidenceVersion string                `json:"capacityEvidenceVersion"`
	FallbackMode            campaign.FallbackMode `json:"fallbackMode"`
	RequiredCapabilities    *[]string             `json:"requiredCapabilities"`
}

func (s *Server) saveCampaignDraft(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identifier := id.CanonicalUUID(r.PathValue("id"))
	if !id.IsUUID(identifier) {
		httpx.WriteError(w, r, 400, "INVALID_CAMPAIGN_ID", "The campaign ID must be a canonical UUID.", nil)
		return
	}
	var request CampaignDraftSaveRequest
	if err := httpx.DecodeJSON(w, r, 256<<10, &request); err != nil {
		httpx.WriteError(w, r, 400, "INVALID_JSON", "The campaign draft request must be a single valid JSON object.", nil)
		return
	}
	if request.ExpectedVersion == nil || *request.ExpectedVersion < 1 || request.MaximumUniqueRecipients == nil || request.MaximumMessagesPerRecipient == nil || request.Transport == nil || request.Transport.RequiredCapabilities == nil || request.Timezone == "" ||
		!id.IsUUID(request.OrganisationID) || !id.IsUUID(request.PurposeID) || !id.IsUUID(request.ConsentReviewID) {
		httpx.WriteError(w, r, 422, "CAMPAIGN_DRAFT_INVALID", "Required draft fields are missing or invalid.", map[string]any{"fields": []string{"REQUIRED_FIELDS"}})
		return
	}
	principal, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, 401, "AUTHENTICATION_REQUIRED", "Authentication is required.", nil)
		return
	}
	if s.deps.Campaigns == nil {
		httpx.WriteError(w, r, 503, "CAMPAIGNS_UNAVAILABLE", "Campaign services are unavailable.", nil)
		return
	}
	t := request.Transport
	input := campaign.DraftSaveInput{ExpectedVersion: *request.ExpectedVersion, ActorID: principal.User.ID, Reason: request.Reason, Name: request.Name,
		OrganisationID: request.OrganisationID, PurposeID: request.PurposeID, ConsentReviewID: request.ConsentReviewID, MaximumUniqueRecipients: *request.MaximumUniqueRecipients,
		MaximumMessagesPerRecipient: *request.MaximumMessagesPerRecipient, RequestedStartAt: request.RequestedStartAt, CompletionDeadlineAt: request.CompletionDeadlineAt,
		Timezone: request.Timezone, QuietHoursStart: string(request.QuietHoursStart), QuietHoursEnd: string(request.QuietHoursEnd),
		Transport: campaign.TransportSelection{Channel: t.Channel, Provider: t.Provider, Engine: t.Engine, RoutingMode: t.RoutingMode, GatewayPoolID: t.GatewayPoolID, SenderPoolID: t.SenderPoolID,
			AdapterVersion: t.AdapterVersion, RoutingPolicyVersion: t.RoutingPolicyVersion, CapacityEvidenceVersion: t.CapacityEvidenceVersion, FallbackMode: t.FallbackMode, RequiredCapabilities: *t.RequiredCapabilities}}
	saved, err := s.deps.Campaigns.SaveDraft(r.Context(), identifier, input)
	switch {
	case errors.Is(err, campaign.ErrNotFound):
		httpx.WriteError(w, r, 404, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
	case errors.Is(err, campaign.ErrConflict):
		httpx.WriteError(w, r, 409, "CAMPAIGN_VERSION_CONFLICT", "The campaign changed. Reload its current version before saving.", nil)
	case errors.Is(err, campaign.ErrDraftNotEditable):
		httpx.WriteError(w, r, 409, "CAMPAIGN_DRAFT_NOT_EDITABLE", "Draft editing is locked by the campaign state or frozen evidence.", nil)
	case errors.Is(err, campaign.ErrDraftReferencesLocked):
		httpx.WriteError(w, r, 409, "CAMPAIGN_DRAFT_REFERENCES_LOCKED", "Organisation, purpose and consent-review references are locked.", nil)
	case errors.Is(err, campaign.ErrDraftGovernanceUnavailable):
		httpx.WriteError(w, r, 503, "CAMPAIGN_DRAFT_GOVERNANCE_UNAVAILABLE", "Current draft governance could not be verified.", nil)
	case errors.Is(err, campaign.ErrDraftInvalid):
		field := "DRAFT"
		var invalid *campaign.DraftFieldError
		if errors.As(err, &invalid) {
			field = invalid.Field
		}
		httpx.WriteError(w, r, 422, "CAMPAIGN_DRAFT_INVALID", "Draft fields or current governance are invalid.", map[string]any{"fields": []string{field}})
	case err != nil:
		s.internalError(w, r, err)
	default:
		httpx.WriteJSON(w, 200, saved)
	}
}

type draftOptionalClock string

func (v *draftOptionalClock) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		return errors.New("quiet-hours value must be a string")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*v = draftOptionalClock(value)
	return nil
}
