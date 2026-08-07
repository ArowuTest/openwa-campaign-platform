package sender

import (
	"context"
	"testing"
	"time"
)

type fixedHealthSignals struct{ signals HealthSignals }

func (f fixedHealthSignals) ReadHealthSignals(context.Context, GovernedSession, HealthPolicy, time.Time) (HealthSignals, error) {
	return f.signals, nil
}

func TestHealthAssessmentUsesFailureAndDisconnectThresholds(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	policy := DefaultHealthPolicy()
	svc := &GovernanceService{Store: store, HealthPolicies: fixedHealthPolicyResolver{policy: policy, evidence: HealthPolicyEvidence{Source: "SAFE_DEFAULT"}}, HealthSignals: fixedHealthSignals{signals: HealthSignals{
		RecentOutcomeCount: 40, RecentFailureCount: 8, RecentDisconnectCount: policy.DisconnectThreshold,
	}}}
	session := registerReadySession(t, ctx, svc, store, GovernedSession{MaskedMSISDN: "+234 ***", EngineType: "openwa", SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 1})
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	heartbeat := now.Add(-10 * time.Second)
	success := now.Add(-time.Minute)
	store.mu.Lock()
	value := store.sessions[session.ID]
	value.LastHeartbeatAt = &heartbeat
	value.LastSuccessAt = &success
	store.sessions[session.ID] = value
	store.mu.Unlock()

	assessment, err := svc.AssessSession(ctx, session.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if !containsReason(assessment.Reasons, "FAILURE_RATE_THRESHOLD_BREACHED") || !containsReason(assessment.Reasons, "DISCONNECT_THRESHOLD_BREACHED") {
		t.Fatalf("threshold reasons missing: %+v", assessment)
	}
	if assessment.RecentFailureRateBPS != 2000 || assessment.State != "CRITICAL" {
		t.Fatalf("threshold evidence/state unexpected: %+v", assessment)
	}
}
func TestHealthEnforcerDrainsBreachedReadySender(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	policy := DefaultHealthPolicy()
	svc := &GovernanceService{Store: store, HealthPolicies: fixedHealthPolicyResolver{policy: policy, evidence: HealthPolicyEvidence{Source: "SAFE_DEFAULT"}}, HealthSignals: fixedHealthSignals{signals: HealthSignals{
		RecentOutcomeCount: int64(policy.FailureMinimumSamples), RecentFailureCount: int64(policy.FailureMinimumSamples),
	}}}
	session := registerReadySession(t, ctx, svc, store, GovernedSession{MaskedMSISDN: "+234 ***", EngineType: "openwa", SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 1})
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	heartbeat := now.Add(-10 * time.Second)
	success := now.Add(-time.Minute)
	store.mu.Lock()
	value := store.sessions[session.ID]
	value.LastHeartbeatAt = &heartbeat
	value.LastSuccessAt = &success
	store.sessions[session.ID] = value
	store.mu.Unlock()

	enforcer := &HealthEnforcer{Governance: svc, ActorID: "actor", Clock: func() time.Time { return now }}
	count, err := enforcer.Process(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := store.GetSession(ctx, session.ID)
	if count != 1 || updated.Status != StatusDraining || updated.Version != session.Version+1 {
		t.Fatalf("sender was not drained: count=%d sender=%+v", count, updated)
	}
}
