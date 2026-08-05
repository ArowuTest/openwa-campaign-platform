package execution

import (
	"campaign-platform/internal/campaign"
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCampaigns struct{ v campaign.Campaign }

func (f *fakeCampaigns) Get(context.Context, string) (campaign.Campaign, error) { return f.v, nil }
func (f *fakeCampaigns) Transition(_ context.Context, _ string, in campaign.TransitionInput) (campaign.Campaign, error) {
	n, err := f.v.Transition(in, time.Now())
	if err == nil {
		f.v = n
	}
	return n, err
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
func (f *fakeStore) RecordEvent(context.Context, string, string, string, string, map[string]any, time.Time) error {
	f.events++
	return nil
}
func TestCoordinatorStartIsCapacityGated(t *testing.T) {
	now := time.Now().UTC()
	c := campaign.Campaign{ID: "c", Status: campaign.StatusScheduled, RequestedStartAt: &now, CompletionDeadlineAt: ptr(now.Add(time.Hour)), EligibleAudienceCount: 100, Version: 1, Transport: campaign.TransportSelection{RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "p", CapacityEvidenceVersion: "v"}}
	fs := &fakeStore{m: Metrics{Authorised: 100}, rate: 10, daily: 100}
	fc := &fakeCampaigns{v: c}
	co := &Coordinator{Campaigns: fc, Store: fs, SafetyMarginPercent: 10, Clock: func() time.Time { return now }}
	got, ev, err := co.Start(context.Background(), "c", "actor", "scheduled start", 1)
	if err != nil || got.Status != campaign.StatusDispatching || ev.Decision != DecisionAdmit || fs.events != 1 {
		t.Fatalf("%+v %+v %v", got, ev, err)
	}
}
func TestCoordinatorRejectsInsufficientCapacity(t *testing.T) {
	now := time.Now().UTC()
	c := campaign.Campaign{ID: "c", Status: campaign.StatusScheduled, RequestedStartAt: &now, CompletionDeadlineAt: ptr(now.Add(time.Minute)), EligibleAudienceCount: 100, Version: 1, Transport: campaign.TransportSelection{RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "p", CapacityEvidenceVersion: "v"}}
	co := &Coordinator{Campaigns: &fakeCampaigns{v: c}, Store: &fakeStore{m: Metrics{Authorised: 100}, rate: 1, daily: 100}, Clock: func() time.Time { return now }}
	_, ev, err := co.Start(context.Background(), "c", "actor", "start", 1)
	if err == nil || ev.Decision != DecisionReject {
		t.Fatalf("%+v %v", ev, err)
	}
}
func ptr(v time.Time) *time.Time { return &v }

var _ = errors.New
