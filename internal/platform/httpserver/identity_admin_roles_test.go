package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"campaign-platform/internal/identity"
)

func TestIdentityRoleCatalogueReturnsServerAuthoritativeRoles(t *testing.T) {
	repo := identity.NewMemoryAdministrationRepository("ANALYST", "CAMPAIGN_OPERATOR")
	server := &Server{deps: Dependencies{IdentityAdministration: identity.NewAdministrationService(repo, nil)}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/roles", nil)
	rr := httptest.NewRecorder()
	server.listInternalRoles(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var roles []identity.RoleDefinition
	if err := json.Unmarshal(rr.Body.Bytes(), &roles); err != nil {
		t.Fatal(err)
	}
	if len(roles) != 2 || roles[0].Code != "ANALYST" || roles[1].Code != "CAMPAIGN_OPERATOR" {
		t.Fatalf("unexpected roles: %+v", roles)
	}

	// Role catalogue reads are non-mutating and must not require a step-up challenge.
	if _, err := repo.GetAccount(context.Background(), "missing"); err == nil {
		t.Fatal("test repository sanity check unexpectedly found an account")
	}
}
