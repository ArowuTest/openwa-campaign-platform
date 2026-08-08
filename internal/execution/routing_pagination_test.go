package execution

import (
	"context"
	"errors"
	"testing"
)

func TestRoutingPlanPageContinuesNewestFirst(t *testing.T) {
	store := NewMemoryRoutingPlanStore()
	store.plans["plan-1"] = RoutingPlan{ID: "plan-1", CampaignID: "campaign-a", Version: 1}
	store.plans["plan-2"] = RoutingPlan{ID: "plan-2", CampaignID: "campaign-a", Version: 2}
	store.plans["plan-3"] = RoutingPlan{ID: "plan-3", CampaignID: "campaign-a", Version: 3}
	store.byCampaign["campaign-a"] = []string{"plan-1", "plan-2", "plan-3"}
	admin := &RoutingAdministration{Store: store}
	first, err := admin.ListByCampaignPage(context.Background(), "campaign-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].Version != 3 || first.Items[1].Version != 2 {
		t.Fatalf("unexpected first routing-plan page: %#v", first)
	}
	second, err := admin.ListByCampaignPage(context.Background(), "campaign-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].Version != 1 {
		t.Fatalf("unexpected second routing-plan page: %#v", second)
	}
	if _, err := admin.ListByCampaignPage(context.Background(), "campaign-a", 2, "invalid"); !errors.Is(err, ErrInvalidRoutingPlanCursor) {
		t.Fatalf("invalid routing-plan cursor accepted: %v", err)
	}
}
