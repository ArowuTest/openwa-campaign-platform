package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/identity"
)

func TestExportRequestPermissionBlocksUnauthorisedRoleBeforeHandler(t *testing.T) {
	hash, err := identity.HashPassword("Correct-Horse-Battery-Export-17")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{
		ID: "export-reader", Email: "export-reader@example.test", DisplayName: "Export Reader",
		Status: identity.StatusActive, PasswordHash: hash, Permissions: map[string]struct{}{"report.read": {}},
	}
	idService := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := idService.Login(context.Background(), user.Email, "Correct-Horse-Battery-Export-17")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{deps: Dependencies{Identity: idService}}
	innerCalled := false
	protected := s.require("export.request", func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusAccepted)
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/exports", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+login.SessionToken)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || innerCalled {
		t.Fatalf("status=%d innerCalled=%v body=%s", response.Code, innerCalled, response.Body.String())
	}
}

func TestExportRequestRequiresRecentMFAStepUp(t *testing.T) {
	hash, err := identity.HashPassword("Correct-Horse-Battery-Export-18")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{
		ID: "export-operator", Email: "export-operator@example.test", DisplayName: "Export Operator",
		Status: identity.StatusActive, PasswordHash: hash, MFARequired: false,
		Permissions: map[string]struct{}{"export.request": {}},
	}
	idService := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := idService.Login(context.Background(), user.Email, "Correct-Horse-Battery-Export-18")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{deps: Dependencies{Identity: idService}}
	protected := s.require("export.request", s.requestExport)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/exports", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+login.SessionToken)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "STEP_UP_REQUIRED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "OPERATIONS_UNAVAILABLE") {
		t.Fatalf("export request reached operations handler before step-up: %s", response.Body.String())
	}
}
