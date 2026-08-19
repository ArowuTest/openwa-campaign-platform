package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/inbound"
	"campaign-platform/internal/shared/httpx"
)

type inboundPaginationRepository struct {
	*inbound.MemoryRepository
	items []inbound.Reply
}

func (r *inboundPaginationRepository) ListReplyPage(_ context.Context, limit int, _ *time.Time, _ string) ([]inbound.Reply, error) {
	if len(r.items) > limit {
		return append([]inbound.Reply(nil), r.items[:limit]...), nil
	}
	return append([]inbound.Reply(nil), r.items...), nil
}

type rotationPaginationRepository struct {
	*inbound.MemoryRotationRepository
	items []inbound.RotationRun
}

func (r *rotationPaginationRepository) ListRunPage(_ context.Context, limit int, _ *time.Time, _ string) ([]inbound.RotationRun, error) {
	if len(r.items) > limit {
		return append([]inbound.RotationRun(nil), r.items[:limit]...), nil
	}
	return append([]inbound.RotationRun(nil), r.items...), nil
}

func TestInboundRepliesReturnCursorContinuationWithoutContentLeak(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := &inboundPaginationRepository{MemoryRepository: inbound.NewMemoryRepository(), items: []inbound.Reply{
		{ID: "reply-c", MessageText: "secret-c", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "reply-b", MessageText: "secret-b", CreatedAt: base.Add(time.Minute)},
		{ID: "reply-a", MessageText: "secret-a", CreatedAt: base},
	}}
	server := &Server{deps: Dependencies{InboundReplies: &inbound.Service{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/inbound-replies?limit=2", nil)
	request = request.WithContext(identity.WithPrincipal(request.Context(), identity.Principal{User: identity.User{ID: "reader", Permissions: map[string]struct{}{"inbound.read": {}}}}))
	response := httptest.NewRecorder()
	server.listInboundReplies(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected inbound-reply page: %+v body=%s", page, response.Body.String())
	}
	if json.Valid(response.Body.Bytes()) && (containsJSONText(response.Body.String(), "secret-c") || containsJSONText(response.Body.String(), "secret-b")) {
		t.Fatalf("message content leaked in paged list: %s", response.Body.String())
	}
}

func containsJSONText(body, value string) bool {
	return len(value) > 0 && strings.Contains(body, value)
}

func TestInboundReencryptionRunsReturnCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := &rotationPaginationRepository{MemoryRotationRepository: inbound.NewMemoryRotationRepository(), items: []inbound.RotationRun{
		{ID: "run-c", RequestedAt: base.Add(2 * time.Minute)},
		{ID: "run-b", RequestedAt: base.Add(time.Minute)},
		{ID: "run-a", RequestedAt: base},
	}}
	server := &Server{deps: Dependencies{InboundRotation: &inbound.RotationService{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/inbound-reencryption-runs?limit=2", nil)
	response := httptest.NewRecorder()
	server.listInboundReencryptionRuns(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected re-encryption page: %+v body=%s", page, response.Body.String())
	}
}
