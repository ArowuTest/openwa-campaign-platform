package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/operations"
	"campaign-platform/internal/shared/httpx"
)

type operationsPaginationRepository struct {
	*operations.MemoryRepository
	incidents  []operations.Incident
	exceptions []operations.DeliveryException
}

func (r *operationsPaginationRepository) ListIncidentPage(_ context.Context, _ operations.IncidentStatus, limit int, _ int, _ *time.Time, _ string) ([]operations.Incident, error) {
	if len(r.incidents) > limit {
		return append([]operations.Incident(nil), r.incidents[:limit]...), nil
	}
	return append([]operations.Incident(nil), r.incidents...), nil
}

func (r *operationsPaginationRepository) ListExceptionPage(_ context.Context, _ string, limit int, _ *time.Time, _ string) ([]operations.DeliveryException, error) {
	if len(r.exceptions) > limit {
		return append([]operations.DeliveryException(nil), r.exceptions[:limit]...), nil
	}
	return append([]operations.DeliveryException(nil), r.exceptions...), nil
}

func decodeListResponse(t *testing.T, recorder *httptest.ResponseRecorder) httpx.ListResponse {
	t.Helper()
	var page httpx.ListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestOperationsIncidentListReturnsCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := &operationsPaginationRepository{MemoryRepository: operations.NewMemoryRepository(), incidents: []operations.Incident{
		{ID: "incident-c", Severity: operations.SeverityCritical, CreatedAt: base.Add(2 * time.Minute)},
		{ID: "incident-b", Severity: operations.SeverityCritical, CreatedAt: base.Add(time.Minute)},
		{ID: "incident-a", Severity: operations.SeverityWarning, CreatedAt: base},
	}}
	server := &Server{deps: Dependencies{Operations: &operations.Service{Repo: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/incidents?limit=2", nil)
	response := httptest.NewRecorder()
	server.listOperationsIncidents(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	page := decodeListResponse(t, response)
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected incident page: %+v body=%s", page, response.Body.String())
	}
}

func TestDeliveryExceptionListReturnsCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := &operationsPaginationRepository{MemoryRepository: operations.NewMemoryRepository(), exceptions: []operations.DeliveryException{
		{RecipientID: "recipient-c", CampaignID: "campaign-a", UpdatedAt: base.Add(2 * time.Minute)},
		{RecipientID: "recipient-b", CampaignID: "campaign-a", UpdatedAt: base.Add(time.Minute)},
		{RecipientID: "recipient-a", CampaignID: "campaign-a", UpdatedAt: base},
	}}
	server := &Server{deps: Dependencies{Operations: &operations.Service{Repo: repo}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/delivery-exceptions?campaignId=campaign-a&limit=2", nil)
	response := httptest.NewRecorder()
	server.listDeliveryExceptions(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	page := decodeListResponse(t, response)
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected delivery-exception page: %+v body=%s", page, response.Body.String())
	}
}
