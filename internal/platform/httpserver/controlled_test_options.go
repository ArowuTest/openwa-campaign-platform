package httpserver

import (
	"context"
	"errors"
	"net/http"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/sender"
	"campaign-platform/internal/shared/httpx"
	"campaign-platform/internal/testmessage"
)

const (
	controlledTestInventoryPageSize = 500
	controlledTestInventoryMaxPages = 10
	controlledTestOptionLimit       = 100
)

type controlledTestRecipientOption struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	MaskedMSISDN string `json:"maskedMsisdn"`
	Status       string `json:"status"`
}

type controlledTestSessionOption struct {
	ID                 string `json:"id"`
	ProfileDisplayName string `json:"profileDisplayName"`
	MaskedMSISDN       string `json:"maskedMsisdn"`
	Status             string `json:"status"`
	Version            int64  `json:"version"`
}

type controlledTestOptionsResponse struct {
	CampaignID                 string                          `json:"campaignId"`
	CampaignVersion            int64                           `json:"campaignVersion"`
	MessageVersionID           string                          `json:"messageVersionId,omitempty"`
	Provider                   string                          `json:"provider"`
	Engine                     string                          `json:"engine"`
	SenderPoolID               string                          `json:"senderPoolId,omitempty"`
	GatewayPoolID              string                          `json:"gatewayPoolId,omitempty"`
	Supported                  bool                            `json:"supported"`
	Reason                     string                          `json:"reason,omitempty"`
	Recipients                 []controlledTestRecipientOption `json:"recipients"`
	Sessions                   []controlledTestSessionOption   `json:"sessions"`
	RecipientInventoryComplete bool                            `json:"recipientInventoryComplete"`
	SessionInventoryComplete   bool                            `json:"sessionInventoryComplete"`
}

func (s *Server) getCampaignTestMessageOptions(w http.ResponseWriter, r *http.Request) {
	if s.deps.Campaigns == nil || s.deps.TestMessages == nil || s.deps.SenderGovernance == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "CONTROLLED_TEST_OPTIONS_UNAVAILABLE", "Controlled-test selection is unavailable.", nil)
		return
	}
	entity, err := s.deps.Campaigns.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, campaign.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, "CAMPAIGN_NOT_FOUND", "The campaign was not found.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	response := controlledTestOptionsResponse{
		CampaignID:       entity.ID,
		CampaignVersion:  entity.Version,
		MessageVersionID: entity.MessageVersionID,
		Provider:         string(entity.Transport.Provider),
		Engine:           string(entity.Transport.Engine),
		SenderPoolID:     entity.Transport.SenderPoolID,
		GatewayPoolID:    entity.Transport.GatewayPoolID,
		Recipients:       []controlledTestRecipientOption{},
		Sessions:         []controlledTestSessionOption{},
	}
	if entity.Transport.Provider != campaign.ProviderOpenWA ||
		(entity.Transport.Engine != campaign.EngineBaileys && entity.Transport.Engine != campaign.EngineWhatsAppWebJS) ||
		entity.Transport.SenderPoolID == "" || entity.Transport.GatewayPoolID == "" {
		response.Reason = "Controlled-test session selection is available only for a governed OpenWA sender-pool route."
		httpx.WriteJSON(w, http.StatusOK, response)
		return
	}

	recipients, recipientsComplete, err := scanControlledTestRecipients(r.Context(), s.deps.TestMessages)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	sessions, sessionsComplete, err := scanControlledTestSessions(r.Context(), s.deps.SenderGovernance, entity)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	response.Supported = true
	response.Recipients = recipients
	response.Sessions = sessions
	response.RecipientInventoryComplete = recipientsComplete
	response.SessionInventoryComplete = sessionsComplete
	httpx.WriteJSON(w, http.StatusOK, response)
}

func scanControlledTestRecipients(ctx context.Context, service *testmessage.Service) ([]controlledTestRecipientOption, bool, error) {
	options := make([]controlledTestRecipientOption, 0, controlledTestOptionLimit)
	cursor := ""
	overflow := false
	for pageNumber := 0; pageNumber < controlledTestInventoryMaxPages; pageNumber++ {
		page, err := service.ListRecipientsPage(ctx, controlledTestInventoryPageSize, cursor)
		if err != nil {
			return nil, false, err
		}
		for _, value := range page.Items {
			if value.Status != testmessage.RecipientActive {
				continue
			}
			if len(options) < controlledTestOptionLimit {
				options = append(options, controlledTestRecipientOption{
					ID: value.ID, Label: value.Label, MaskedMSISDN: value.MaskedMSISDN, Status: string(value.Status),
				})
			} else {
				overflow = true
			}
		}
		if page.NextCursor == "" {
			return options, !overflow, nil
		}
		cursor = page.NextCursor
	}
	return options, false, nil
}

func scanControlledTestSessions(ctx context.Context, service *sender.GovernanceService, entity campaign.Campaign) ([]controlledTestSessionOption, bool, error) {
	options := make([]controlledTestSessionOption, 0, controlledTestOptionLimit)
	cursor := ""
	overflow := false
	for pageNumber := 0; pageNumber < controlledTestInventoryMaxPages; pageNumber++ {
		page, err := service.ListSessionsPage(ctx, controlledTestInventoryPageSize, cursor)
		if err != nil {
			return nil, false, err
		}
		for _, value := range page.Items {
			if value.Status != sender.StatusReady ||
				value.PoolID != entity.Transport.SenderPoolID ||
				value.GatewayPoolID != entity.Transport.GatewayPoolID ||
				value.EngineType != string(entity.Transport.Engine) {
				continue
			}
			if len(options) < controlledTestOptionLimit {
				options = append(options, controlledTestSessionOption{
					ID: value.ID, ProfileDisplayName: value.ProfileDisplayName, MaskedMSISDN: value.MaskedMSISDN,
					Status: string(value.Status), Version: value.Version,
				})
			} else {
				overflow = true
			}
		}
		if page.NextCursor == "" {
			return options, !overflow, nil
		}
		cursor = page.NextCursor
	}
	return options, false, nil
}
