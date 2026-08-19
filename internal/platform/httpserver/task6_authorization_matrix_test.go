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

	"campaign-platform/internal/commercial"
	"campaign-platform/internal/execution"
	"campaign-platform/internal/identity"
	"campaign-platform/internal/operations"
	"campaign-platform/internal/orchestration"
	"campaign-platform/internal/platformpolicy"
	"campaign-platform/internal/privacy"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/retention"
	"campaign-platform/internal/sender"
)

type task6RoutePermission struct {
	method     string
	path       string
	permission string
}

var task6HighRiskRoutePermissions = []task6RoutePermission{
	{http.MethodPost, "/api/v1/campaigns/campaign-1/commercial-record", "finance.write"},
	{http.MethodPost, "/api/v1/commercial-records/record-1/submit", "finance.write"},
	{http.MethodPost, "/api/v1/commercial-records/record-1/decision", "finance.approve"},
	{http.MethodPost, "/api/v1/commercial-records/record-1/revoke", "finance.approve"},
	{http.MethodPost, "/api/v1/campaigns/campaign-1/execution/start", "campaign.operate"},
	{http.MethodPost, "/api/v1/campaigns/campaign-1/release", "campaign.operate"},
	{http.MethodPost, "/api/v1/routing-plans/plan-1/release", "campaign.operate"},
	{http.MethodPut, "/api/v1/sender-sessions/session-1/metadata", "sender.admin"},
	{http.MethodPut, "/api/v1/sender-sessions/session-1/proxy", "sender.admin"},
	{http.MethodDelete, "/api/v1/sender-sessions/session-1/proxy", "sender.admin"},
	{http.MethodPost, "/api/v1/sender-sessions/session-1/quarantine", "sender.operate"},
	{http.MethodPost, "/api/v1/sender-sessions/session-1/reinstate", "sender.admin"},
	{http.MethodGet, "/api/v1/sender-sessions/session-1/pairing/qr", "sender.admin"},
	{http.MethodPost, "/api/v1/sender-sessions/session-1/pairing/code", "sender.admin"},
	{http.MethodPost, "/api/v1/sender-sessions/session-1/logout", "sender.admin"},
	{http.MethodDelete, "/api/v1/sender-sessions/session-1", "sender.admin"},
	{http.MethodPost, "/api/v1/exports", "export.request"},
	{http.MethodPost, "/api/v1/exports/export-1/decision", "export.approve"},
	{http.MethodPost, "/api/v1/exports/export-1/download-authorisations", "export.request"},
	{http.MethodPost, "/api/v1/exports/export-1/revoke", "export.approve"},
	{http.MethodPost, "/api/v1/privacy/legal-holds", "privacy.hold"},
	{http.MethodPost, "/api/v1/privacy/legal-holds/hold-1/submit", "privacy.hold"},
	{http.MethodPost, "/api/v1/privacy/legal-holds/hold-1/decision", "privacy.approve"},
	{http.MethodPost, "/api/v1/privacy/legal-holds/hold-1/release", "privacy.hold"},
	{http.MethodPost, "/api/v1/admin/retention-policies", "retention.write"},
	{http.MethodPost, "/api/v1/admin/retention-policies/policy-1/submit", "retention.write"},
	{http.MethodPost, "/api/v1/admin/retention-policies/policy-1/decision", "retention.approve"},
	{http.MethodPost, "/api/v1/admin/retention-policies/policy-1/retire", "retention.approve"},
	{http.MethodPost, "/api/v1/admin/configurations", "configuration.write"},
	{http.MethodPost, "/api/v1/admin/configurations/config-1/submit", "configuration.write"},
	{http.MethodPost, "/api/v1/admin/configurations/config-1/decision", "configuration.approve"},
	{http.MethodPost, "/api/v1/admin/configurations/config-1/retire", "configuration.approve"},
	{http.MethodPost, "/api/v1/admin/configurations/config-1/rollback", "configuration.approve"},
	{http.MethodPost, "/api/v1/admin/provider-capabilities", "configuration.write"},
	{http.MethodPost, "/api/v1/admin/provider-capabilities/provider-1/submit", "configuration.write"},
	{http.MethodPost, "/api/v1/admin/provider-capabilities/provider-1/decision", "configuration.approve"},
	{http.MethodPost, "/api/v1/admin/provider-capabilities/provider-1/retire", "configuration.approve"},
	{http.MethodPost, "/api/v1/admin/sender-pacing-policies", "configuration.write"},
	{http.MethodPost, "/api/v1/admin/sender-pacing-policies/pacing-1/submit", "configuration.write"},
	{http.MethodPost, "/api/v1/admin/sender-pacing-policies/pacing-1/decision", "configuration.approve"},
}

const task6Password = "Task6-Authorization-Matrix-Password-19"

func task6PasswordHash(t *testing.T) string {
	t.Helper()
	hash, err := identity.HashPassword(task6Password)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func task6Identity(t *testing.T, passwordHash string, permissions ...string) (*identity.Service, string) {
	t.Helper()
	grants := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		grants[permission] = struct{}{}
	}
	user := identity.User{
		ID: "task6-user", Email: "task6-user@example.test", DisplayName: "Task 6 User",
		Status: identity.StatusActive, PasswordHash: passwordHash, Permissions: grants,
	}
	service := identity.NewService(identity.NewMemoryRepository(user), 30*time.Minute, 12*time.Hour)
	login, err := service.Login(context.Background(), user.Email, task6Password)
	if err != nil {
		t.Fatal(err)
	}
	return service, login.SessionToken
}

func task6Handler(deps Dependencies) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(logger, deps).Handler()
}

func task6Request(method, path, token string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(`{}`))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request
}

func TestTask6HighRiskRoutesRejectMissingAndWrongAuthority(t *testing.T) {
	passwordHash := task6PasswordHash(t)
	wrongIdentity, wrongToken := task6Identity(t, passwordHash, "task6.unrelated")
	wrongHandler := task6Handler(Dependencies{Identity: wrongIdentity})
	type authenticatedHandler struct {
		handler http.Handler
		token   string
	}
	allowedByPermission := map[string]authenticatedHandler{}
	for _, route := range task6HighRiskRoutePermissions {
		if _, exists := allowedByPermission[route.permission]; exists {
			continue
		}
		allowedIdentity, allowedToken := task6Identity(t, passwordHash, route.permission)
		allowedByPermission[route.permission] = authenticatedHandler{
			handler: task6Handler(Dependencies{Identity: allowedIdentity}),
			token:   allowedToken,
		}
	}

	for _, test := range task6HighRiskRoutePermissions {
		test := test
		t.Run(test.method+"_"+test.path, func(t *testing.T) {
			unauthenticated := httptest.NewRecorder()
			wrongHandler.ServeHTTP(unauthenticated, task6Request(test.method, test.path, ""))
			if unauthenticated.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated status=%d body=%s", unauthenticated.Code, unauthenticated.Body.String())
			}

			denied := httptest.NewRecorder()
			wrongHandler.ServeHTTP(denied, task6Request(test.method, test.path, wrongToken))
			if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "PERMISSION_DENIED") {
				t.Fatalf("wrong permission status=%d body=%s", denied.Code, denied.Body.String())
			}

			fixture := allowedByPermission[test.permission]
			allowed := httptest.NewRecorder()
			fixture.handler.ServeHTTP(allowed, task6Request(test.method, test.path, fixture.token))
			if strings.Contains(allowed.Body.String(), "PERMISSION_DENIED") {
				t.Fatalf("expected permission %q was rejected: status=%d body=%s", test.permission, allowed.Code, allowed.Body.String())
			}
		})
	}
}

func TestTask6PrivilegedMutationsRequireRecentMFABeforeDomainWork(t *testing.T) {
	passwordHash := task6PasswordHash(t)
	permissions := []string{
		"campaign.operate", "sender.admin", "sender.operate", "export.request", "export.approve",
		"privacy.hold", "privacy.approve", "retention.approve", "configuration.approve", "finance.approve",
	}
	identityService, token := task6Identity(t, passwordHash, permissions...)
	handler := task6Handler(Dependencies{
		Identity:               identityService,
		Commercial:             &commercial.Service{},
		Releases:               &orchestration.ReleaseService{},
		Execution:              &execution.Coordinator{},
		RoutingPlans:           &execution.RoutingAdministration{},
		SenderGovernance:       &sender.GovernanceService{},
		SenderSessionProxies:   &sender.SessionProxyAdministration{},
		SenderSessionLifecycle: &sender.SessionLifecycleService{},
		PacingPolicies:         &sender.PacingAdministration{},
		Operations:             &operations.Service{},
		PrivacyCases:           &privacy.Service{},
		Retention:              &retention.Administration{},
		Configurations:         &platformpolicy.ConfigurationAdministration{},
		ProviderCapabilities:   &provider.Service{},
	})
	routes := []task6RoutePermission{
		{http.MethodPost, "/api/v1/commercial-records/record-1/decision", "finance.approve"},
		{http.MethodPost, "/api/v1/commercial-records/record-1/revoke", "finance.approve"},
		{http.MethodPost, "/api/v1/campaigns/campaign-1/execution/start", "campaign.operate"},
		{http.MethodPost, "/api/v1/campaigns/campaign-1/release", "campaign.operate"},
		{http.MethodPost, "/api/v1/routing-plans/plan-1/release", "campaign.operate"},
		{http.MethodPut, "/api/v1/sender-sessions/session-1/metadata", "sender.admin"},
		{http.MethodPut, "/api/v1/sender-sessions/session-1/proxy", "sender.admin"},
		{http.MethodDelete, "/api/v1/sender-sessions/session-1/proxy", "sender.admin"},
		{http.MethodPost, "/api/v1/sender-sessions/session-1/quarantine", "sender.operate"},
		{http.MethodPost, "/api/v1/sender-sessions/session-1/reinstate", "sender.admin"},
		{http.MethodGet, "/api/v1/sender-sessions/session-1/pairing/qr", "sender.admin"},
		{http.MethodPost, "/api/v1/sender-sessions/session-1/pairing/code", "sender.admin"},
		{http.MethodPost, "/api/v1/sender-sessions/session-1/logout", "sender.admin"},
		{http.MethodDelete, "/api/v1/sender-sessions/session-1", "sender.admin"},
		{http.MethodPost, "/api/v1/exports", "export.request"},
		{http.MethodPost, "/api/v1/exports/export-1/decision", "export.approve"},
		{http.MethodPost, "/api/v1/exports/export-1/download-authorisations", "export.request"},
		{http.MethodPost, "/api/v1/exports/export-1/revoke", "export.approve"},
		{http.MethodPost, "/api/v1/privacy/legal-holds", "privacy.hold"},
		{http.MethodPost, "/api/v1/privacy/legal-holds/hold-1/decision", "privacy.approve"},
		{http.MethodPost, "/api/v1/privacy/legal-holds/hold-1/release", "privacy.hold"},
		{http.MethodPost, "/api/v1/admin/retention-policies/policy-1/decision", "retention.approve"},
		{http.MethodPost, "/api/v1/admin/retention-policies/policy-1/retire", "retention.approve"},
		{http.MethodPost, "/api/v1/admin/configurations/config-1/decision", "configuration.approve"},
		{http.MethodPost, "/api/v1/admin/configurations/config-1/retire", "configuration.approve"},
		{http.MethodPost, "/api/v1/admin/configurations/config-1/rollback", "configuration.approve"},
		{http.MethodPost, "/api/v1/admin/provider-capabilities/provider-1/decision", "configuration.approve"},
		{http.MethodPost, "/api/v1/admin/provider-capabilities/provider-1/retire", "configuration.approve"},
		{http.MethodPost, "/api/v1/admin/sender-pacing-policies/pacing-1/decision", "configuration.approve"},
	}
	for _, test := range routes {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, task6Request(test.method, test.path, token))
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "STEP_UP_REQUIRED") {
			t.Errorf("%s %s reached domain work without fresh MFA: status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
}
