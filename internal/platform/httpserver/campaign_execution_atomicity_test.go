package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/identity"
)

type atomicityCampaigns struct {
	value        campaign.Campaign
	prepareCalls int
}

func (f *atomicityCampaigns) Get(
	context.Context, string,
) (campaign.Campaign, error) {
	return f.value, nil
}

func (f *atomicityCampaigns) PrepareTransition(
	_ context.Context, _ string, input campaign.TransitionInput,
) (campaign.Campaign, error) {
	f.prepareCalls++
	return f.value.Transition(input, time.Now().UTC())
}

type recordingAtomicityCommitter struct {
	calls int
	value execution.LifecycleCommit
}

func (f *recordingAtomicityCommitter) Commit(
	_ context.Context, value execution.LifecycleCommit,
) (campaign.Campaign, error) {
	f.calls++
	f.value = value
	return value.Campaign, nil
}

func TestCampaignExecutionRequiresAtomicCommitter(t *testing.T) {
	now := time.Now().UTC()
	deadline := now.Add(time.Hour)
	entity := campaign.Campaign{
		ID: "campaign-atomic-route", Status: campaign.StatusScheduled,
		RequestedStartAt: &now, CompletionDeadlineAt: &deadline,
		EligibleAudienceCount: 10, Version: 1,
		Transport: campaign.TransportSelection{
			RoutingMode:  campaign.RoutingSenderPool,
			SenderPoolID: "pool-atomic-route", CapacityEvidenceVersion: "v1",
		},
	}
	campaigns := &atomicityCampaigns{value: entity}
	store := execution.NewMemoryStore()
	store.MetricsByCampaign[entity.ID] = execution.Metrics{Authorised: 10}
	coordinator := &execution.Coordinator{
		Campaigns: campaigns, Store: store, Clock: func() time.Time { return now },
	}
	response := invokeAtomicityStart(t, coordinator, entity.ID, entity.Version)
	if response.Code != http.StatusConflict {
		t.Fatalf("missing committer status=%d body=%s",
			response.Code, response.Body.String())
	}
	if campaigns.prepareCalls != 0 ||
		campaigns.value.Status != campaign.StatusScheduled ||
		campaigns.value.Version != 1 {
		t.Fatalf("campaign changed without committer: %+v calls=%d",
			campaigns.value, campaigns.prepareCalls)
	}

	committer := &recordingAtomicityCommitter{}
	coordinator.Committer = committer
	response = invokeAtomicityStart(t, coordinator, entity.ID, entity.Version)
	if response.Code != http.StatusOK {
		t.Fatalf("atomic start status=%d body=%s",
			response.Code, response.Body.String())
	}
	if committer.calls != 1 {
		t.Fatalf("atomic committer calls=%d", committer.calls)
	}
	if committer.value.Action != campaign.ActionStartDispatch ||
		committer.value.EventType != "DISPATCH_STARTED" {
		t.Fatalf("unexpected lifecycle commit: %+v", committer.value)
	}
}
func invokeAtomicityStart(
	t *testing.T,
	coordinator *execution.Coordinator,
	campaignID string,
	version int64,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/campaigns/"+campaignID+"/execution/start",
		strings.NewReader(`{"expectedVersion":`+
			strconv.FormatInt(version, 10)+
			`,"reason":"route atomicity test"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", campaignID)
	request.SetPathValue("action", "start")
	verified := time.Now().UTC()
	request = request.WithContext(identity.WithPrincipal(
		request.Context(),
		identity.Principal{
			User:    identity.User{ID: "operator-atomic-route"},
			Session: identity.Session{MFAVerifiedAt: &verified},
		},
	))
	response := httptest.NewRecorder()
	server := &Server{deps: Dependencies{Execution: coordinator}}
	server.executeCampaignAction(response, request)
	return response
}
