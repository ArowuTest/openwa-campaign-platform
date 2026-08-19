package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
)

func atomicStartFixture(
	t *testing.T,
) (*Coordinator, *MemoryRoutingPlanStore, RoutingPlan, *recordingLifecycleCommitter, *atomicCampaigns) {
	t.Helper()
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	end := now.Add(24 * time.Hour)
	campaigns := &atomicCampaigns{current: campaign.Campaign{
		ID: "campaign-1", Status: campaign.StatusScheduled, Version: 1,
		UpdatedAt: now.Add(-time.Minute), RequestedStartAt: &now,
		CompletionDeadlineAt: &end, Timezone: "UTC",
		MaximumUniqueRecipients: 1000, EligibleAudienceCount: 1000,
		Transport: campaign.TransportSelection{
			RequiredCapabilities: []string{"SEND_TEXT"},
		},
	}}
	store := NewMemoryRoutingPlanStore()
	providers, gateways := governedRoutingDependencies(t, now, end.Add(time.Hour))
	routing := &RoutingAdministration{
		Store: store, Campaigns: campaigns,
		ProviderCapabilities: providers, GatewayPools: gateways,
		Clock: func() time.Time { return now },
	}
	plan, err := routing.CreateApproved(context.Background(), validPlan(), "approver")
	if err != nil {
		t.Fatal(err)
	}
	committer := &recordingLifecycleCommitter{}
	coordinator := &Coordinator{
		Campaigns: campaigns,
		Store: &fakeStore{
			m: Metrics{Authorised: 1000}, rate: 100, daily: 5000,
		},
		Committer:    committer,
		RoutingPlans: routing,
		Clock:        func() time.Time { return now },
	}
	return coordinator, store, plan, committer, campaigns
}

func TestCoordinatorStartDefersReservationActivationToAtomicCommitter(t *testing.T) {
	coordinator, store, plan, committer, campaigns := atomicStartFixture(t)
	_, _, err := coordinator.Start(
		context.Background(), campaigns.current.ID,
		"operator", "scheduled start", campaigns.current.Version,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(committer.commits) != 1 {
		t.Fatalf("atomic commits=%d", len(committer.commits))
	}
	value := committer.commits[0]
	if value.ReservationOperation != ReservationActivate ||
		value.RoutingPlanID != plan.ID {
		t.Fatalf("unexpected atomic start commit: %+v", value)
	}
	reservations, err := store.Reservations(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, reservation := range reservations {
		if reservation.Status != "HELD" || reservation.FencingVersion != 1 {
			t.Fatalf("coordinator mutated reservation before atomic commit: %+v", reservation)
		}
	}
}

func TestCoordinatorStartCommitFailureLeavesPreparedStateUnchanged(t *testing.T) {
	coordinator, store, plan, committer, campaigns := atomicStartFixture(t)
	committer.err = errors.New("simulated atomic commit failure")
	_, _, err := coordinator.Start(
		context.Background(), campaigns.current.ID,
		"operator", "scheduled start", campaigns.current.Version,
	)
	if err == nil {
		t.Fatal("expected atomic commit failure")
	}
	if campaigns.current.Status != campaign.StatusScheduled ||
		campaigns.current.Version != 1 {
		t.Fatalf("atomic commit failure changed source campaign: %+v", campaigns.current)
	}
	reservations, err := store.Reservations(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, reservation := range reservations {
		if reservation.Status != "HELD" || reservation.FencingVersion != 1 {
			t.Fatalf("atomic commit failure changed reservation: %+v", reservation)
		}
	}
}
