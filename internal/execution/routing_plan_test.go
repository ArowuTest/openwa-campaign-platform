package execution

import (
	"testing"
	"time"
)

func validPlan() RoutingPlan {
	return RoutingPlan{CampaignID: "campaign-1", RoutingPolicyVersion: "rp1", CapacityEvidenceVersion: "cap1", PacingPolicyVersion: "pace1", FallbackMode: "NONE", ApprovedBy: "approver", Routes: []PoolRoute{{SenderPoolID: "pool-a", GatewayPoolID: "gw-a", Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AllocationWeight: 2, MaximumRecipients: 700, ReservedMessagesPerMinute: 60, ReservedHourlyUnits: 500, ReservedDailyUnits: 900, AllowReallocationIn: true, AllowReallocationOut: true}, {SenderPoolID: "pool-b", GatewayPoolID: "gw-b", Provider: "OPENWA", Engine: "BAILEYS", AllocationWeight: 1, MaximumRecipients: 300, ReservedMessagesPerMinute: 40, ReservedHourlyUnits: 250, ReservedDailyUnits: 500}}}
}
func TestRoutingPlanSupportsMultipleOpenWAEnginePools(t *testing.T) {
	p := validPlan()
	if err := p.Validate(1000); err != nil {
		t.Fatal(err)
	}
	a, _ := p.AssignShard(0)
	b, _ := p.AssignShard(2)
	if a.SenderPoolID != "pool-a" || b.SenderPoolID != "pool-b" {
		t.Fatalf("unexpected assignments %s %s", a.SenderPoolID, b.SenderPoolID)
	}
}
func TestRoutingPlanRejectsUnfundedAudience(t *testing.T) {
	p := validPlan()
	p.Routes[1].MaximumRecipients = 10
	if p.Validate(1000) == nil {
		t.Fatal("expected insufficient route allocation rejection")
	}
}
func TestMultiPoolAdmissionIncludesPacingCapacityAndSafetyMargin(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	p := validPlan()
	out, err := EvaluateMultiPoolAdmission(MultiPoolAdmissionInput{CampaignID: "campaign-1", RemainingRecipients: 1000, EffectiveStart: now, Deadline: now.Add(20 * time.Minute), SafetyMarginPercent: 20, Plan: p, Now: now, Capacities: []PoolCapacity{{SenderPoolID: "pool-a", AvailableMessagesPerMinute: 60, AvailableHourlyUnits: 500, AvailableDailyUnits: 900, HealthySessions: 3}, {SenderPoolID: "pool-b", AvailableMessagesPerMinute: 40, AvailableHourlyUnits: 250, AvailableDailyUnits: 500, HealthySessions: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != DecisionAdmit {
		t.Fatalf("expected admit, got %s %#v", out.Decision, out.Reasons)
	}
	if out.EffectiveMessagesPerMinute != 80 {
		t.Fatalf("expected 80 mpm got %d", out.EffectiveMessagesPerMinute)
	}
}
func TestMultiPoolAdmissionHoldsDeadlineShortfall(t *testing.T) {
	now := time.Now().UTC()
	p := validPlan()
	out, err := EvaluateMultiPoolAdmission(MultiPoolAdmissionInput{CampaignID: "campaign-1", RemainingRecipients: 1000, EffectiveStart: now, Deadline: now.Add(5 * time.Minute), SafetyMarginPercent: 20, Plan: p, Now: now, Capacities: []PoolCapacity{{SenderPoolID: "pool-a", AvailableMessagesPerMinute: 60, AvailableHourlyUnits: 500, AvailableDailyUnits: 900, HealthySessions: 3}, {SenderPoolID: "pool-b", AvailableMessagesPerMinute: 40, AvailableHourlyUnits: 250, AvailableDailyUnits: 500, HealthySessions: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != DecisionHold {
		t.Fatalf("expected hold got %s", out.Decision)
	}
}
