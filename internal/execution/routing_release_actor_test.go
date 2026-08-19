package execution

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
)

type releaseActorRecordingStore struct {
	*MemoryRoutingPlanStore
	actor string
}

func (s *releaseActorRecordingStore) ReleaseReservations(ctx context.Context, planID, actor string, at time.Time) error {
	s.actor = actor
	return s.MemoryRoutingPlanStore.ReleaseReservations(ctx, planID, actor, at)
}

func TestRoutingAdministrationReleaseCanonicalisesActor(t *testing.T) {
	base := NewMemoryRoutingPlanStore()
	store := &releaseActorRecordingStore{MemoryRoutingPlanStore: base}
	plan := RoutingPlan{ID: "plan-release-actor", CampaignID: "campaign-release-actor", IdempotencyKey: "release-actor", RequestHash: "hash"}
	if _, err := base.Create(context.Background(), plan, nil); err != nil {
		t.Fatal(err)
	}
	admin := &RoutingAdministration{Store: store, Campaigns: routingCampaignReader{value: campaign.Campaign{ID: plan.CampaignID, Status: campaign.StatusCancelled}}}
	if err := admin.Release(context.Background(), plan.ID, "  operator-1  "); err != nil {
		t.Fatal(err)
	}
	if store.actor != "operator-1" {
		t.Fatalf("release actor=%q want canonical operator-1", store.actor)
	}
}
