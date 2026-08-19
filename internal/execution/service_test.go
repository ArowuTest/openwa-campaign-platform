package execution

import (
	"campaign-platform/internal/campaign"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeCampaigns struct{ v campaign.Campaign }

func (f *fakeCampaigns) Get(context.Context, string) (campaign.Campaign, error) { return f.v, nil }
func (f *fakeCampaigns) PrepareTransition(_ context.Context, _ string, in campaign.TransitionInput) (campaign.Campaign, error) {
	return f.v.Transition(in, time.Now())
}

type fakeStore struct {
	m          Metrics
	rate       int
	daily      int64
	events     int
	admissions int
}

func (f *fakeStore) RecordAdmission(context.Context, CapacityEvidence) error {
	f.admissions++
	return nil
}
func (f *fakeStore) Metrics(context.Context, string) (Metrics, error) { return f.m, nil }
func (f *fakeStore) Capacity(context.Context, string, time.Time) (int, int64, error) {
	return f.rate, f.daily, nil
}
func (f *fakeStore) RouteCapacity(_ context.Context, route PoolRoute, _ time.Time) (PoolCapacity, error) {
	return PoolCapacity{SenderPoolID: route.SenderPoolID, GatewayPoolID: route.GatewayPoolID, AvailableMessagesPerMinute: f.rate, AvailableHourlyUnits: int64(f.rate) * 60, AvailableDailyUnits: f.daily, HealthySessions: 1, HealthyNodes: 1, MinimumHealthyNodes: 1}, nil
}
func (f *fakeStore) RecordEvent(context.Context, string, string, string, string, map[string]any, time.Time) error {
	f.events++
	return nil
}
func TestCoordinatorStartIsCapacityGated(t *testing.T) {
	now := time.Now().UTC()
	c := campaign.Campaign{ID: "c", Status: campaign.StatusScheduled, RequestedStartAt: &now, CompletionDeadlineAt: ptr(now.Add(time.Hour)), EligibleAudienceCount: 100, Version: 1, Transport: campaign.TransportSelection{RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "p", CapacityEvidenceVersion: "v"}}
	fs := &fakeStore{m: Metrics{Authorised: 100}, rate: 10, daily: 100}
	fc := &fakeCampaigns{v: c}
	co := &Coordinator{Campaigns: fc, Store: fs, Committer: &recordingLifecycleCommitter{}, SafetyMarginPercent: 10, Clock: func() time.Time { return now }}
	got, ev, err := co.Start(context.Background(), "c", "actor", "scheduled start", 1)
	if err != nil || got.Status != campaign.StatusDispatching || ev.Decision != DecisionAdmit || fs.events != 0 {
		t.Fatalf("%+v %+v %v", got, ev, err)
	}
}
func TestCoordinatorMetaExecutionRequiresApprovedRoutingPlan(t *testing.T) {
	now := time.Now().UTC()
	entity := campaign.Campaign{
		ID: "meta-campaign", Status: campaign.StatusScheduled,
		RequestedStartAt: &now, CompletionDeadlineAt: ptr(now.Add(time.Hour)), EligibleAudienceCount: 10, Version: 1,
		Transport: campaign.TransportSelection{
			Provider: campaign.ProviderMeta, Engine: campaign.EngineMetaCloud, RoutingMode: campaign.RoutingSenderPool,
			SenderPoolID: "meta-pool", MetaSenderID: "meta-sender", CapacityEvidenceVersion: "cap-v1",
		},
	}
	store := &fakeStore{m: Metrics{Authorised: 10}, rate: 10, daily: 100}
	coordinator := &Coordinator{Campaigns: &fakeCampaigns{v: entity}, Store: store, Committer: &recordingLifecycleCommitter{}, Clock: func() time.Time { return now }}
	_, _, err := coordinator.Start(context.Background(), entity.ID, "actor", "scheduled Meta start", entity.Version)
	if err == nil || !strings.Contains(err.Error(), "routing plan") {
		t.Fatalf("Meta execution without frozen routing plan was not rejected: %v", err)
	}
	if store.admissions != 0 {
		t.Fatalf("unfrozen Meta route recorded admission evidence: %d", store.admissions)
	}
}

func TestCoordinatorRejectsInsufficientCapacity(t *testing.T) {
	now := time.Now().UTC()
	c := campaign.Campaign{ID: "c", Status: campaign.StatusScheduled, RequestedStartAt: &now, CompletionDeadlineAt: ptr(now.Add(time.Minute)), EligibleAudienceCount: 100, Version: 1, Transport: campaign.TransportSelection{RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "p", CapacityEvidenceVersion: "v"}}
	co := &Coordinator{Campaigns: &fakeCampaigns{v: c}, Store: &fakeStore{m: Metrics{Authorised: 100}, rate: 1, daily: 100}, Committer: &recordingLifecycleCommitter{}, Clock: func() time.Time { return now }}
	_, ev, err := co.Start(context.Background(), "c", "actor", "start", 1)
	if err == nil || ev.Decision != DecisionReject {
		t.Fatalf("%+v %v", ev, err)
	}
}
func ptr(v time.Time) *time.Time { return &v }

var _ = errors.New

func TestCoordinatorStartRespectsDispatchWindow(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	start := now.Add(time.Hour)
	c := campaign.Campaign{ID: "c", Status: campaign.StatusScheduled, RequestedStartAt: &start, CompletionDeadlineAt: ptr(now.Add(3 * time.Hour)), Timezone: "UTC", EligibleAudienceCount: 10, Version: 1, Transport: campaign.TransportSelection{RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "p", CapacityEvidenceVersion: "v"}}
	co := &Coordinator{Campaigns: &fakeCampaigns{v: c}, Store: &fakeStore{m: Metrics{Authorised: 10}, rate: 10, daily: 10}, Committer: &recordingLifecycleCommitter{}, Clock: func() time.Time { return now }}
	_, _, err := co.Start(context.Background(), "c", "actor", "early start", 1)
	if err == nil {
		t.Fatal("campaign started before requested window")
	}
}

func TestCoordinatorResumeRespectsQuietHours(t *testing.T) {
	now := time.Date(2026, 8, 5, 22, 30, 0, 0, time.UTC)
	start := now.Add(-time.Hour)
	c := campaign.Campaign{ID: "c", Status: campaign.StatusPaused, RequestedStartAt: &start, CompletionDeadlineAt: ptr(now.Add(3 * time.Hour)), Timezone: "UTC", QuietHoursStart: "22:00", QuietHoursEnd: "07:00", EligibleAudienceCount: 10, Version: 1, Transport: campaign.TransportSelection{RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "p", CapacityEvidenceVersion: "v"}}
	co := &Coordinator{Campaigns: &fakeCampaigns{v: c}, Store: &fakeStore{m: Metrics{Authorised: 10}, rate: 10, daily: 10}, Committer: &recordingLifecycleCommitter{}, Clock: func() time.Time { return now }}
	_, _, err := co.Resume(context.Background(), "c", "actor", "resume", 1)
	if err == nil {
		t.Fatal("campaign resumed during quiet hours")
	}
}
