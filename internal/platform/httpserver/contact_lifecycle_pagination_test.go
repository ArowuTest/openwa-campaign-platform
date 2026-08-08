package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/audience/contactlife"
	"campaign-platform/internal/shared/httpx"
)

type contactLifecyclePaginationRepository struct {
	*contactlife.MemoryRepository
	items []contactlife.Event
}

func (r *contactLifecyclePaginationRepository) ListEventPage(_ context.Context, _ string, limit int, _ *time.Time, _ string) ([]contactlife.Event, error) {
	if len(r.items) > limit {
		return append([]contactlife.Event(nil), r.items[:limit]...), nil
	}
	return append([]contactlife.Event(nil), r.items...), nil
}

func TestContactLifecycleEventsReturnCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := &contactLifecyclePaginationRepository{MemoryRepository: contactlife.NewMemoryRepository(contactlife.Record{ContactID: "contact-a", Version: 1}), items: []contactlife.Event{
		{ID: "event-c", ContactID: "contact-a", OccurredAt: base.Add(2 * time.Minute)},
		{ID: "event-b", ContactID: "contact-a", OccurredAt: base.Add(time.Minute)},
		{ID: "event-a", ContactID: "contact-a", OccurredAt: base},
	}}
	server := &Server{deps: Dependencies{ContactLifecycle: &contactlife.Service{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/contacts/contact-a/lifecycle/events?limit=2", nil)
	request.SetPathValue("id", "contact-a")
	response := httptest.NewRecorder()
	server.listContactLifecycleEvents(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected lifecycle page: %+v body=%s", page, response.Body.String())
	}
}
