package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	"campaign-platform/internal/operations"
)

func TestAuthenticationEventSearchRouteIsRedactedAndPermissionGated(t *testing.T) {
	passwordHash := task6PasswordHash(t)
	allowedIdentity, token := task6Identity(t, passwordHash, "audit.read")
	events := identity.NewMemoryEventRecorder()
	if err := events.Append(context.Background(), identity.AuthenticationEvent{
		ID:            "00000000-0000-4000-8000-000000000301",
		UserID:        "task6-user",
		EmailHash:     "aabbccddeeff",
		Type:          identity.EventLoginFailed,
		SourceIP:      "203.0.113.10",
		UserAgent:     "private-agent",
		CorrelationID: "auth-route-correlation",
		OccurredAt:    time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Metadata:      map[string]string{"reason": "invalid credentials"},
	}); err != nil {
		t.Fatal(err)
	}
	handler := task6Handler(Dependencies{
		Identity:   allowedIdentity,
		Operations: &operations.Service{AuthenticationEvents: events},
	})
	recorder := requestRecorder(t, handler, token, "/api/v1/operations/authentication-events?limit=10")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{"\"source\":\"authentication\"", "\"eventType\":\"LOGIN_FAILED\"", "\"actorId\":\"task6-user\"", "\"occurredAt\""} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response missing %s: %s", expected, body)
		}
	}
	for _, secret := range []string{"aabbccddeeff", "203.0.113.10", "private-agent", "invalid credentials"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked %s: %s", secret, body)
		}
	}

	invalid := requestRecorder(t, handler, token, "/api/v1/operations/authentication-events?cursor=invalid")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	deniedIdentity, deniedToken := task6Identity(t, passwordHash, "operations.read")
	deniedHandler := task6Handler(Dependencies{
		Identity:   deniedIdentity,
		Operations: &operations.Service{AuthenticationEvents: events},
	})
	denied := requestRecorder(t, deniedHandler, deniedToken, "/api/v1/operations/authentication-events")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("denied status=%d body=%s", denied.Code, denied.Body.String())
	}
}

func requestRecorder(t *testing.T, handler http.Handler, token, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := task6Request(http.MethodGet, path, token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
