package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/execution"
	"campaign-platform/internal/shared/httpx"
)

func TestShardReallocationsReturnCursorContinuation(t *testing.T) {
	repo := execution.NewMemoryShardRepository()
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo.Reallocations["shard-a"] = []execution.ShardReallocation{
		{ID: "reallocation-a", DispatchShardID: "shard-a", CreatedAt: base},
		{ID: "reallocation-b", DispatchShardID: "shard-a", CreatedAt: base.Add(time.Minute)},
		{ID: "reallocation-c", DispatchShardID: "shard-a", CreatedAt: base.Add(2 * time.Minute)},
	}
	server := &Server{deps: Dependencies{ShardReallocations: &execution.ReallocationAdministration{Store: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dispatch-shards/shard-a/reallocations?limit=2", nil)
	request.SetPathValue("id", "shard-a")
	response := httptest.NewRecorder()
	server.listDispatchShardReallocations(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected shard-reallocation page: %+v body=%s", page, response.Body.String())
	}
}
