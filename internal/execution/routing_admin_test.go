package execution

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
)

type routingCampaignReader struct{ value campaign.Campaign }

func (r routingCampaignReader) Get(context.Context, string) (campaign.Campaign, error) {
	return r.value, nil
}

func TestRoutingAdministrationCreatesReservationsForEveryPool(t *testing.T) {
	start := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	store := NewMemoryRoutingPlanStore()
	svc := &RoutingAdministration{Store: store, Campaigns: routingCampaignReader{value: campaign.Campaign{ID: "campaign-1", MaximumUniqueRecipients: 1000, RequestedStartAt: &start, CompletionDeadlineAt: &end}}, Clock: func() time.Time { return start.Add(-time.Hour) }}
	plan := validPlan()
	plan.ApprovedBy = ""
	plan.ApprovedAt = time.Time{}
	created, err := svc.CreateApproved(context.Background(), plan, "approver-1")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.ApprovedBy != "approver-1" {
		t.Fatalf("unexpected created plan: %+v", created)
	}
	reservations, err := svc.Reservations(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reservations) != 2 {
		t.Fatalf("reservations=%d", len(reservations))
	}
	for _, r := range reservations {
		if r.Status != "HELD" || !r.ReservationStart.Equal(start) || !r.ReservationEnd.Equal(end) {
			t.Fatalf("unexpected reservation: %+v", r)
		}
	}
	if err := svc.Release(context.Background(), created.ID, "operator-1"); err != nil {
		t.Fatal(err)
	}
	reservations, _ = svc.Reservations(context.Background(), created.ID)
	for _, r := range reservations {
		if r.Status != "RELEASED" {
			t.Fatalf("status=%s", r.Status)
		}
	}
}
