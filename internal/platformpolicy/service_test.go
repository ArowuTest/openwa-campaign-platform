package platformpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestConfigurationMakerCheckerEffectiveReplacement(t *testing.T) {
	repo := NewMemoryStore()
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	admin := &ConfigurationAdministration{Store: repo, Clock: func() time.Time { return now }}
	first, err := admin.Create(context.Background(), Configuration{Key: "CAMPAIGN.SAFETY_MARGIN", ScopeType: ScopePlatform, Value: json.RawMessage(`{"percent":15}`), EffectiveFrom: now.Add(-time.Hour)}, "maker-a", "initial safe margin")
	if err != nil {
		t.Fatal(err)
	}
	first, err = admin.Submit(context.Background(), first.ID, first.Version, "submitter-a", "submit initial")
	if err != nil {
		t.Fatal(err)
	}
	first, err = admin.Decide(context.Background(), first.ID, first.Version, true, "approver-a", "approve initial")
	if err != nil {
		t.Fatal(err)
	}
	futureStart := now.Add(24 * time.Hour)
	second, err := admin.Create(context.Background(), Configuration{Key: first.Key, ScopeType: ScopePlatform, Value: json.RawMessage(`{"percent":20}`), EffectiveFrom: futureStart}, "maker-b", "future margin")
	if err != nil {
		t.Fatal(err)
	}
	second, _ = admin.Submit(context.Background(), second.ID, second.Version, "submitter-b", "submit future")
	second, err = admin.Decide(context.Background(), second.ID, second.Version, true, "approver-b", "approve future")
	if err != nil {
		t.Fatal(err)
	}
	if second.SupersedesID != first.ID {
		t.Fatalf("replacement does not reference prior configuration: %#v", second)
	}
	prior, err := repo.GetConfiguration(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if prior.Status != StatusSuperseded || prior.EffectiveTo == nil || !prior.EffectiveTo.Equal(futureStart) {
		t.Fatalf("prior configuration does not retain supersession boundary: %#v", prior)
	}
	events, err := admin.Events(context.Background(), first.ID, 20)
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
		t.Fatalf("supersession event missing: %#v", events)
	}
	resolved, err := admin.Resolve(context.Background(), first.Key, nil, now)
	if err != nil || resolved.ID != first.ID {
		t.Fatalf("current configuration changed before future boundary: %#v %v", resolved, err)
	}
	resolved, err = admin.Resolve(context.Background(), first.Key, nil, futureStart.Add(time.Second))
	if err != nil || resolved.ID != second.ID {
		t.Fatalf("future configuration did not activate: %#v %v", resolved, err)
	}
}

func TestConfigurationApprovalRequiresIndependentActor(t *testing.T) {
	admin := &ConfigurationAdministration{Store: NewMemoryStore()}
	v, err := admin.Create(context.Background(), Configuration{Key: "QUEUE.BATCH_SIZE", ScopeType: ScopePlatform, Value: json.RawMessage(`100`)}, "maker", "create")
	if err != nil {
		t.Fatal(err)
	}
	v, err = admin.Submit(context.Background(), v.ID, v.Version, "maker", "submit")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Decide(context.Background(), v.ID, v.Version, true, "maker", "approve"); err == nil {
		t.Fatal("expected maker-checker rejection")
	}
}

func TestMaintenanceGateBlocksByModeAndScope(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	admin := &MaintenanceAdministration{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	window, err := admin.Create(context.Background(), MaintenanceWindow{Name: "gateway drain", Mode: MaintenanceDraining, ScopeType: ScopeGatewayPool, ScopeID: "gp-1", StartsAt: now.Add(-time.Minute), AllowActiveDispatch: true}, "maker", "planned drain")
	if err != nil {
		t.Fatal(err)
	}
	window, _ = admin.Submit(context.Background(), window.ID, window.Version, "submitter", "submit")
	window, err = admin.Decide(context.Background(), window.ID, window.Version, true, "approver", "approve")
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.Check(context.Background(), OperationCampaignStart, OperationalScope{GatewayPoolID: "gp-1"}, now); !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected start block, got %v", err)
	}
	if err = admin.Check(context.Background(), OperationDispatchSubmit, OperationalScope{GatewayPoolID: "gp-1"}, now); err != nil {
		t.Fatalf("existing dispatch should be allowed during drain: %v", err)
	}
	if err = admin.Check(context.Background(), OperationCampaignStart, OperationalScope{GatewayPoolID: "gp-2"}, now); err != nil {
		t.Fatalf("unrelated pool should remain available: %v", err)
	}
}

func TestEmergencyMaintenanceBlocksAllActions(t *testing.T) {
	now := time.Now().UTC()
	admin := &MaintenanceAdministration{Store: NewMemoryStore(), Clock: func() time.Time { return now }}
	window, _ := admin.Create(context.Background(), MaintenanceWindow{Name: "emergency", Mode: MaintenanceEmergencyStop, ScopeType: ScopePlatform, StartsAt: now.Add(-time.Second)}, "maker", "incident")
	window, _ = admin.Submit(context.Background(), window.ID, window.Version, "submitter", "submit")
	window, _ = admin.Decide(context.Background(), window.ID, window.Version, true, "approver", "approve")
	for _, action := range []Operation{OperationAPIWrite, OperationCampaignStart, OperationCampaignResume, OperationDispatchSubmit, OperationSessionChange} {
		if err := admin.Check(context.Background(), action, OperationalScope{}, now); !errors.Is(err, ErrBlocked) {
			t.Fatalf("%s should be blocked, got %v", action, err)
		}
	}
}
