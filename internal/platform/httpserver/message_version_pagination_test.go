package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/message"
	"campaign-platform/internal/shared/httpx"
)

type messageVersionPaginationRepository struct {
	items []message.Version
}

func (*messageVersionPaginationRepository) CreateDraft(context.Context, message.Input, time.Time) (message.Version, error) { return message.Version{}, nil }
func (*messageVersionPaginationRepository) Get(context.Context, string) (message.Version, error) { return message.Version{}, nil }
func (*messageVersionPaginationRepository) ListByCampaign(context.Context, string) ([]message.Version, error) { return nil, nil }
func (*messageVersionPaginationRepository) Approve(context.Context, string, string, string, time.Time) (message.Version, error) { return message.Version{}, nil }
func (r *messageVersionPaginationRepository) ListByCampaignPage(_ context.Context, _ string, limit int, _ int) ([]message.Version, error) {
	if len(r.items) > limit { return append([]message.Version(nil), r.items[:limit]...), nil }
	return append([]message.Version(nil), r.items...), nil
}

func TestMessageVersionsReturnCursorContinuation(t *testing.T) {
	repo := &messageVersionPaginationRepository{items: []message.Version{
		{ID: "message-3", CampaignID: "campaign-a", Version: 3},
		{ID: "message-2", CampaignID: "campaign-a", Version: 2},
		{ID: "message-1", CampaignID: "campaign-a", Version: 1},
	}}
	server := &Server{deps: Dependencies{Messages: message.NewService(repo)}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/campaigns/campaign-a/messages?limit=2", nil)
	request.SetPathValue("id", "campaign-a")
	response := httptest.NewRecorder()
	server.listMessageVersions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected message-version page: %+v body=%s", page, response.Body.String())
	}
}
