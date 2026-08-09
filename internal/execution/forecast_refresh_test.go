package execution

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
)

func TestCoordinatorForecastUpdatesWhenMeasuredThroughputChanges(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(2 * time.Hour)
	entity := campaign.Campaign{
		ID: "forecast-refresh", Status: campaign.StatusDispatching,
		CompletionDeadlineAt: &deadline, Version: 1,
		Transport: campaign.TransportSelection{
			RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "pool-forecast", CapacityEvidenceVersion: "capacity-v1",
		},
	}
	store := &fakeStore{m: Metrics{Authorised: 600}, rate: 20, daily: 10_000}
	coordinator := &Coordinator{Campaigns: &fakeCampaigns{v: entity}, Store: store, Clock: func() time.Time { return now }}
	first, err := coordinator.Forecast(context.Background(), entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	store.rate = 10
	second, err := coordinator.Forecast(context.Background(), entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProjectedCompletionAt == nil || second.ProjectedCompletionAt == nil {
		t.Fatalf("missing forecasts: first=%+v second=%+v", first, second)
	}
	if !second.ProjectedCompletionAt.After(*first.ProjectedCompletionAt) {
		t.Fatalf("forecast did not move later after throughput fell: first=%s second=%s", first.ProjectedCompletionAt, second.ProjectedCompletionAt)
	}
	if first.EffectiveMessagesPerMinute != 20 || second.EffectiveMessagesPerMinute != 10 {
		t.Fatalf("measured throughput not reflected: first=%v second=%v", first.EffectiveMessagesPerMinute, second.EffectiveMessagesPerMinute)
	}
}
