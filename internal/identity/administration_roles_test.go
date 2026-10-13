package identity

import (
	"context"
	"testing"
)

func TestAdministrationListsAuthoritativeRoleCatalogueDefensively(t *testing.T) {
	repo := NewMemoryAdministrationRepository("ANALYST", "CAMPAIGN_OPERATOR")
	service := NewAdministrationService(repo, NewMemorySessionRepository())

	roles, err := service.ListRoles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles, got %d: %+v", len(roles), roles)
	}
	if roles[0].Code != "ANALYST" || roles[0].Name != "Analyst" || !containsPermission(roles[0].Permissions, "operations.read") || !containsPermission(roles[0].Permissions, "export.request") {
		t.Fatalf("unexpected first role: %+v", roles[0])
	}
	if roles[1].Code != "CAMPAIGN_OPERATOR" || roles[1].Name != "Campaign operator" || !containsPermission(roles[1].Permissions, "audience.import_mapping.write") {
		t.Fatalf("unexpected second role: %+v", roles[1])
	}

	roles[0].Permissions[0] = "mutated"
	again, err := service.ListRoles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Permissions[0] == "mutated" {
		t.Fatalf("role catalogue leaked mutable permission storage: %+v", again[0])
	}
}

func containsPermission(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
