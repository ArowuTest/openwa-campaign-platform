package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/identity"
)

func TestBrowserAuthenticationCookiesSeparateSessionAndCSRF(t *testing.T) {
	server := &Server{deps: Dependencies{SecureCookies: true}}
	recorder := httptest.NewRecorder()
	expires := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)

	server.setAuthenticationCookies(recorder, "session-secret", "csrf-value", expires)

	response := recorder.Result()
	cookies := response.Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected session and CSRF cookies, got %d", len(cookies))
	}
	byName := map[string]*http.Cookie{}
	for _, cookie := range cookies {
		byName[cookie.Name] = cookie
	}
	session := byName[sessionCookieName]
	if session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteStrictMode || session.Value != "session-secret" {
		t.Fatalf("session cookie does not preserve hardened attributes: %#v", session)
	}
	csrf := byName[csrfCookieName]
	if csrf == nil || csrf.HttpOnly || !csrf.Secure || csrf.SameSite != http.SameSiteStrictMode || csrf.Value != "csrf-value" {
		t.Fatalf("CSRF cookie must be script-readable but otherwise hardened: %#v", csrf)
	}
}

func TestClearAuthenticationCookiesClearsBothBrowserCookies(t *testing.T) {
	server := &Server{deps: Dependencies{SecureCookies: true}}
	recorder := httptest.NewRecorder()

	server.clearAuthenticationCookies(recorder)

	cookies := recorder.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected two clearing cookies, got %d", len(cookies))
	}
	for _, cookie := range cookies {
		if cookie.MaxAge >= 0 || cookie.Value != "" {
			t.Fatalf("cookie %s was not cleared: %#v", cookie.Name, cookie)
		}
	}
}

func TestMeReturnsAuthoritativeRuntimeAndPlatformScopeContext(t *testing.T) {
	server := &Server{deps: Dependencies{
		Environment:    "production",
		Classification: "INTERNAL",
	}}
	user := identity.User{
		ID: "user-1", Email: "operator@example.test", DisplayName: "Operator",
		Permissions: map[string]struct{}{"campaign.read": {}},
	}
	session := identity.Session{ID: "session-1"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req = req.WithContext(identity.WithPrincipal(req.Context(), identity.Principal{
		User: user, Session: session, SessionID: session.ID,
	}))
	recorder := httptest.NewRecorder()

	server.me(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["environment"] != "production" || payload["classification"] != "INTERNAL" {
		t.Fatalf("missing authoritative runtime banners: %#v", payload)
	}
	if payload["scopeType"] != "PLATFORM" {
		t.Fatalf("identity scope must reflect current platform-wide RBAC model: %#v", payload)
	}
	orgs, ok := payload["organisationIds"].([]any)
	if !ok || len(orgs) != 0 {
		t.Fatalf("platform scope must not invent organisation bindings: %#v", payload["organisationIds"])
	}
}

func TestCookieAuthenticationWithMalformedAuthorizationStillRequiresCSRF(t *testing.T) {
	hash, err := identity.HashPassword("a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.User{
		ID: "csrf-user", Email: "csrf@example.test", DisplayName: "CSRF User",
		Status: identity.StatusActive, PasswordHash: hash, MFARequired: false,
		Permissions: map[string]struct{}{"campaign.write": {}},
	}
	service := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := service.Login(context.Background(), user.Email, "a-very-long-development-password")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{deps: Dependencies{Identity: service}}
	protected := server.require("campaign.write", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	missingCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/1/draft", nil)
	missingCSRF.AddCookie(&http.Cookie{Name: sessionCookieName, Value: login.SessionToken})
	missingCSRF.Header.Set("Authorization", "Basic malformed")
	response := httptest.NewRecorder()
	protected.ServeHTTP(response, missingCSRF)
	if response.Code != http.StatusForbidden {
		t.Fatalf("malformed Authorization must not bypass cookie CSRF, got %d", response.Code)
	}

	validCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/1/draft", nil)
	validCSRF.AddCookie(&http.Cookie{Name: sessionCookieName, Value: login.SessionToken})
	validCSRF.Header.Set("Authorization", "Basic malformed")
	validCSRF.Header.Set("X-CSRF-Token", login.CSRFToken)
	response = httptest.NewRecorder()
	protected.ServeHTTP(response, validCSRF)
	if response.Code != http.StatusNoContent {
		t.Fatalf("valid CSRF should permit cookie-authenticated request, got %d", response.Code)
	}
}
