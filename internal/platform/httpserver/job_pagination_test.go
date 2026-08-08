package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/jobs"
	"campaign-platform/internal/shared/httpx"
)

type jobPaginationRepository struct {
	items      []jobs.Job
	events     []jobs.AdministrationEvent
	legacyList []jobs.Job
	get        jobs.Job
}

func (r *jobPaginationRepository) List(context.Context, jobs.Query) ([]jobs.Job, error) {
	return append([]jobs.Job(nil), r.legacyList...), nil
}
func (r *jobPaginationRepository) Get(_ context.Context, id string) (jobs.Job, error) {
	if r.get.ID == id {
		return r.get, nil
	}
	return jobs.Job{}, jobs.ErrNotFound
}
func (r *jobPaginationRepository) ListPage(_ context.Context, query jobs.Query, _ *time.Time, _ string) ([]jobs.Job, error) {
	if len(r.items) > query.Limit {
		return append([]jobs.Job(nil), r.items[:query.Limit]...), nil
	}
	return append([]jobs.Job(nil), r.items...), nil
}
func (*jobPaginationRepository) Summary(context.Context, time.Time) (jobs.QueueSummary, error) {
	return jobs.QueueSummary{}, nil
}
func (*jobPaginationRepository) RetryDeadLetter(context.Context, string, string, string, time.Time) (jobs.Job, error) {
	return jobs.Job{}, nil
}
func (*jobPaginationRepository) CancelPending(context.Context, string, string, string, time.Time) (jobs.Job, error) {
	return jobs.Job{}, nil
}
func (*jobPaginationRepository) Events(context.Context, string, int) ([]jobs.AdministrationEvent, error) {
	return nil, nil
}
func (r *jobPaginationRepository) ListAdministrationEventPage(_ context.Context, _ string, limit int, _ *time.Time, _ string) ([]jobs.AdministrationEvent, error) {
	if len(r.events) > limit {
		return append([]jobs.AdministrationEvent(nil), r.events[:limit]...), nil
	}
	return append([]jobs.AdministrationEvent(nil), r.events...), nil
}

func TestListOperationalJobsReturnsCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := &jobPaginationRepository{items: []jobs.Job{
		{ID: "job-c", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "job-b", CreatedAt: base.Add(time.Minute)},
		{ID: "job-a", CreatedAt: base},
	}}
	repo.legacyList = append([]jobs.Job(nil), repo.items...)
	server := &Server{deps: Dependencies{JobOperations: &jobs.AdministrationService{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/jobs?limit=2", nil)
	response := httptest.NewRecorder()
	server.listOperationalJobs(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected job page: %+v body=%s", page, response.Body.String())
	}
}

func TestGetOperationalJobUsesDirectLookup(t *testing.T) {
	repo := &jobPaginationRepository{get: jobs.Job{ID: "old-job", Type: "DISPATCH", Status: jobs.StatusCompleted}}
	server := &Server{deps: Dependencies{JobOperations: &jobs.AdministrationService{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/jobs/old-job", nil)
	request.SetPathValue("id", "old-job")
	response := httptest.NewRecorder()
	server.getOperationalJob(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("direct job lookup failed: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOperationalJobEventsReturnCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := &jobPaginationRepository{events: []jobs.AdministrationEvent{
		{ID: "event-c", JobID: "job-a", OccurredAt: base.Add(2 * time.Minute)},
		{ID: "event-b", JobID: "job-a", OccurredAt: base.Add(time.Minute)},
		{ID: "event-a", JobID: "job-a", OccurredAt: base},
	}}
	server := &Server{deps: Dependencies{JobOperations: &jobs.AdministrationService{Repository: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/jobs/job-a/events?limit=2", nil)
	request.SetPathValue("id", "job-a")
	response := httptest.NewRecorder()
	server.listOperationalJobEvents(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected job-event page: %+v body=%s", page, response.Body.String())
	}
}
