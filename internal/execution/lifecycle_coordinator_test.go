package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
)

type atomicCampaigns struct {
	current campaign.Campaign
}

func (f *atomicCampaigns) Get(context.Context, string) (campaign.Campaign, error) {
	return f.current, nil
}

func (f *atomicCampaigns) PrepareTransition(
	_ context.Context, _ string, input campaign.TransitionInput,
) (campaign.Campaign, error) {
	return f.current.Transition(input, f.current.UpdatedAt.Add(time.Minute))
}

type recordingLifecycleCommitter struct {
	commits []LifecycleCommit
	err     error
}

func (f *recordingLifecycleCommitter) Commit(
	_ context.Context, value LifecycleCommit,
) (campaign.Campaign, error) {
	f.commits = append(f.commits, value)
	if f.err != nil {
		return campaign.Campaign{}, f.err
	}
	return value.Campaign, nil
}

func TestCoordinatorStartUsesAtomicLifecycleCommitter(t *testing.T) {
	now := time.Date(2026, 8, 10, 14, 0, 0, 0, time.UTC)
	deadline := now.Add(time.Hour)
	campaigns := &atomicCampaigns{current: campaign.Campaign{
		ID: "campaign-atomic-start", Status: campaign.StatusScheduled,
		Version: 3, UpdatedAt: now, RequestedStartAt: &now,
		CompletionDeadlineAt: &deadline, EligibleAudienceCount: 10,
		Transport: campaign.TransportSelection{
			RoutingMode:             campaign.RoutingSenderPool,
			SenderPoolID:            "pool-atomic-start",
			CapacityEvidenceVersion: "capacity-v1",
		},
	}}
	committer := &recordingLifecycleCommitter{}
	coordinator := &Coordinator{
		Campaigns: campaigns,
		Store:     &fakeStore{m: Metrics{Authorised: 10}, rate: 10, daily: 100},
		Committer: committer,
		Clock:     func() time.Time { return now },
	}
	got, _, err := coordinator.Start(
		context.Background(), campaigns.current.ID, "operator-atomic",
		"scheduled start", campaigns.current.Version,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != campaign.StatusDispatching || len(committer.commits) != 1 {
		t.Fatalf("campaign=%+v commits=%+v", got, committer.commits)
	}
	commit := committer.commits[0]
	if commit.ReservationOperation != ReservationNone ||
		commit.EventType != "DISPATCH_STARTED" {
		t.Fatalf("unexpected lifecycle commit: %+v", commit)
	}
	if campaigns.current.Status != campaign.StatusScheduled {
		t.Fatalf("preparation mutated source campaign: %+v", campaigns.current)
	}
}

func TestCoordinatorFailsClosedWithoutLifecycleCommitter(t *testing.T) {
	now := time.Date(2026, 8, 10, 14, 30, 0, 0, time.UTC)
	deadline := now.Add(time.Hour)
	campaigns := &atomicCampaigns{current: campaign.Campaign{
		ID: "campaign-atomic-missing", Status: campaign.StatusScheduled,
		Version: 5, UpdatedAt: now, RequestedStartAt: &now,
		CompletionDeadlineAt: &deadline, EligibleAudienceCount: 10,
		Transport: campaign.TransportSelection{
			RoutingMode:             campaign.RoutingSenderPool,
			SenderPoolID:            "pool-atomic-missing",
			CapacityEvidenceVersion: "capacity-v1",
		},
	}}
	coordinator := &Coordinator{
		Campaigns: campaigns,
		Store:     &fakeStore{m: Metrics{Authorised: 10}, rate: 10, daily: 100},
		Clock:     func() time.Time { return now },
	}
	_, _, err := coordinator.Start(
		context.Background(), campaigns.current.ID, "operator-atomic",
		"scheduled start", campaigns.current.Version,
	)
	if err == nil || !errors.Is(err, ErrLifecycleCommitterRequired) {
		t.Fatalf("expected fail-closed committer error, got %v", err)
	}
	if campaigns.current.Status != campaign.StatusScheduled {
		t.Fatalf("missing committer changed campaign: %+v", campaigns.current)
	}
}

func TestCoordinatorPauseCancelAndCompleteUseAtomicCommitter(t *testing.T) {
	now := time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		entity    campaign.Campaign
		metrics   Metrics
		invoke    func(*Coordinator, campaign.Campaign) error
		eventType string
		status    campaign.Status
	}{
		{
			name: "pause",
			entity: campaign.Campaign{
				ID: "campaign-pause", Status: campaign.StatusDispatching,
				Version: 1, UpdatedAt: now,
			},
			invoke: func(c *Coordinator, v campaign.Campaign) error {
				_, err := c.Pause(context.Background(), v.ID, "operator", "incident", v.Version)
				return err
			},
			eventType: "CAMPAIGN_PAUSED", status: campaign.StatusPaused,
		},
		{
			name: "cancel",
			entity: campaign.Campaign{
				ID: "campaign-cancel", Status: campaign.StatusScheduled,
				Version: 2, UpdatedAt: now,
			},
			invoke: func(c *Coordinator, v campaign.Campaign) error {
				_, err := c.Cancel(context.Background(), v.ID, "operator", "withdrawn", v.Version)
				return err
			},
			eventType: "CAMPAIGN_CANCELLED", status: campaign.StatusCancelled,
		},
		{
			name: "complete",
			entity: campaign.Campaign{
				ID: "campaign-complete", Status: campaign.StatusDispatching,
				Version: 4, UpdatedAt: now,
			},
			metrics: Metrics{Sent: 10},
			invoke: func(c *Coordinator, v campaign.Campaign) error {
				_, _, err := c.AssessAndComplete(context.Background(), v.ID, "scheduler", v.Version)
				return err
			},
			eventType: string(CompletionReady), status: campaign.StatusCompleted,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			campaigns := &atomicCampaigns{current: test.entity}
			committer := &recordingLifecycleCommitter{}
			coordinator := &Coordinator{
				Campaigns: campaigns, Store: &fakeStore{m: test.metrics},
				Committer: committer, Clock: func() time.Time { return now },
			}
			if err := test.invoke(coordinator, test.entity); err != nil {
				t.Fatal(err)
			}
			if len(committer.commits) != 1 {
				t.Fatalf("commits=%d", len(committer.commits))
			}
			value := committer.commits[0]
			if value.EventType != test.eventType ||
				value.ReservationOperation != ReservationNone ||
				value.Campaign.Status != test.status {
				t.Fatalf("unexpected lifecycle commit: %+v", value)
			}
		})
	}
}

func TestCoordinatorResumeUsesAtomicCommitter(t *testing.T) {
	now := time.Date(2026, 8, 10, 16, 0, 0, 0, time.UTC)
	start := now.Add(-time.Hour)
	deadline := now.Add(time.Hour)
	campaigns := &atomicCampaigns{current: campaign.Campaign{
		ID: "campaign-resume", Status: campaign.StatusPaused,
		Version: 6, UpdatedAt: now, RequestedStartAt: &start,
		CompletionDeadlineAt: &deadline, Timezone: "UTC",
		EligibleAudienceCount: 10,
		Transport: campaign.TransportSelection{
			RoutingMode:  campaign.RoutingSenderPool,
			SenderPoolID: "pool-resume", CapacityEvidenceVersion: "capacity-v1",
		},
	}}
	committer := &recordingLifecycleCommitter{}
	coordinator := &Coordinator{
		Campaigns: campaigns,
		Store:     &fakeStore{m: Metrics{Authorised: 10}, rate: 10, daily: 100},
		Committer: committer, Clock: func() time.Time { return now },
	}
	entity, _, err := coordinator.Resume(
		context.Background(), campaigns.current.ID,
		"operator", "incident cleared", campaigns.current.Version,
	)
	if err != nil {
		t.Fatal(err)
	}
	if entity.Status != campaign.StatusDispatching || len(committer.commits) != 1 {
		t.Fatalf("campaign=%+v commits=%+v", entity, committer.commits)
	}
	value := committer.commits[0]
	if value.EventType != "CAMPAIGN_RESUMED" ||
		value.ReservationOperation != ReservationNone {
		t.Fatalf("unexpected lifecycle commit: %+v", value)
	}
}

func TestCoordinatorTerminalActionsRequestAtomicReservationRelease(t *testing.T) {
	now := time.Date(2026, 8, 10, 17, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		entity  campaign.Campaign
		metrics Metrics
		invoke  func(*Coordinator, campaign.Campaign) error
	}{
		{
			name: "cancel",
			entity: campaign.Campaign{
				ID: "campaign-release-cancel", Status: campaign.StatusScheduled,
				Version: 3, UpdatedAt: now,
			},
			invoke: func(c *Coordinator, v campaign.Campaign) error {
				_, err := c.Cancel(
					context.Background(), v.ID, "operator", "withdrawn", v.Version,
				)
				return err
			},
		},
		{
			name: "complete",
			entity: campaign.Campaign{
				ID: "campaign-release-complete", Status: campaign.StatusDispatching,
				Version: 8, UpdatedAt: now,
			},
			metrics: Metrics{Sent: 25},
			invoke: func(c *Coordinator, v campaign.Campaign) error {
				_, _, err := c.AssessAndComplete(
					context.Background(), v.ID, "scheduler", v.Version,
				)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			campaigns := &atomicCampaigns{current: test.entity}
			planStore := NewMemoryRoutingPlanStore()
			plan, err := planStore.Create(context.Background(), RoutingPlan{
				ID: "plan-" + test.name, CampaignID: test.entity.ID,
				IdempotencyKey: "request-" + test.name,
				RequestHash:    "hash-" + test.name,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			committer := &recordingLifecycleCommitter{}
			coordinator := &Coordinator{
				Campaigns: campaigns, Store: &fakeStore{m: test.metrics},
				Committer: committer,
				RoutingPlans: &RoutingAdministration{
					Store: planStore, Campaigns: campaigns,
				},
				Clock: func() time.Time { return now },
			}
			if err := test.invoke(coordinator, test.entity); err != nil {
				t.Fatal(err)
			}
			if len(committer.commits) != 1 {
				t.Fatalf("commits=%d", len(committer.commits))
			}
			value := committer.commits[0]
			if value.ReservationOperation != ReservationRelease ||
				value.RoutingPlanID != plan.ID {
				t.Fatalf("unexpected terminal lifecycle commit: %+v", value)
			}
		})
	}
}
