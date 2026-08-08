package operations

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"campaign-platform/internal/platformpolicy"
)

func TestGatewayRuntimeHealthResolverUsesApprovedConfiguration(t *testing.T) {
	now := time.Date(2026, 8, 7, 18, 0, 0, 0, time.UTC)
	store := platformpolicy.NewMemoryStore()
	admin := &platformpolicy.ConfigurationAdministration{Store: store, Clock: func() time.Time { return now }}
	created, err := admin.Create(context.Background(), platformpolicy.Configuration{
		Key: GatewayRuntimeHealthConfigurationKey, ScopeType: platformpolicy.ScopePlatform,
		Value: json.RawMessage(`{"staleAfterSeconds":180}`),
	}, "maker", "configure gateway staleness")
	if err != nil {
		t.Fatal(err)
	}
	created, err = admin.Submit(context.Background(), created.ID, created.Version, "submitter", "submit gateway health policy")
	if err != nil {
		t.Fatal(err)
	}
	created, err = admin.Decide(context.Background(), created.ID, created.Version, true, "approver", "approve gateway health policy")
	if err != nil {
		t.Fatal(err)
	}
	resolver := &PlatformGatewayRuntimeHealthResolver{Configurations: admin, FallbackStaleAfter: 2 * time.Minute}
	policy, err := resolver.ResolveGatewayRuntimeHealth(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if policy.StaleAfter != 3*time.Minute || policy.Source != "GOVERNED_CONFIGURATION" || policy.ConfigurationID != created.ID {
		t.Fatalf("unexpected policy: %+v", policy)
	}
}

func TestGatewayRuntimeHealthPolicyRejectsUnknownAndUnsafeValues(t *testing.T) {
	for _, raw := range []string{
		`{"staleAfterSeconds":10}`,
		`{"staleAfterSeconds":180,"unexpected":true}`,
	} {
		if _, err := decodeGatewayRuntimeHealthPolicy([]byte(raw)); err == nil {
			t.Fatalf("unsafe gateway runtime policy accepted: %s", raw)
		}
	}
}

type staticGatewayRuntimeHealthResolver struct {
	policy GatewayRuntimeHealthPolicy
}

func (s staticGatewayRuntimeHealthResolver) ResolveGatewayRuntimeHealth(context.Context, time.Time) (GatewayRuntimeHealthPolicy, error) {
	return s.policy, nil
}

func TestPostgreSQLDashboardUsesResolvedGatewayStaleThreshold(t *testing.T) {
	now := time.Date(2026, 8, 7, 19, 0, 0, 0, time.UTC)
	repo := &PostgreSQLRepository{RuntimeHealth: staticGatewayRuntimeHealthResolver{policy: GatewayRuntimeHealthPolicy{
		StaleAfter: 3 * time.Minute, Source: "GOVERNED_CONFIGURATION", ConfigurationID: "config-1", Version: 7,
	}}}
	before, policy, err := repo.gatewayStaleBefore(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Equal(now.Add(-3*time.Minute)) || policy.ConfigurationID != "config-1" || policy.Version != 7 {
		t.Fatalf("unexpected stale boundary/policy: %s %+v", before, policy)
	}
}
