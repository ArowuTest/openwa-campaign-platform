package operations

import (
	"context"
	"errors"
	"testing"
	"time"
)

type staticDashboard struct{ value Dashboard }

func (s *staticDashboard) Dashboard(context.Context) (Dashboard, error) { return s.value, nil }

func activateAlertPolicy(t *testing.T, admin *AlertAdministration, policy AlertPolicy, maker string) AlertPolicy {
	t.Helper()
	value, err := admin.CreatePolicy(context.Background(), policy, maker, "create governed alert policy")
	if err != nil {
		t.Fatal(err)
	}
	value, err = admin.SubmitPolicy(context.Background(), value.ID, value.Version, maker+"-submit", "submit governed alert policy")
	if err != nil {
		t.Fatal(err)
	}
	value, err = admin.DecidePolicy(context.Background(), value.ID, value.Version, true, maker+"-approve", "approve governed alert policy")
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAlertPolicyFutureReplacementPreservesCurrentPolicy(t *testing.T) {
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	store := NewMemoryAlertStore()
	admin := &AlertAdministration{Store: store, Clock: func() time.Time { return now }}
	first := activateAlertPolicy(t, admin, AlertPolicy{Name: "queue depth", Metric: AlertQueueDepth, Comparison: ComparisonGT, Threshold: 100, Severity: SeverityWarning, ConsecutiveEvaluations: 1, CooldownSeconds: 60, EffectiveFrom: now.Add(-time.Hour)}, "first")
	future := now.Add(time.Hour)
	second := activateAlertPolicy(t, admin, AlertPolicy{Name: "queue depth replacement", Metric: AlertQueueDepth, Comparison: ComparisonGT, Threshold: 200, Severity: SeverityCritical, ConsecutiveEvaluations: 1, CooldownSeconds: 60, EffectiveFrom: future}, "second")

	currentPolicies, err := store.ListActiveAlertPolicies(context.Background(), now)
	if err != nil || len(currentPolicies) != 1 || currentPolicies[0].ID != first.ID {
		t.Fatalf("current policy changed before future boundary: %#v %v", currentPolicies, err)
	}
	futurePolicies, err := store.ListActiveAlertPolicies(context.Background(), future.Add(time.Second))
	if err != nil || len(futurePolicies) != 1 || futurePolicies[0].ID != second.ID {
		t.Fatalf("future policy did not take effect: %#v %v", futurePolicies, err)
	}
	storedFirst, _ := store.GetAlertPolicy(context.Background(), first.ID)
	if storedFirst.Status != AlertPolicyActive || storedFirst.EffectiveTo == nil || !storedFirst.EffectiveTo.Equal(future) {
		t.Fatalf("prior policy was retired early: %#v", storedFirst)
	}
	events, err := store.ListAlertPolicyEvents(context.Background(), first.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	foundSuperseded := false
	for _, event := range events {
		if event.EventType == "SUPERSEDED" && event.Evidence["supersededById"] == second.ID {
			foundSuperseded = true
		}
	}
	if !foundSuperseded {
		t.Fatalf("alert policy supersession evidence missing: %#v", events)
	}

	third, err := admin.CreatePolicy(context.Background(), AlertPolicy{Name: "backdated", Metric: AlertQueueDepth, Comparison: ComparisonGT, Threshold: 300, Severity: SeverityCritical, ConsecutiveEvaluations: 1, CooldownSeconds: 60, EffectiveFrom: now.Add(-2 * time.Hour)}, "third", "create backdated alert policy")
	if err != nil {
		t.Fatal(err)
	}
	third, _ = admin.SubmitPolicy(context.Background(), third.ID, third.Version, "third-submit", "submit backdated alert policy")
	if _, err = admin.DecidePolicy(context.Background(), third.ID, third.Version, true, "third-approve", "approve backdated alert policy"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected backdated overlap conflict, got %v", err)
	}
}

func TestAlertEvaluationEscalationAcknowledgementAndRecovery(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	store := NewMemoryAlertStore()
	admin := &AlertAdministration{Store: store, Clock: func() time.Time { return now }}
	activateAlertPolicy(t, admin, AlertPolicy{Name: "queue saturation", Metric: AlertQueueDepth, Comparison: ComparisonGTE, Threshold: 10, Severity: SeverityCritical, ConsecutiveEvaluations: 2, CooldownSeconds: 30, AutoIncident: true, EscalationSteps: []EscalationStep{{AfterSeconds: 10, Target: "operations"}, {AfterSeconds: 20, Target: "senior-operations"}}, EffectiveFrom: now.Add(-time.Minute)}, "policy")
	dashboard := &staticDashboard{value: Dashboard{QueueDepth: 10}}
	evaluator := &AlertEvaluator{Store: store, Dashboard: dashboard, WorkerID: "worker-a", Lease: time.Minute, Clock: func() time.Time { return now }}

	if changed, err := evaluator.Evaluate(context.Background()); err != nil || changed != 0 {
		t.Fatalf("first observation: %d %v", changed, err)
	}
	if changed, err := evaluator.Evaluate(context.Background()); err != nil || changed != 1 {
		t.Fatalf("trigger observation: %d %v", changed, err)
	}
	alerts, _ := store.ListAlerts(context.Background(), AlertActive, SeverityCritical, 10)
	if len(alerts) != 1 || alerts[0].LinkedIncidentID == "" {
		t.Fatalf("alert or incident missing: %#v", alerts)
	}

	now = now.Add(11 * time.Second)
	if count, err := evaluator.Escalate(context.Background(), 10); err != nil || count != 1 {
		t.Fatalf("escalation: %d %v", count, err)
	}
	alerts, _ = store.ListAlerts(context.Background(), AlertActive, SeverityCritical, 10)
	if alerts[0].EscalationIndex != 1 || alerts[0].NextEscalationAt == nil {
		t.Fatalf("bad escalation state: %#v", alerts[0])
	}
	notifications, _ := store.ListNotifications(context.Background(), "DELIVERED", 10)
	if len(notifications) != 1 || notifications[0].Target != "operations" || notifications[0].DeliveredAt == nil || notifications[0].AttemptCount != 1 {
		t.Fatalf("notification missing: %#v", notifications)
	}

	acknowledged, err := admin.Acknowledge(context.Background(), alerts[0].ID, alerts[0].Version, "operator", "investigation accepted")
	if err != nil || acknowledged.NextEscalationAt != nil {
		t.Fatalf("acknowledge: %#v %v", acknowledged, err)
	}
	now = now.Add(20 * time.Second)
	if count, err := evaluator.Escalate(context.Background(), 10); err != nil || count != 0 {
		t.Fatalf("acknowledged alert escalated: %d %v", count, err)
	}

	dashboard.value.QueueDepth = 0
	if _, err = evaluator.Evaluate(context.Background()); err != nil {
		t.Fatal(err)
	}
	resolved, _ := store.GetAlert(context.Background(), alerts[0].ID)
	if resolved.Status != AlertResolved || resolved.ResolvedAt == nil {
		t.Fatalf("alert did not resolve: %#v", resolved)
	}
	timeline, _ := store.ListIncidentEvents(context.Background(), resolved.LinkedIncidentID, 20)
	if len(timeline) < 2 || timeline[0].EventType != "CREATED" {
		t.Fatalf("incident timeline missing: %#v", timeline)
	}
}
