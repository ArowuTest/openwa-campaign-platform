package execution

import (
	"testing"

	"campaign-platform/internal/campaign"
)

func TestStartRoutingPlanActivationUsesExactAssessedAuthority(t *testing.T) {
	tests := []struct {
		name      string
		provider  campaign.Provider
		pilot     PilotAdmissionEvidence
		evidence  CapacityEvidence
		wantPlan  string
		wantOp    ReservationOperation
		wantError bool
	}{
		{
			name:     "OpenWA uses pilot-bound plan",
			provider: campaign.ProviderOpenWA,
			pilot:    PilotAdmissionEvidence{RoutingPlanID: "openwa-plan"},
			evidence: CapacityEvidence{PoolID: "different-capacity-reference", routingPlanID: "different-assessed-plan"},
			wantPlan: "openwa-plan",
			wantOp:   ReservationActivate,
		},
		{
			name:     "Meta uses the exact plan used by capacity assessment",
			provider: campaign.ProviderMeta,
			evidence: CapacityEvidence{PoolID: "meta-capacity-reference", routingPlanID: "meta-assessed-plan"},
			wantPlan: "meta-assessed-plan",
			wantOp:   ReservationActivate,
		},
		{
			name:     "Legacy governed route uses exact assessed plan",
			evidence: CapacityEvidence{PoolID: "legacy-capacity-reference", routingPlanID: "legacy-assessed-plan"},
			wantPlan: "legacy-assessed-plan",
			wantOp:   ReservationActivate,
		},
		{
			name:     "Legacy single-pool route preserves reservation-free start",
			evidence: CapacityEvidence{PoolID: "legacy-sender-pool"},
			wantPlan: "",
			wantOp:   ReservationNone,
		},
		{
			name:      "Meta fails closed without assessed routing plan identity",
			provider:  campaign.ProviderMeta,
			evidence:  CapacityEvidence{},
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entity := campaign.Campaign{Transport: campaign.TransportSelection{Provider: tc.provider}}
			planID, op, err := startRoutingPlanActivation(entity, tc.pilot, tc.evidence)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected fail-closed routing-plan identity error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if planID != tc.wantPlan || op != tc.wantOp {
				t.Fatalf("plan=%q op=%q want plan=%q op=%q", planID, op, tc.wantPlan, tc.wantOp)
			}
		})
	}
}
