package sender

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"campaign-platform/internal/platformpolicy"
)

func activateTransportRuntimeConfig(t *testing.T, admin *platformpolicy.ConfigurationAdministration, value platformpolicy.Configuration, suffix string) platformpolicy.Configuration {
	t.Helper()
	created, err := admin.Create(context.Background(), value, "maker-"+suffix, "create transport recovery policy")
	if err != nil {
		t.Fatal(err)
	}
	created, err = admin.Submit(context.Background(), created.ID, created.Version, "submitter-"+suffix, "submit transport recovery policy")
	if err != nil {
		t.Fatal(err)
	}
	created, err = admin.Decide(context.Background(), created.ID, created.Version, true, "approver-"+suffix, "approve transport recovery policy")
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func transportRuntimeJSON(mode string, attempts int, baseDelay, reset, probe int64, failures int, teardown int64) json.RawMessage {
	value := map[string]any{
		"reconnectMode": mode, "reconnectMaxAttempts": attempts,
		"reconnectBaseDelayMs": baseDelay, "reconnectStabilityResetMs": reset,
		"watchdogProbeTimeoutMs": probe, "watchdogFailureThreshold": failures,
		"engineTeardownTimeoutMs": teardown,
	}
	raw, _ := json.Marshal(value)
	return raw
}
func TestPlatformTransportRuntimeResolverPrefersSenderSessionOverride(t *testing.T) {
	now := time.Date(2026, 8, 7, 17, 0, 0, 0, time.UTC)
	admin := &platformpolicy.ConfigurationAdministration{Store: platformpolicy.NewMemoryStore(), Clock: func() time.Time { return now }}
	activateTransportRuntimeConfig(t, admin, platformpolicy.Configuration{
		Key: TransportRuntimeConfigurationKey, ScopeType: platformpolicy.ScopePlatform,
		Value: transportRuntimeJSON("UNBOUNDED", 0, 5000, 300000, 15000, 2, 30000),
	}, "platform")
	specific := activateTransportRuntimeConfig(t, admin, platformpolicy.Configuration{
		Key: TransportRuntimeConfigurationKey, ScopeType: platformpolicy.ScopeSenderSession, ScopeID: "session-1",
		Value: transportRuntimeJSON("BOUNDED", 4, 7000, 240000, 9000, 3, 45000),
	}, "session")

	resolver := &PlatformTransportRuntimeResolver{Configurations: admin}
	policy, err := resolver.ResolveTransportRuntime(context.Background(), GovernedSession{ID: "session-1", PoolID: "pool-1", GatewayPoolID: "gateway-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if policy == nil {
		t.Fatal("expected governed transport runtime policy")
	}
	if policy.ReconnectMode != ReconnectBounded || policy.ReconnectMaxAttempts != 4 || policy.WatchdogFailureThreshold != 3 {
		t.Fatalf("wrong resolved policy: %+v", policy)
	}
	if policy.ConfigurationID != specific.ID || policy.ScopeType != string(platformpolicy.ScopeSenderSession) || policy.Version != specific.Version {
		t.Fatalf("wrong policy evidence: %+v", policy)
	}
}
func TestPlatformTransportRuntimeResolverRejectsUnsafeConfiguration(t *testing.T) {
	now := time.Date(2026, 8, 7, 17, 0, 0, 0, time.UTC)
	admin := &platformpolicy.ConfigurationAdministration{Store: platformpolicy.NewMemoryStore(), Clock: func() time.Time { return now }}
	activateTransportRuntimeConfig(t, admin, platformpolicy.Configuration{
		Key: TransportRuntimeConfigurationKey, ScopeType: platformpolicy.ScopePlatform,
		Value: transportRuntimeJSON("BOUNDED", 0, 5000, 300000, 15000, 0, 30000),
	}, "invalid")
	resolver := &PlatformTransportRuntimeResolver{Configurations: admin}
	if _, err := resolver.ResolveTransportRuntime(context.Background(), GovernedSession{}, now); err == nil {
		t.Fatal("unsafe transport runtime configuration was accepted")
	}
}

func TestDecodeTransportRuntimeRejectsEvidenceOnlyFields(t *testing.T) {
	value := map[string]any{}
	if err := json.Unmarshal(transportRuntimeJSON("UNBOUNDED", 0, 5000, 300000, 15000, 2, 30000), &value); err != nil {
		t.Fatal(err)
	}
	value["source"] = "DEPLOYMENT_BOOTSTRAP"
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeTransportRuntime(raw); err == nil {
		t.Fatal("stored transport policy accepted evidence-only source field")
	}
}
func TestPlatformTransportRuntimeResolverUsesDeploymentFallbackWhenNoGovernedValueExists(t *testing.T) {
	resolver := &PlatformTransportRuntimeResolver{Configurations: &platformpolicy.ConfigurationAdministration{Store: platformpolicy.NewMemoryStore()}}
	policy, err := resolver.ResolveTransportRuntime(context.Background(), GovernedSession{ID: "session-1"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if policy != nil {
		t.Fatalf("expected deployment fallback, got %+v", policy)
	}
}
