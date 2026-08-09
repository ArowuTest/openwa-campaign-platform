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

func TestSenderPairingPermissionBlocksNonAdministratorBeforeSensitiveHandler(t *testing.T) {
	hash, err := identity.HashPassword("Correct-Horse-Battery-17")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{
		ID: "pairing-reader", Email: "pairing-reader@example.test", DisplayName: "Pairing Reader",
		Status: identity.StatusActive, PasswordHash: hash, Permissions: map[string]struct{}{"sender.read": {}},
	}
	idService := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := idService.Login(context.Background(), user.Email, "Correct-Horse-Battery-17")
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{deps: Dependencies{Identity: idService}}
	innerCalled := false
	protected := s.require("sender.admin", func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		_, _ = w.Write([]byte("SENSITIVE-PAIRING-PAYLOAD"))
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sender-sessions/session-1/pairing/qr", nil)
	request.Header.Set("Authorization", "Bearer "+login.SessionToken)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if innerCalled {
		t.Fatal("non-administrator reached pairing payload handler")
	}
	if strings.Contains(response.Body.String(), "SENSITIVE-PAIRING-PAYLOAD") {
		t.Fatalf("pairing payload leaked to unauthorised role: %s", response.Body.String())
	}
}

func TestSenderPairingRequiresRecentMFAStepUpForAdministrator(t *testing.T) {
	hash, err := identity.HashPassword("Correct-Horse-Battery-18")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{
		ID: "pairing-admin", Email: "pairing-admin@example.test", DisplayName: "Pairing Admin",
		Status: identity.StatusActive, PasswordHash: hash, MFARequired: false,
		Permissions: map[string]struct{}{"sender.admin": {}},
	}
	idService := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := idService.Login(context.Background(), user.Email, "Correct-Horse-Battery-18")
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{deps: Dependencies{Identity: idService}}
	protected := s.require("sender.admin", s.getGatewaySenderSessionQR)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sender-sessions/session-1/pairing/qr", nil)
	request.Header.Set("Authorization", "Bearer "+login.SessionToken)
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "STEP_UP_REQUIRED") {
		t.Fatalf("pairing request was not stopped by step-up gate: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "SENDER_LIFECYCLE_UNAVAILABLE") {
		t.Fatalf("pairing request reached lifecycle before step-up: %s", response.Body.String())
	}
}
