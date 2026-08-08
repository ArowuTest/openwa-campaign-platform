package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/platformpolicy"
	"campaign-platform/internal/shared/httpx"
)

func TestPlatformConfigurationsReturnCursorContinuation(t *testing.T) {
	store := platformpolicy.NewMemoryStore()
	admin := &platformpolicy.ConfigurationAdministration{Store: store}
	base := time.Date(2026, 8, 8, 6, 0, 0, 0, time.UTC)
	for i, key := range []string{"TEST.CONFIG.A", "TEST.CONFIG.B", "TEST.CONFIG.C"} {
		at := base.Add(time.Duration(i) * time.Minute)
		admin.Clock = func() time.Time { return at }
		if _, err := admin.Create(context.Background(), platformpolicy.Configuration{Key: key, ScopeType: platformpolicy.ScopePlatform, Value: json.RawMessage(`{"enabled":true}`)}, "actor", "pagination test"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{Configurations: admin}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/configurations?limit=2", nil)
	response := httptest.NewRecorder()
	server.listPlatformConfigurations(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected configuration page: %+v body=%s", page, response.Body.String())
	}
}

func TestMaintenanceWindowsReturnCursorContinuation(t *testing.T) {
	store := platformpolicy.NewMemoryStore()
	admin := &platformpolicy.MaintenanceAdministration{Store: store}
	base := time.Date(2026, 8, 8, 6, 0, 0, 0, time.UTC)
	for i, name := range []string{"Window A", "Window B", "Window C"} {
		at := base.Add(time.Duration(i) * time.Minute)
		admin.Clock = func() time.Time { return at }
		if _, err := admin.Create(context.Background(), platformpolicy.MaintenanceWindow{Name: name, Mode: platformpolicy.MaintenanceReadOnly, ScopeType: platformpolicy.ScopePlatform}, "actor", "pagination test"); err != nil {
			t.Fatal(err)
		}
	}
	server := &Server{deps: Dependencies{Maintenance: admin}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/maintenance-windows?limit=2", nil)
	response := httptest.NewRecorder()
	server.listMaintenanceWindows(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page httpx.ListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.NextCursor == "" || !page.HasMore {
		t.Fatalf("unexpected maintenance page: %+v body=%s", page, response.Body.String())
	}
}
