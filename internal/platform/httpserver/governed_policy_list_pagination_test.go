package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/operations"
	"campaign-platform/internal/retention"
	"campaign-platform/internal/shared/httpx"
)

func TestRetentionPoliciesReturnCursorContinuation(t *testing.T) {
	store := retention.NewMemoryStore()
	admin := &retention.Administration{Store: store}
	base := time.Date(2026, 8, 8, 6, 30, 0, 0, time.UTC)
	for index, name := range []string{"Retention A", "Retention B", "Retention C"} {
		at := base.Add(time.Duration(index) * time.Minute)
		admin.Clock = func() time.Time { return at }
		if _, err := admin.Create(context.Background(), retention.Policy{Name: name, ObjectType: retention.ObjectAuditEvent, Action: retention.ActionArchive, ScopeType: retention.ScopePlatform, RetentionDays: 30}, "actor", "pagination test"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{Retention: admin}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/retention-policies?status=DRAFT&limit=2", nil)
	response := httptest.NewRecorder()
	server.listRetentionPolicies(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected retention page: %+v body=%s", page, response.Body.String())
	}
}

func TestAlertPoliciesReturnCursorContinuation(t *testing.T) {
	store := operations.NewMemoryAlertStore()
	admin := &operations.AlertAdministration{Store: store}
	base := time.Date(2026, 8, 8, 6, 30, 0, 0, time.UTC)
	for index, name := range []string{"Alert A", "Alert B", "Alert C"} {
		at := base.Add(time.Duration(index) * time.Minute)
		admin.Clock = func() time.Time { return at }
		if _, err := admin.CreatePolicy(context.Background(), operations.AlertPolicy{Name: name, Metric: operations.AlertQueueDepth, Comparison: operations.ComparisonGT, Threshold: 1, Severity: operations.SeverityWarning, ConsecutiveEvaluations: 1, CooldownSeconds: 60}, "actor", "pagination test"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{AlertPolicies: admin}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/alert-policies?status=DRAFT&limit=2", nil)
	response := httptest.NewRecorder()
	server.listAlertPolicies(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected alert-policy page: %+v body=%s", page, response.Body.String())
	}
}
