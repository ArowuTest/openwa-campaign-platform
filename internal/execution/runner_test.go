package execution

import (
	"campaign-platform/internal/campaign"
	"context"
	"testing"
	"time"
)

type leaseRepo struct {
	due, active []CampaignLease
	released    int
}

func (l *leaseRepo) ClaimDue(context.Context, string, time.Duration, time.Time, int) ([]CampaignLease, error) {
	x := l.due
	l.due = nil
	return x, nil
}
func (l *leaseRepo) ClaimActive(context.Context, string, time.Duration, time.Time, int) ([]CampaignLease, error) {
	x := l.active
	l.active = nil
	return x, nil
}
func (l *leaseRepo) Release(context.Context, string, string, int64, time.Time) error {
	l.released++
	return nil
}

type applyingLifecycleCommitter struct {
	campaigns *fakeCampaigns
	last      LifecycleCommit
}

func (c *applyingLifecycleCommitter) Commit(
	_ context.Context, value LifecycleCommit,
) (campaign.Campaign, error) {
	c.campaigns.v = value.Campaign
	c.last = value
	return value.Campaign, nil
}

func TestRunnerProcessesDueAndCompletion(t *testing.T) {
	now := time.Now().UTC()
	scheduled := campaign.Campaign{ID: "c", Status: campaign.StatusScheduled, RequestedStartAt: &now, CompletionDeadlineAt: ptr(now.Add(time.Hour)), EligibleAudienceCount: 1, Version: 1, Transport: campaign.TransportSelection{RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "p", CapacityEvidenceVersion: "v"}}
	fc := &fakeCampaigns{v: scheduled}
	fs := &fakeStore{m: Metrics{Authorised: 1}, rate: 10, daily: 10}
	lr := &leaseRepo{due: []CampaignLease{{
		Campaign: scheduled, Owner: "w", FenceToken: 7, ExpiresAt: now.Add(time.Minute),
	}}}
	committer := &applyingLifecycleCommitter{campaigns: fc}
	r := &Runner{
		Repository: lr,
		Coordinator: &Coordinator{
			Campaigns: fc, Store: fs,
			Committer: committer,
			Clock:     func() time.Time { return now },
		},
		Owner: "w",
	}
	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fc.v.Status != campaign.StatusDispatching || lr.released != 1 {
		t.Fatalf("%s %d", fc.v.Status, lr.released)
	}
	if committer.last.ExecutionLease == nil ||
		committer.last.ExecutionLease.Owner != "w" ||
		committer.last.ExecutionLease.FenceToken != 7 {
		t.Fatalf("runner did not fence lifecycle commit: %+v", committer.last.ExecutionLease)
	}
}
