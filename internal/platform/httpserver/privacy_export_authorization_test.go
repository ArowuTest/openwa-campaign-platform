package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/privacy"
)

func TestPrivacyExportRouteRejectsMissingPermission(t *testing.T) {
	hash, err := identity.HashPassword("Correct-Horse-Privacy-Export-17")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{ID: "privacy-reader", Email: "privacy-reader@example.test", DisplayName: "Privacy Reader", Status: identity.StatusActive, PasswordHash: hash, Permissions: map[string]struct{}{"report.read": {}}}
	ids := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := ids.Login(context.Background(), user.Email, "Correct-Horse-Privacy-Export-17")
	if err != nil {
		t.Fatal(err)
	}
	server := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{Identity: ids, PrivacyCases: &privacy.Service{}, Operations: &operations.Service{}})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/privacy/cases/00000000-0000-4000-8000-000000000001/exports", strings.NewReader(`{"format":"JSON","reason":"authorised privacy request"}`))
	request.Header.Set("Authorization", "Bearer "+login.SessionToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "STEP_UP_REQUIRED") {
		t.Fatalf("permission gate was bypassed: %s", response.Body.String())
	}
}

func TestPrivacyExportRouteRequiresRecentMFAStepUp(t *testing.T) {
	hash, err := identity.HashPassword("Correct-Horse-Privacy-Export-18")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{ID: "privacy-exporter", Email: "privacy-exporter@example.test", DisplayName: "Privacy Exporter", Status: identity.StatusActive, PasswordHash: hash, MFARequired: false, Permissions: map[string]struct{}{"privacy.read": {}}}
	ids := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := ids.Login(context.Background(), user.Email, "Correct-Horse-Privacy-Export-18")
	if err != nil {
		t.Fatal(err)
	}
	server := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{Identity: ids, PrivacyCases: &privacy.Service{}, Operations: &operations.Service{}})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/privacy/cases/00000000-0000-4000-8000-000000000001/exports", strings.NewReader(`{"format":"JSON","reason":"authorised privacy request"}`))
	request.Header.Set("Authorization", "Bearer "+login.SessionToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "STEP_UP_REQUIRED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "PRIVACY_UNAVAILABLE") || strings.Contains(response.Body.String(), "EXPORT_UNAVAILABLE") {
		t.Fatalf("privacy export reached dependency work before step-up: %s", response.Body.String())
	}
}
