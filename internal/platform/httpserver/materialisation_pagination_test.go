package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/audience/materialisation"
	"campaign-platform/internal/shared/httpx"
)

func TestAudienceMaterialisationsReturnCursorContinuation(t *testing.T) {
	repo := materialisation.NewMemoryMaterialisationRepository()
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	for index, job := range []materialisation.MaterialisationJob{
		{ID: "job-a", CampaignID: "campaign-a", RequestFingerprint: "fingerprint-a", RequestedAt: base},
		{ID: "job-b", CampaignID: "campaign-a", RequestFingerprint: "fingerprint-b", RequestedAt: base.Add(time.Minute)},
		{ID: "job-c", CampaignID: "campaign-a", RequestFingerprint: "fingerprint-c", RequestedAt: base.Add(2 * time.Minute)},
	} {
		if _, err := repo.Create(context.Background(), job); err != nil {
			t.Fatalf("seed materialisation %d: %v", index, err)
		}
	}
	server := &Server{deps: Dependencies{AudienceMaterialisations: &materialisation.MaterialisationService{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/campaigns/campaign-a/audience-materialisations?limit=2", nil)
	request.SetPathValue("id", "campaign-a")
	response := httptest.NewRecorder()
	server.listAudienceMaterialisations(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected materialisation page: %+v body=%s", page, response.Body.String())
	}
}
