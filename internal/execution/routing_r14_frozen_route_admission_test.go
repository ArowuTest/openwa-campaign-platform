package execution

import (
	"testing"
	"time"
)

func TestMultiPoolAdmissionCannotAdmitWithUnavailableFrozenRoute(t *testing.T) {
	now := time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC)
	plan := validPlan()
	for i := range plan.Routes {
		plan.Routes[i].ReservedHourlyUnits = 3600
		plan.Routes[i].ReservedDailyUnits = 10000
	}
	out, err := EvaluateMultiPoolAdmission(MultiPoolAdmissionInput{
		CampaignID: "campaign-1", RemainingRecipients: 100, EffectiveStart: now,
		Deadline: now.Add(2 * time.Hour), SafetyMarginPercent: 0, Plan: plan, Now: now,
		Capacities: []PoolCapacity{{
			SenderPoolID: "pool-a", GatewayPoolID: "gw-a", AvailableMessagesPerMinute: 60,
			AvailableHourlyUnits: 3600, AvailableDailyUnits: 10000,
			HealthySessions: 2, HealthyNodes: 1, MinimumHealthyNodes: 1,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision == DecisionAdmit {
		t.Fatalf("admitted while frozen route pool-b was unavailable: %#v", out)
	}
}
