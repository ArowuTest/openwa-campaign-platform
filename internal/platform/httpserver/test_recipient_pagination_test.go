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

func TestTestRecipientsReturnCursorContinuation(t *testing.T) {
	repo := testmessage.NewMemoryRepository()
	base := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	for i, id := range []string{"recipient-a", "recipient-b", "recipient-c"} {
		at := base.Add(time.Duration(i) * time.Minute)
		_, err := repo.CreateRecipient(context.Background(), testmessage.Recipient{ID: id, Label: id, MSISDNLookupHash: []byte(id), MaskedMSISDN: "+234 ***", Status: testmessage.RecipientActive, CreatedBy: "actor", Reason: "pagination seed", Version: 1, CreatedAt: at, UpdatedAt: at})
		if err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{TestMessages: &testmessage.Service{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/test-recipients?limit=2", nil)
	response := httptest.NewRecorder()
	server.listTestRecipients(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected test-recipient page: %+v body=%s", page, response.Body.String())
	}
}
