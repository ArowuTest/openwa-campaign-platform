package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"campaign-platform/internal/execution"
	"campaign-platform/internal/shared/httpx"
)

func TestCampaignRoutingPlansReturnCursorContinuation(t *testing.T) {
	store := execution.NewMemoryRoutingPlanStore()
	for index, plan := range []execution.RoutingPlan{
		{ID: "plan-1", CampaignID: "campaign-a", IdempotencyKey: "route-1", RequestHash: "hash-1"},
		{ID: "plan-2", CampaignID: "campaign-a", IdempotencyKey: "route-2", RequestHash: "hash-2"},
		{ID: "plan-3", CampaignID: "campaign-a", IdempotencyKey: "route-3", RequestHash: "hash-3"},
	} {
		if _, err := store.Create(context.Background(), plan, nil); err != nil {
			t.Fatalf("seed routing plan %d: %v", index, err)
		}
	}
	server := &Server{deps: Dependencies{RoutingPlans: &execution.RoutingAdministration{Store: store}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/campaigns/campaign-a/routing-plans?limit=2", nil)
	request.SetPathValue("id", "campaign-a")
	response := httptest.NewRecorder()
	server.listCampaignRoutingPlans(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected routing-plan page: %+v body=%s", page, response.Body.String())
	}
}
