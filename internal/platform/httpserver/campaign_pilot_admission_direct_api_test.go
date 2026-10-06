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
	"campaign-platform/internal/orchestration"
	"campaign-platform/internal/segment"
)

type pilotCampaignReader struct{ value campaign.Campaign }

func (r pilotCampaignReader) Get(context.Context, string) (campaign.Campaign, error) {
	return r.value, nil
}

func freshPilotPrincipal(request *http.Request) *http.Request {
	verified := time.Now().UTC()
	return request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{
		User:    identity.User{ID: "pilot-operator"},
		Session: identity.Session{MFAVerifiedAt: &verified},
	}))
}

func TestDirectReleaseCannotBypassDayOnePilotAdmission(t *testing.T) {
	now := time.Now().UTC()
	start, deadline := now.Add(time.Hour), now.Add(2*time.Hour)
	entity := campaign.Campaign{
		ID: "campaign-direct-release", Status: campaign.StatusScheduled, Version: 7,
		RequestedStartAt: &start, CompletionDeadlineAt: &deadline,
		Transport: campaign.TransportSelection{
			Provider: campaign.ProviderOpenWA, Engine: campaign.EngineWhatsAppWebJS,
			RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "sender-pool-1",
			GatewayPoolID: "gateway-pool-1", FallbackMode: campaign.FallbackNone,
		},
	}
	pilotCalls := 0
	releases := &orchestration.ReleaseService{
		Campaigns: pilotCampaignReader{value: entity},
		Snapshots: segment.NewMemoryRepository(),
		Store:     orchestration.NewMemoryStore(),
		Eligibility: orchestration.EligibilityFunc(func(context.Context, string, string, string, string, time.Time) (orchestration.EligibilityDecision, error) {
			return orchestration.EligibilityDecision{Eligible: true}, nil
		}),
		PilotAdmission: func(context.Context, campaign.Campaign) error {
			pilotCalls++
			return execution.ErrPilotAdmission
		},
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+entity.ID+"/release", strings.NewReader("{\"expectedVersion\":7}"))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", entity.ID)
	request = freshPilotPrincipal(request)
	response := httptest.NewRecorder()

	server := &Server{deps: Dependencies{Releases: releases}}
	server.releaseCampaignAudience(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("direct release status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "CAMPAIGN_RELEASE_REJECTED") {
		t.Fatalf("direct release did not report pilot rejection: %s", response.Body.String())
	}
	if pilotCalls != 1 {
		t.Fatalf("pilot admission calls=%d; release did not fail at the intended gate", pilotCalls)
	}
}

func TestDirectStartCannotBypassDayOnePilotAdmission(t *testing.T) {
	now := time.Now().UTC()
	deadline := now.Add(time.Hour)
	entity := campaign.Campaign{
		ID: "campaign-direct-start", Status: campaign.StatusScheduled, Version: 4,
		RequestedStartAt: &now, CompletionDeadlineAt: &deadline,
		EligibleAudienceCount: 1,
		Transport: campaign.TransportSelection{
			Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineWhatsAppWebJS,
			RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "sender-pool-1",
			GatewayPoolID: "gateway-pool-1", FallbackMode: campaign.FallbackNone,
			AdapterVersion: "0.13.0", RoutingPolicyVersion: "routing-v1", CapacityEvidenceVersion: "capacity-v1",
		},
	}
	campaigns := &atomicityCampaigns{value: entity}
	store := execution.NewMemoryStore()
	store.MetricsByCampaign[entity.ID] = execution.Metrics{Authorised: 1}
	coordinator := &execution.Coordinator{
		Campaigns: campaigns,
		Store:     store,
		Committer: &recordingAtomicityCommitter{},
		Clock:     func() time.Time { return now },
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/"+entity.ID+"/execution/start", strings.NewReader("{\"expectedVersion\":4,\"reason\":\"direct API pilot bypass regression\"}"))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", entity.ID)
	request.SetPathValue("action", "start")
	request = freshPilotPrincipal(request)
	response := httptest.NewRecorder()

	server := &Server{deps: Dependencies{Execution: coordinator}}
	server.executeCampaignAction(response, request)

	if response.Code == http.StatusOK {
		t.Fatalf("direct start bypassed pilot admission: %s", response.Body.String())
	}
	if campaigns.prepareCalls != 0 {
		t.Fatalf("campaign transition was prepared before pilot admission: calls=%d", campaigns.prepareCalls)
	}
}
