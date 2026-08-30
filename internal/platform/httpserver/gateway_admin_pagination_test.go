package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/sender"
)

type runtimePageStore struct {
	events []sender.RuntimeEvent
}

func (f *runtimePageStore) GetNode(context.Context, string) (sender.Node, error) {
	return sender.Node{}, nil
}
func (f *runtimePageStore) UseRuntimeNonce(context.Context, string, string, string, time.Time) error {
	return nil
}
func (f *runtimePageStore) ApplyRuntimeReport(context.Context, string, int64, sender.RuntimeReport, string, string, time.Time, time.Time) (sender.Node, error) {
	return sender.Node{}, nil
}
func (f *runtimePageStore) RecordRuntimeRejection(context.Context, string, sender.RuntimeReport, string, string, string, time.Time, time.Time) error {
	return nil
}
func (f *runtimePageStore) ListRuntimeEvents(context.Context, string, int) ([]sender.RuntimeEvent, error) {
	return append([]sender.RuntimeEvent(nil), f.events...), nil
}
func (f *runtimePageStore) ListRuntimeEventPage(_ context.Context, _ string, limit int, before *time.Time, beforeID string) ([]sender.RuntimeEvent, error) {
	out := make([]sender.RuntimeEvent, 0, limit)
	for _, event := range f.events {
		if before != nil && (event.OccurredAt.After(*before) || event.OccurredAt.Equal(*before) && event.ID >= beforeID) {
			continue
		}
		out = append(out, event)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func TestListGatewayRuntimeEventsReturnsCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 18, 0, 0, 0, time.UTC)
	store := &runtimePageStore{events: []sender.RuntimeEvent{
		{ID: "event-c", NodeID: "node-1", OccurredAt: base.Add(2 * time.Minute)},
		{ID: "event-b", NodeID: "node-1", OccurredAt: base.Add(time.Minute)},
		{ID: "event-a", NodeID: "node-1", OccurredAt: base},
	}}
	server := &Server{deps: Dependencies{GatewayRuntime: &sender.RuntimeRegistrationService{Store: store}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/gateway-nodes/node-1/runtime-events?limit=2", nil)
	request.SetPathValue("id", "node-1")
	response := httptest.NewRecorder()
	server.listGatewayRuntimeEvents(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items      []sender.RuntimeEvent `json:"items"`
		Count      int                   `json:"count"`
		NextCursor string                `json:"nextCursor"`
		HasMore    bool                  `json:"hasMore"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected runtime page: %+v body=%s", page, response.Body.String())
	}
}
