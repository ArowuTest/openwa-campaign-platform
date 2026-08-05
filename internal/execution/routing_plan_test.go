package execution

import (
	"testing"
	"time"
)

func validPlan() RoutingPlan {
	return RoutingPlan{CampaignID: "campaign-1", RoutingPolicyVersion: "rp1", CapacityEvidenceVersion: "cap1", PacingPolicyVersion: "pace1", FallbackMode: "NONE", ApprovedBy: "approver", IdempotencyKey: "routing-plan-campaign-1", Routes: []PoolRoute{{SenderPoolID: "pool-a", GatewayPoolID: "gw-a", Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AllocationWeight: 2, MaximumRecipients: 700, ReservedMessagesPerMinute: 60, ReservedHourlyUnits: 500, ReservedDailyUnits: 900, AllowReallocationIn: true, AllowReallocationOut: true}, {SenderPoolID: "pool-b", GatewayPoolID: "gw-b", Provider: "OPENWA", Engine: "BAILEYS", AllocationWeight: 1, MaximumRecipients: 300, ReservedMessagesPerMinute: 40, ReservedHourlyUnits: 250, ReservedDailyUnits: 500}}}
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
	p.Routes[0].ReservedHourlyUnits, p.Routes[0].ReservedDailyUnits = 1000, 10000
	p.Routes[1].ReservedHourlyUnits, p.Routes[1].ReservedDailyUnits = 500, 5000
	out, err := EvaluateMultiPoolAdmission(MultiPoolAdmissionInput{CampaignID: "campaign-1", RemainingRecipients: 1000, EffectiveStart: now, Deadline: now.Add(20 * time.Minute), SafetyMarginPercent: 20, Plan: p, Now: now, Capacities: []PoolCapacity{{SenderPoolID: "pool-a", GatewayPoolID: "gw-a", AvailableMessagesPerMinute: 60, AvailableHourlyUnits: 1000, AvailableDailyUnits: 10000, HealthySessions: 3, HealthyNodes: 2, MinimumHealthyNodes: 1}, {SenderPoolID: "pool-b", GatewayPoolID: "gw-b", AvailableMessagesPerMinute: 40, AvailableHourlyUnits: 500, AvailableDailyUnits: 5000, HealthySessions: 2, HealthyNodes: 1, MinimumHealthyNodes: 1}}})
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
	out, err := EvaluateMultiPoolAdmission(MultiPoolAdmissionInput{CampaignID: "campaign-1", RemainingRecipients: 1000, EffectiveStart: now, Deadline: now.Add(5 * time.Minute), SafetyMarginPercent: 20, Plan: p, Now: now, Capacities: []PoolCapacity{{SenderPoolID: "pool-a", GatewayPoolID: "gw-a", AvailableMessagesPerMinute: 60, AvailableHourlyUnits: 500, AvailableDailyUnits: 900, HealthySessions: 3, HealthyNodes: 2, MinimumHealthyNodes: 1}, {SenderPoolID: "pool-b", GatewayPoolID: "gw-b", AvailableMessagesPerMinute: 40, AvailableHourlyUnits: 250, AvailableDailyUnits: 500, HealthySessions: 2, HealthyNodes: 1, MinimumHealthyNodes: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != DecisionHold {
		t.Fatalf("expected hold got %s", out.Decision)
	}
}

func TestMultiPoolAdmissionEnforcesHourlyAllowance(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	p := validPlan()
	out, err := EvaluateMultiPoolAdmission(MultiPoolAdmissionInput{CampaignID: "campaign-1", RemainingRecipients: 1000, EffectiveStart: now, Deadline: now.Add(20 * time.Minute), SafetyMarginPercent: 20, Plan: p, Now: now, Capacities: []PoolCapacity{{SenderPoolID: "pool-a", GatewayPoolID: "gw-a", AvailableMessagesPerMinute: 60, AvailableHourlyUnits: 500, AvailableDailyUnits: 900, HealthySessions: 3, HealthyNodes: 2, MinimumHealthyNodes: 1}, {SenderPoolID: "pool-b", GatewayPoolID: "gw-b", AvailableMessagesPerMinute: 40, AvailableHourlyUnits: 250, AvailableDailyUnits: 500, HealthySessions: 2, HealthyNodes: 1, MinimumHealthyNodes: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != DecisionHold {
		t.Fatalf("expected hourly allowance hold, got %s %#v", out.Decision, out.Reasons)
	}
	found := false
	for _, reason := range out.Reasons {
		if reason == "HOURLY_ALLOWANCE_SHORTFALL" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected hourly allowance evidence, got %#v", out.Reasons)
	}
}

func TestMultiPoolAdmissionAllowsMultiDayUseOfDailyAllowance(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	p := validPlan()
	for i := range p.Routes {
		p.Routes[i].MaximumRecipients = 1200
	}
	out, err := EvaluateMultiPoolAdmission(MultiPoolAdmissionInput{CampaignID: "campaign-1", RemainingRecipients: 2000, EffectiveStart: now, Deadline: now.Add(48 * time.Hour), SafetyMarginPercent: 20, Plan: p, Now: now, Capacities: []PoolCapacity{{SenderPoolID: "pool-a", GatewayPoolID: "gw-a", AvailableMessagesPerMinute: 60, AvailableHourlyUnits: 500, AvailableDailyUnits: 900, HealthySessions: 3, HealthyNodes: 2, MinimumHealthyNodes: 1}, {SenderPoolID: "pool-b", GatewayPoolID: "gw-b", AvailableMessagesPerMinute: 40, AvailableHourlyUnits: 250, AvailableDailyUnits: 500, HealthySessions: 2, HealthyNodes: 1, MinimumHealthyNodes: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != DecisionAdmit {
		t.Fatalf("expected multi-day admission, got %s %#v", out.Decision, out.Reasons)
	}
	if out.ForecastCompletionAt == nil || out.ForecastCompletionAt.After(now.Add(48*time.Hour)) {
		t.Fatalf("unexpected forecast: %v", out.ForecastCompletionAt)
	}
}

func TestCapacityReservationRejectsHourlyOverbooking(t *testing.T) {
	route := PoolRoute{ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 300, ReservedDailyUnits: 1000}
	if !reservationWouldOverbook(10, 5000, 0, 301, 0, route) {
		t.Fatal("expected overlapping hourly reservations to be rejected")
	}
	if reservationWouldOverbook(10, 5000, 0, 300, 0, route) {
		t.Fatal("expected reservation at the hourly boundary to be accepted")
	}
}
