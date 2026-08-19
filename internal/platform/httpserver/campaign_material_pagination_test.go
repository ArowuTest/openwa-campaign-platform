package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/shared/httpx"
)

type campaignMaterialPaginationRepository struct {
	items []campaign.MaterialChangeEvent
}

func (*campaignMaterialPaginationRepository) Create(context.Context, campaign.Campaign) error {
	return nil
}
func (*campaignMaterialPaginationRepository) CompareAndSwap(context.Context, campaign.Campaign, int64) error {
	return nil
}
func (*campaignMaterialPaginationRepository) Get(_ context.Context, id string) (campaign.Campaign, error) {
	return campaign.Campaign{ID: id}, nil
}
func (*campaignMaterialPaginationRepository) ListPage(context.Context, int, *time.Time, string) ([]campaign.Campaign, error) {
	return nil, nil
}
func (*campaignMaterialPaginationRepository) AmendMaterial(context.Context, campaign.Campaign, campaign.MaterialChangeEvent, int64) error {
	return nil
}
func (*campaignMaterialPaginationRepository) ListMaterialChanges(context.Context, string) ([]campaign.MaterialChangeEvent, error) {
	return nil, nil
}
func (r *campaignMaterialPaginationRepository) ListMaterialChangePage(_ context.Context, _ string, limit int, _ int64) ([]campaign.MaterialChangeEvent, error) {
	if len(r.items) > limit {
		return append([]campaign.MaterialChangeEvent(nil), r.items[:limit]...), nil
	}
	return append([]campaign.MaterialChangeEvent(nil), r.items...), nil
}

func TestCampaignMaterialChangesReturnCursorContinuation(t *testing.T) {
	repo := &campaignMaterialPaginationRepository{items: []campaign.MaterialChangeEvent{
		{ID: "change-1", CampaignID: "campaign-a", Sequence: 1},
		{ID: "change-2", CampaignID: "campaign-a", Sequence: 2},
		{ID: "change-3", CampaignID: "campaign-a", Sequence: 3},
	}}
	server := &Server{deps: Dependencies{Campaigns: campaign.NewService(repo)}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/campaigns/campaign-a/material-changes?limit=2", nil)
	request.SetPathValue("id", "campaign-a")
	response := httptest.NewRecorder()
	server.listCampaignMaterialChanges(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected material-change page: %+v body=%s", page, response.Body.String())
	}
}
