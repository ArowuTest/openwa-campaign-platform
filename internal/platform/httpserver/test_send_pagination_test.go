package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/shared/httpx"
	"campaign-platform/internal/testmessage"
)

func TestTestMessageSendsReturnCursorContinuation(t *testing.T) {
	repo := testmessage.NewMemoryRepository()
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	for index, send := range []testmessage.Send{
		{ID: "send-a", CampaignID: "campaign-a", IdempotencyKey: "test-send-a", CreatedAt: base},
		{ID: "send-b", CampaignID: "campaign-a", IdempotencyKey: "test-send-b", CreatedAt: base.Add(time.Minute)},
		{ID: "send-c", CampaignID: "campaign-a", IdempotencyKey: "test-send-c", CreatedAt: base.Add(2 * time.Minute)},
	} {
		if _, err := repo.CreateSend(context.Background(), send); err != nil {
			t.Fatalf("seed send %d: %v", index, err)
		}
	}
	server := &Server{deps: Dependencies{TestMessages: &testmessage.Service{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/campaigns/campaign-a/test-messages?limit=2", nil)
	request.SetPathValue("id", "campaign-a")
	response := httptest.NewRecorder()
	server.listTestMessages(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected test-send page: %+v body=%s", page, response.Body.String())
	}
}
