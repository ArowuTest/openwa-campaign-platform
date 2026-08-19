package execution

import (
	"testing"
	"time"

	"campaign-platform/internal/campaign"
)

func validLifecycleCommit() LifecycleCommit {
	return LifecycleCommit{
		Campaign: campaign.Campaign{
			ID: "campaign-lifecycle", Version: 2,
		},
		ExpectedVersion:      1,
		Action:               campaign.ActionPause,
		EventType:            "CAMPAIGN_PAUSED",
		ActorID:              "operator-lifecycle",
		ReservationOperation: ReservationNone,
		OccurredAt:           time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC),
	}
}

func TestLifecycleCommitValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*LifecycleCommit)
	}{
		{"campaign", func(v *LifecycleCommit) { v.Campaign.ID = "" }},
		{"expected version", func(v *LifecycleCommit) { v.ExpectedVersion = 0 }},
		{"candidate version", func(v *LifecycleCommit) { v.Campaign.Version = 3 }},
		{"action", func(v *LifecycleCommit) { v.Action = "" }},
		{"event type", func(v *LifecycleCommit) { v.EventType = "" }},
		{"actor", func(v *LifecycleCommit) { v.ActorID = "" }},
		{"occurred at", func(v *LifecycleCommit) { v.OccurredAt = time.Time{} }},
		{"operation", func(v *LifecycleCommit) {
			v.ReservationOperation = ReservationOperation("INVALID")
		}},
		{"plan required", func(v *LifecycleCommit) {
			v.ReservationOperation = ReservationActivate
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := validLifecycleCommit()
			test.mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatalf("expected invalid lifecycle commit: %+v", value)
			}
		})
	}
	value := validLifecycleCommit()
	if err := value.Validate(); err != nil {
		t.Fatalf("valid lifecycle commit rejected: %v", err)
	}
	value.ReservationOperation = ReservationRelease
	value.RoutingPlanID = "plan-lifecycle"
	if err := value.Validate(); err != nil {
		t.Fatalf("valid reservation lifecycle commit rejected: %v", err)
	}
}
