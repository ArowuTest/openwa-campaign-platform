package sender

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"campaign-platform/internal/platformpolicy"
)

func activateHealthConfig(t *testing.T, admin *platformpolicy.ConfigurationAdministration, value platformpolicy.Configuration, maker, submitter, approver string) platformpolicy.Configuration {
	t.Helper()
	created, err := admin.Create(context.Background(), value, maker, "create sender health policy")
	if err != nil {
		t.Fatal(err)
	}
	created, err = admin.Submit(context.Background(), created.ID, created.Version, submitter, "submit sender health policy")
	if err != nil {
		t.Fatal(err)
	}
	created, err = admin.Decide(context.Background(), created.ID, created.Version, true, approver, "approve sender health policy")
	if err != nil {
		t.Fatal(err)
	}
	return created
}
func TestPlatformHealthPolicyResolverUsesMostSpecificApprovedScope(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	store := platformpolicy.NewMemoryStore()
	admin := &platformpolicy.ConfigurationAdministration{Store: store, Clock: func() time.Time { return now }}
	platform := activateHealthConfig(t, admin, platformpolicy.Configuration{
		Key: HealthPolicyConfigurationKey, ScopeType: platformpolicy.ScopePlatform,
		Value: json.RawMessage(`{"heartbeatStaleSeconds":90,"heartbeatCriticalSeconds":180,"successStaleSeconds":21600,"successCriticalSeconds":86400,"capacityNearLimitPercent":90}`),
	}, "maker-p", "submitter-p", "approver-p")
	senderPool := activateHealthConfig(t, admin, platformpolicy.Configuration{
		Key: HealthPolicyConfigurationKey, ScopeType: platformpolicy.ScopeSenderPool, ScopeID: "pool-1",
		Value: json.RawMessage(`{"heartbeatStaleSeconds":120,"heartbeatCriticalSeconds":300,"successStaleSeconds":1800,"successCriticalSeconds":3600,"capacityNearLimitPercent":80}`),
	}, "maker-s", "submitter-s", "approver-s")

	resolver := &PlatformHealthPolicyResolver{Configurations: admin}
	policy, evidence, err := resolver.ResolveHealthPolicy(context.Background(), GovernedSession{PoolID: "pool-1", GatewayPoolID: "gateway-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if policy.SuccessCriticalAfter != time.Hour || policy.CapacityNearLimitPercent != 80 {
		t.Fatalf("sender-pool override not applied: %+v", policy)
	}
	if evidence.ConfigurationID != senderPool.ID || evidence.Version != senderPool.Version || evidence.ScopeType != string(platformpolicy.ScopeSenderPool) {
		t.Fatalf("wrong policy evidence: %+v platform=%s sender=%s", evidence, platform.ID, senderPool.ID)
	}
}
func TestPlatformHealthPolicyResolverRejectsUnsafeThresholds(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	admin := &platformpolicy.ConfigurationAdministration{Store: platformpolicy.NewMemoryStore(), Clock: func() time.Time { return now }}
	activateHealthConfig(t, admin, platformpolicy.Configuration{
		Key: HealthPolicyConfigurationKey, ScopeType: platformpolicy.ScopePlatform,
		Value: json.RawMessage(`{"heartbeatStaleSeconds":300,"heartbeatCriticalSeconds":120,"successStaleSeconds":3600,"successCriticalSeconds":7200,"capacityNearLimitPercent":90}`),
	}, "maker", "submitter", "approver")

	resolver := &PlatformHealthPolicyResolver{Configurations: admin}
	if _, _, err := resolver.ResolveHealthPolicy(context.Background(), GovernedSession{}, now); err == nil {
		t.Fatal("unsafe health threshold ordering was accepted")
	}
}

func TestPlatformHealthPolicyResolverPrefersSenderSessionOverride(t *testing.T) {
	now := time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC)
	admin := &platformpolicy.ConfigurationAdministration{Store: platformpolicy.NewMemoryStore(), Clock: func() time.Time { return now }}
	activateHealthConfig(t, admin, platformpolicy.Configuration{
		Key: HealthPolicyConfigurationKey, ScopeType: platformpolicy.ScopeSenderPool, ScopeID: "pool-1",
		Value: json.RawMessage(`{"heartbeatStaleSeconds":120,"heartbeatCriticalSeconds":300,"successStaleSeconds":1800,"successCriticalSeconds":3600,"capacityNearLimitPercent":80}`),
	}, "maker-pool", "submitter-pool", "approver-pool")
	sessionConfig := activateHealthConfig(t, admin, platformpolicy.Configuration{
		Key: HealthPolicyConfigurationKey, ScopeType: platformpolicy.ScopeSenderSession, ScopeID: "session-1",
		Value: json.RawMessage(`{"heartbeatStaleSeconds":45,"heartbeatCriticalSeconds":120,"successStaleSeconds":900,"successCriticalSeconds":1800,"capacityNearLimitPercent":70}`),
	}, "maker-session", "submitter-session", "approver-session")

	resolver := &PlatformHealthPolicyResolver{Configurations: admin}
	policy, evidence, err := resolver.ResolveHealthPolicy(context.Background(), GovernedSession{ID: "session-1", PoolID: "pool-1", GatewayPoolID: "gateway-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if policy.SuccessCriticalAfter != 30*time.Minute || policy.CapacityNearLimitPercent != 70 {
		t.Fatalf("sender-session override not applied: %+v", policy)
	}
	if evidence.ConfigurationID != sessionConfig.ID || evidence.ScopeType != string(platformpolicy.ScopeSenderSession) {
		t.Fatalf("wrong sender-session policy evidence: %+v", evidence)
	}
}
