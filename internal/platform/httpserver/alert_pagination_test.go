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

type alertPaginationStore struct {
	*operations.MemoryAlertStore
	alerts        []operations.Alert
	alertEvents   []operations.AlertEvent
	notifications []operations.Notification
}

func (s *alertPaginationStore) ListAlertPage(_ context.Context, _ operations.AlertStatus, _ operations.Severity, limit int, _ *time.Time, _ string) ([]operations.Alert, error) {
	if len(s.alerts) > limit {
		return append([]operations.Alert(nil), s.alerts[:limit]...), nil
	}
	return append([]operations.Alert(nil), s.alerts...), nil
}

func (s *alertPaginationStore) ListAlertEventPage(_ context.Context, _ string, limit int, _ *time.Time, _ string) ([]operations.AlertEvent, error) {
	if len(s.alertEvents) > limit {
		return append([]operations.AlertEvent(nil), s.alertEvents[:limit]...), nil
	}
	return append([]operations.AlertEvent(nil), s.alertEvents...), nil
}

func (s *alertPaginationStore) ListNotificationPage(_ context.Context, _ string, limit int, _ *time.Time, _ string) ([]operations.Notification, error) {
	if len(s.notifications) > limit {
		return append([]operations.Notification(nil), s.notifications[:limit]...), nil
	}
	return append([]operations.Notification(nil), s.notifications...), nil
}

func TestOperationalAlertsReturnCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	store := &alertPaginationStore{MemoryAlertStore: operations.NewMemoryAlertStore(), alerts: []operations.Alert{
		{ID: "alert-c", LastObservedAt: base.Add(2 * time.Minute)},
		{ID: "alert-b", LastObservedAt: base.Add(time.Minute)},
		{ID: "alert-a", LastObservedAt: base},
	}}
	server := &Server{deps: Dependencies{AlertPolicies: &operations.AlertAdministration{Store: store}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/alerts?limit=2", nil)
	response := httptest.NewRecorder()
	server.listOperationalAlerts(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected alert page: %+v body=%s", page, response.Body.String())
	}
}

func TestOperationalNotificationsReturnCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	store := &alertPaginationStore{MemoryAlertStore: operations.NewMemoryAlertStore(), notifications: []operations.Notification{
		{ID: "notification-c", Status: "DELIVERED", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "notification-b", Status: "PENDING", CreatedAt: base.Add(time.Minute)},
		{ID: "notification-a", Status: "PENDING", CreatedAt: base},
	}}
	server := &Server{deps: Dependencies{AlertPolicies: &operations.AlertAdministration{Store: store}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/notifications?limit=2", nil)
	response := httptest.NewRecorder()
	server.listOperationalNotifications(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected notification page: %+v body=%s", page, response.Body.String())
	}
}

func TestOperationalAlertEventsReturnCursorContinuation(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	store := &alertPaginationStore{MemoryAlertStore: operations.NewMemoryAlertStore(), alertEvents: []operations.AlertEvent{
		{ID: "event-a", AlertID: "alert-a", OccurredAt: base},
		{ID: "event-b", AlertID: "alert-a", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", AlertID: "alert-a", OccurredAt: base.Add(2 * time.Minute)},
	}}
	server := &Server{deps: Dependencies{AlertPolicies: &operations.AlertAdministration{Store: store}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/operations/alerts/alert-a/events?limit=2", nil)
	request.SetPathValue("id", "alert-a")
	response := httptest.NewRecorder()
	server.listOperationalAlertEvents(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected alert-event page: %+v body=%s", page, response.Body.String())
	}
}
