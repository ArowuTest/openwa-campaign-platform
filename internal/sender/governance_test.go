package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func registerReadySession(t *testing.T, ctx context.Context, svc *GovernanceService, store *MemoryGovernanceStore, value GovernedSession) GovernedSession {
	t.Helper()
	value.Status = StatusNew
	if value.OwnerReference == "" {
		value.OwnerReference = "test-operations"
	}
	if value.RegistrationCountryISO2 == "" {
		value.RegistrationCountryISO2 = "NG"
	}
	if value.ProfileDisplayName == "" {
		value.ProfileDisplayName = "Test sender"
	}
	if value.RecoveryReference == "" {
		value.RecoveryReference = "vault://test/sender-recovery"
	}
	session, err := svc.RegisterSession(ctx, value, []byte("cipher"), "actor", "approved sender")
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []Status{StatusPairing, StatusConnecting, StatusReady} {
		session, err = svc.TransitionSession(ctx, session.ID, session.Version, status, "actor", "complete sender lifecycle")
		if err != nil {
			t.Fatal(err)
		}
	}
	return session
}
func TestGovernedSenderCapacityAndConflicts(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	svc := &GovernanceService{Store: store}
	p, err := svc.CreatePool(ctx, Pool{Name: "Nigeria primary", MaxMessagesPerMinute: 120, DailyCapacity: 10000, ReservedCapacity: 1000}, "actor", "approved pool")
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.RegisterNode(ctx, Node{Name: "worker-a", Status: "READY", Capacity: 5}, "actor", "approved node")
	if err != nil {
		t.Fatal(err)
	}
	s := registerReadySession(t, ctx, svc, store, GovernedSession{NodeID: n.ID, PoolID: p.ID, MaskedMSISDN: "+234 801 *** 1234", EngineType: "openwa", SafeMessagesPerMinute: 50, SafeDailyCapacity: 5000, InFlightLimit: 2})
	now := time.Now().UTC()
	s, err = store.HeartbeatSession(ctx, s.ID, s.Version, GovernedSession{Status: StatusReady, EngineVersion: "test", SafeMessagesPerMinute: 50, SafeDailyCapacity: 5000, InFlightLimit: 2, SentToday: 500}, now)
	if err != nil {
		t.Fatal(err)
	}
	n, err = store.HeartbeatNode(ctx, n.ID, n.Version, Node{Status: "READY", BuildVersion: "test", Capacity: 5}, now)
	if err != nil {
		t.Fatal(err)
	}
	cap, err := store.Capacity(ctx, p.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if cap.ReadySessions != 1 || cap.AvailableMessagesPerMinute != 50 || cap.AvailableDailyCapacity != 3500 {
		t.Fatalf("unexpected capacity %#v", cap)
	}
	if _, err := svc.TransitionSession(ctx, s.ID, s.Version-1, StatusPaused, "actor", "pause"); err != ErrSenderConflict {
		t.Fatalf("expected conflict got %v", err)
	}
}

func TestQuarantineExcludesCapacityAndHeartbeatCannotReinstate(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	svc := &GovernanceService{Store: store}
	pool, _ := svc.CreatePool(ctx, Pool{Name: "Quarantine pool", MaxMessagesPerMinute: 100, DailyCapacity: 1000}, "actor", "approved pool")
	node, _ := svc.RegisterNode(ctx, Node{Name: "worker-q", Status: "READY", Capacity: 1}, "actor", "approved node")
	session := registerReadySession(t, ctx, svc, store, GovernedSession{NodeID: node.ID, PoolID: pool.ID, MaskedMSISDN: "+234 ***", EngineType: "openwa", SafeMessagesPerMinute: 25, SafeDailyCapacity: 500, InFlightLimit: 1})
	now := time.Now().UTC()
	node, _ = store.HeartbeatNode(ctx, node.ID, node.Version, Node{Status: "READY", Capacity: 1}, now)
	session, _ = store.HeartbeatSession(ctx, session.ID, session.Version, GovernedSession{Status: StatusReady, SafeMessagesPerMinute: 25, SafeDailyCapacity: 500, InFlightLimit: 1}, now)
	quarantined, err := svc.QuarantineSession(ctx, session.ID, session.Version, "operator", "failure threshold exceeded")
	if err != nil {
		t.Fatal(err)
	}
	if quarantined.Status != StatusQuarantined || quarantined.QuarantinedAt == nil || quarantined.QuarantineReason == "" {
		t.Fatalf("missing quarantine evidence: %+v", quarantined)
	}
	heartbeat, err := store.HeartbeatSession(ctx, quarantined.ID, quarantined.Version, GovernedSession{Status: StatusReady, SafeMessagesPerMinute: 25, SafeDailyCapacity: 500, InFlightLimit: 1}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if heartbeat.Status != StatusQuarantined {
		t.Fatalf("heartbeat bypassed quarantine: %s", heartbeat.Status)
	}
	capacity, _ := store.Capacity(ctx, pool.ID, now.Add(time.Second))
	if capacity.ReadySessions != 0 || capacity.AvailableMessagesPerMinute != 0 {
		t.Fatalf("quarantined session contributed capacity: %+v", capacity)
	}
	reinstated, err := svc.ReinstateSession(ctx, heartbeat.ID, heartbeat.Version, "admin", "health verification passed")
	if err != nil {
		t.Fatal(err)
	}
	if reinstated.Status != StatusReady || reinstated.ReinstatedAt == nil || reinstated.QuarantineReason != "" {
		t.Fatalf("invalid reinstatement evidence: %+v", reinstated)
	}
}

func TestQuarantineRequiresDedicatedOperation(t *testing.T) {
	store := NewMemoryGovernanceStore()
	svc := &GovernanceService{Store: store}
	if _, err := svc.TransitionSession(context.Background(), "session", 1, StatusQuarantined, "actor", "quarantine it"); err == nil {
		t.Fatal("generic transition accepted quarantine")
	}
	if _, err := svc.QuarantineSession(context.Background(), "session", 1, "actor", "short"); err == nil {
		t.Fatal("short quarantine reason accepted")
	}
}

func TestHealthAssessmentRecommendsDrainAndQuarantine(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	svc := &GovernanceService{Store: store}
	session, err := svc.RegisterSession(ctx, GovernedSession{MaskedMSISDN: "+234 ***", OwnerReference: "test-operations", RegistrationCountryISO2: "NG", ProfileDisplayName: "Test sender", RecoveryReference: "vault://test/sender-recovery", EngineType: "openwa", Status: StatusNew, SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 1, SentToday: 95}, []byte("cipher"), "actor", "approved sender")
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.TransitionSession(ctx, session.ID, session.Version, StatusPairing, "actor", "begin sender pairing")
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.TransitionSession(ctx, session.ID, session.Version, StatusDisconnected, "actor", "pairing connection lost")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	stale := now.Add(-4 * time.Minute)
	store.mu.Lock()
	value := store.sessions[session.ID]
	value.LastHeartbeatAt = &stale
	store.sessions[session.ID] = value
	store.mu.Unlock()
	assessment, err := svc.AssessSession(ctx, session.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.State != "CRITICAL" || assessment.Recommendation != "QUARANTINE_OR_KEEP_OUT_OF_ALLOCATION" || assessment.Score >= 30 {
		t.Fatalf("unexpected health decision: %+v", assessment)
	}
	if assessment.CapacityUsedPct != 95 || assessment.HeartbeatAgeSec < 200 {
		t.Fatalf("missing health evidence: %+v", assessment)
	}
}

func TestHealthAssessmentUsesLastSuccessRecency(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	svc := &GovernanceService{Store: store}
	session := registerReadySession(t, ctx, svc, store, GovernedSession{MaskedMSISDN: "+234 ***", EngineType: "openwa", SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 1})
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	freshHeartbeat := now.Add(-10 * time.Second)
	staleSuccess := now.Add(-25 * time.Hour)
	store.mu.Lock()
	value := store.sessions[session.ID]
	value.LastHeartbeatAt = &freshHeartbeat
	value.LastSuccessAt = &staleSuccess
	value.SentToday = 10
	store.sessions[session.ID] = value
	store.mu.Unlock()

	assessment, err := svc.AssessSession(ctx, session.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !containsReason(assessment.Reasons, "SUCCESS_CRITICALLY_STALE") || assessment.Score >= 100 {
		t.Fatalf("stale success evidence did not affect health: %+v", assessment)
	}
}

type fixedHealthPolicyResolver struct {
	policy   HealthPolicy
	evidence HealthPolicyEvidence
}

func (r fixedHealthPolicyResolver) ResolveHealthPolicy(context.Context, GovernedSession, time.Time) (HealthPolicy, HealthPolicyEvidence, error) {
	return r.policy, r.evidence, nil
}

func TestHealthAssessmentUsesGovernedThresholdsAndReportsEvidence(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	policy := DefaultHealthPolicy()
	policy.SuccessStaleAfter = 30 * time.Minute
	policy.SuccessCriticalAfter = time.Hour
	svc := &GovernanceService{Store: store, HealthPolicies: fixedHealthPolicyResolver{
		policy:   policy,
		evidence: HealthPolicyEvidence{Source: "GOVERNED_CONFIGURATION", ConfigurationID: "config-1", ScopeType: "SENDER_POOL", ScopeID: "pool-1", Version: 4},
	}}
	session := registerReadySession(t, ctx, svc, store, GovernedSession{PoolID: "pool-1", MaskedMSISDN: "+234 ***", EngineType: "openwa", SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 1})
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	heartbeat := now.Add(-10 * time.Second)
	success := now.Add(-2 * time.Hour)
	store.mu.Lock()
	value := store.sessions[session.ID]
	value.LastHeartbeatAt = &heartbeat
	value.LastSuccessAt = &success
	value.SentToday = 1
	store.sessions[session.ID] = value
	store.mu.Unlock()

	assessment, err := svc.AssessSession(ctx, session.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !containsReason(assessment.Reasons, "SUCCESS_CRITICALLY_STALE") {
		t.Fatalf("governed success threshold was not applied: %+v", assessment)
	}
	if assessment.PolicyConfigurationID != "config-1" || assessment.PolicyScopeType != "SENDER_POOL" || assessment.PolicyScopeID != "pool-1" || assessment.PolicyVersion != 4 {
		t.Fatalf("health policy evidence missing: %+v", assessment)
	}
}

func TestHealthAssessmentPenalisesMissingSuccessAfterTraffic(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	svc := &GovernanceService{Store: store}
	session := registerReadySession(t, ctx, svc, store, GovernedSession{MaskedMSISDN: "+234 ***", EngineType: "openwa", SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 1})
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	freshHeartbeat := now.Add(-10 * time.Second)
	store.mu.Lock()
	value := store.sessions[session.ID]
	value.LastHeartbeatAt = &freshHeartbeat
	value.LastSuccessAt = nil
	value.SentToday = 1
	store.sessions[session.ID] = value
	store.mu.Unlock()

	assessment, err := svc.AssessSession(ctx, session.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !containsReason(assessment.Reasons, "NO_SUCCESS_EVIDENCE") || assessment.Score >= 100 {
		t.Fatalf("missing success evidence did not affect health: %+v", assessment)
	}
}

func containsReason(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func TestRegisterSessionRequiresCompleteOperationalMetadata(t *testing.T) {
	svc := &GovernanceService{Store: NewMemoryGovernanceStore()}
	_, err := svc.RegisterSession(context.Background(), GovernedSession{
		MaskedMSISDN: "+234 ***", EngineType: "openwa", Status: StatusNew,
		SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 1,
	}, []byte("cipher"), "actor", "approved sender")
	if !errors.Is(err, ErrSenderMetadataInvalid) {
		t.Fatalf("expected metadata validation error, got %v", err)
	}
}

func TestUpdateSessionOperationalMetadataUsesOptimisticVersion(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	svc := &GovernanceService{Store: store}
	session, err := svc.RegisterSession(ctx, GovernedSession{
		MaskedMSISDN: "+234 ***", EngineType: "openwa", Status: StatusNew,
		OwnerReference: "team-a", RegistrationCountryISO2: "NG", ProfileDisplayName: "Primary",
		RecoveryReference: "vault://recovery/one", SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 1,
	}, []byte("cipher"), "actor", "approved sender")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateSessionMetadata(ctx, session.ID, session.Version, SessionOperationalMetadata{
		OwnerReference: "team-b", RegistrationCountryISO2: "GH", ProfileDisplayName: "Ghana primary", RecoveryReference: "vault://recovery/two",
	}, "actor", "change approved sender ownership")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != session.Version+1 || updated.OwnerReference != "team-b" || updated.RegistrationCountryISO2 != "GH" || !updated.RecoveryReferenceConfigured {
		t.Fatalf("unexpected metadata update: %+v", updated)
	}
	if _, err := svc.UpdateSessionMetadata(ctx, session.ID, session.Version, SessionOperationalMetadata{OwnerReference: "team-c", RegistrationCountryISO2: "NG", ProfileDisplayName: "Stale", RecoveryReference: "vault://recovery/stale"}, "actor", "stale ownership update"); !errors.Is(err, ErrSenderConflict) {
		t.Fatalf("expected optimistic conflict, got %v", err)
	}
}

func TestSenderOperationalMetadataNeverSerialisesRecoveryReference(t *testing.T) {
	value := GovernedSession{
		OwnerReference: "operations-team", RegistrationCountryISO2: "NG", ProfileDisplayName: "Primary sender",
		RecoveryReference: "vault://whatsapp/recovery/session-1", RecoveryReferenceConfigured: true,
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || bytes.Contains(raw, []byte("vault://whatsapp/recovery/session-1")) {
		t.Fatalf("recovery reference leaked in sender JSON: %s", raw)
	}
}
