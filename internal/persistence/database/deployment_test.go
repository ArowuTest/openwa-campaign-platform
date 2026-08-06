package database

import "testing"

func TestValidateDeploymentDSNRequiresTLS(t *testing.T) {
	for _, value := range []string{"postgres://user:pass@example/db", "postgres://user:pass@example/db?sslmode=disable", "host=example dbname=db sslmode=disable"} {
		if err := ValidateDeploymentDSN("production", value); err == nil {
			t.Fatalf("unsafe DSN accepted: %s", value)
		}
	}
	for _, value := range []string{"postgres://user:pass@example/db?sslmode=require", "host=example dbname=db sslmode=verify-full"} {
		if err := ValidateDeploymentDSN("production", value); err != nil {
			t.Fatalf("safe DSN rejected: %s: %v", value, err)
		}
	}
}

func TestDefaultServiceRole(t *testing.T) {
	if got := DefaultServiceRole("platform-governance-worker"); got != "campaign_platform_governance_worker" {
		t.Fatalf("role=%q", got)
	}
}

func TestValidateServiceIdentityAllowsRotatableSingleRoleLogin(t *testing.T) {
	if err := validateServiceIdentity("prod_control_api_202608", "campaign_control_api", false, true, []string{"campaign_control_api"}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateServiceIdentityRejectsSuperuser(t *testing.T) {
	if err := validateServiceIdentity("neondb_owner", "campaign_control_api", true, true, []string{"campaign_control_api"}); err == nil {
		t.Fatal("superuser service identity was accepted")
	}
}

func TestValidateServiceIdentityRejectsMissingMembership(t *testing.T) {
	if err := validateServiceIdentity("prod_control_api_202608", "campaign_control_api", false, false, nil); err == nil {
		t.Fatal("login without expected service membership was accepted")
	}
}

func TestValidateServiceIdentityRejectsCrossServiceMembership(t *testing.T) {
	if err := validateServiceIdentity("shared_worker", "campaign_metrics_worker", false, true, []string{"campaign_campaign_worker", "campaign_metrics_worker"}); err == nil {
		t.Fatal("cross-service database identity was accepted")
	}
}
