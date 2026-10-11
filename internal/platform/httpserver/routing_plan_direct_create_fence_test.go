package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/identity"
)

func steppedUpRoutingPlanRequest(t *testing.T, campaignID string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+campaignID+"/routing-plans", strings.NewReader("{"))
	request.SetPathValue("id", campaignID)
	request.Header.Set("Content-Type", "application/json")
	verified := time.Now().UTC()
	return request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{
		User:    identity.User{ID: "11111111-1111-4111-8111-111111111111"},
		Session: identity.Session{MFAVerifiedAt: &verified},
	}))
}

func TestDirectRoutingPlanCreateCannotBypassDayOneOpenWACapacityHold(t *testing.T) {
	const campaignID = "10000000-0000-4000-8000-000000000001"
	repository := campaign.NewMemoryRepository()
	if err := repository.Create(context.Background(), campaign.Campaign{
		ID: campaignID,
		Transport: campaign.TransportSelection{
			Channel:       "WHATSAPP",
			Provider:      campaign.ProviderOpenWA,
			Engine:        campaign.EngineBaileys,
			RoutingMode:   campaign.RoutingSenderPool,
			FallbackMode:  campaign.FallbackNone,
			SenderPoolID:  "20000000-0000-4000-8000-000000000001",
			GatewayPoolID: "30000000-0000-4000-8000-000000000001",
		},
	}); err != nil {
		t.Fatal(err)
	}
	server := &Server{deps: Dependencies{
		Campaigns:    campaign.NewService(repository),
		RoutingPlans: &execution.RoutingAdministration{},
	}}
	response := httptest.NewRecorder()
	server.createCampaignRoutingPlan(response, steppedUpRoutingPlanRequest(t, campaignID))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "ROUTING_PLAN_DIRECT_CREATE_DISABLED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDirectRoutingPlanCreateFenceDoesNotDisableNonDayOneRouteAdministration(t *testing.T) {
	const campaignID = "10000000-0000-4000-8000-000000000002"
	repository := campaign.NewMemoryRepository()
	if err := repository.Create(context.Background(), campaign.Campaign{
		ID: campaignID,
		Transport: campaign.TransportSelection{
			Channel:      "WHATSAPP",
			Provider:     campaign.ProviderMeta,
			Engine:       campaign.EngineMetaCloud,
			RoutingMode:  campaign.RoutingSenderPool,
			FallbackMode: campaign.FallbackNone,
			SenderPoolID: "20000000-0000-4000-8000-000000000002",
			MetaSenderID: "30000000-0000-4000-8000-000000000002",
		},
	}); err != nil {
		t.Fatal(err)
	}
	server := &Server{deps: Dependencies{
		Campaigns:    campaign.NewService(repository),
		RoutingPlans: &execution.RoutingAdministration{},
	}}
	response := httptest.NewRecorder()
	server.createCampaignRoutingPlan(response, steppedUpRoutingPlanRequest(t, campaignID))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "INVALID_JSON") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDirectRoutingPlanCreateAlsoFencesLegacyOpenWASpecificSession(t *testing.T) {
	const campaignID = "10000000-0000-4000-8000-000000000003"
	repository := campaign.NewMemoryRepository()
	if err := repository.Create(context.Background(), campaign.Campaign{
		ID: campaignID,
		Transport: campaign.TransportSelection{
			Channel:       "WHATSAPP",
			Provider:      campaign.ProviderOpenWA,
			Engine:        campaign.EngineBaileys,
			RoutingMode:   campaign.RoutingSpecificSession,
			FallbackMode:  campaign.FallbackNone,
			SessionID:     "20000000-0000-4000-8000-000000000003",
			GatewayPoolID: "30000000-0000-4000-8000-000000000003",
		},
	}); err != nil {
		t.Fatal(err)
	}
	server := &Server{deps: Dependencies{
		Campaigns:    campaign.NewService(repository),
		RoutingPlans: &execution.RoutingAdministration{},
	}}
	response := httptest.NewRecorder()
	server.createCampaignRoutingPlan(response, steppedUpRoutingPlanRequest(t, campaignID))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "ROUTING_PLAN_DIRECT_CREATE_DISABLED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
