package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/retention"
	"campaign-platform/internal/shared/httpx"
)

func TestRetentionJobsReturnsCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	store := retention.NewMemoryStore()
	for _, job := range []retention.Job{
		{ID: "job-a", Status: retention.JobPending, CreatedAt: base, UpdatedAt: base},
		{ID: "job-b", Status: retention.JobPending, CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute)},
		{ID: "job-c", Status: retention.JobPending, CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base.Add(2 * time.Minute)},
	} {
		if err := store.AddJob(job); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{Retention: &retention.Administration{Store: store}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/retention-jobs?limit=2", nil)
	response := httptest.NewRecorder()
	server.listRetentionJobs(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected retention-job page: %+v body=%s", page, response.Body.String())
	}
}
