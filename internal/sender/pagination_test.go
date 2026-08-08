package sender

import (
	"context"
	"testing"
	"time"
)

func TestSenderSessionPaginationHasStableContinuation(t *testing.T) {
	store := NewMemoryGovernanceStore()
	store.sessions["session-c"] = GovernedSession{ID: "session-c", MaskedMSISDN: "+234 *** 03"}
	store.sessions["session-a"] = GovernedSession{ID: "session-a", MaskedMSISDN: "+234 *** 01"}
	store.sessions["session-b"] = GovernedSession{ID: "session-b", MaskedMSISDN: "+234 *** 02"}
	service := &GovernanceService{Store: store}

	first, err := service.ListSessionsPage(context.Background(), 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "session-a" || first.Items[1].ID != "session-b" || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	second, err := service.ListSessionsPage(context.Background(), 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "session-c" || second.NextCursor != "" {
		t.Fatalf("unexpected second page: %+v", second)
	}
	if _, err := service.ListSessionsPage(context.Background(), 2, "not-a-cursor"); err == nil {
		t.Fatal("invalid sender-session cursor was accepted")
	}
}

func TestGatewayRuntimeEventPaginationHasStableContinuation(t *testing.T) {
	store := NewMemoryGovernanceStore()
	store.nodes["node-1"] = Node{ID: "node-1"}
	base := time.Date(2026, 8, 7, 18, 0, 0, 0, time.UTC)
	store.runtimeEvents["node-1"] = []RuntimeEvent{
		{ID: "event-a", NodeID: "node-1", OccurredAt: base},
		{ID: "event-b", NodeID: "node-1", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", NodeID: "node-1", OccurredAt: base.Add(2 * time.Minute)},
	}
	service := &RuntimeRegistrationService{Store: store}

	first, err := service.EventsPage(context.Background(), "node-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" || first.NextCursor == "" {
		t.Fatalf("unexpected first runtime page: %+v", first)
	}
	second, err := service.EventsPage(context.Background(), "node-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "event-a" || second.NextCursor != "" {
		t.Fatalf("unexpected second runtime page: %+v", second)
	}
	if _, err := service.EventsPage(context.Background(), "node-1", 2, "not-a-cursor"); err == nil {
		t.Fatal("invalid runtime-event cursor was accepted")
	}
}

func TestGatewayPoolEventPaginationHasStableContinuation(t *testing.T) {
	store := NewMemoryGovernanceStore()
	store.gatewayPools["pool-1"] = GatewayPool{ID: "pool-1"}
	base := time.Date(2026, 8, 7, 19, 0, 0, 0, time.UTC)
	store.gatewayPoolEvents["pool-1"] = []GatewayPoolEvent{
		{ID: "event-a", PoolID: "pool-1", OccurredAt: base},
		{ID: "event-b", PoolID: "pool-1", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", PoolID: "pool-1", OccurredAt: base.Add(2 * time.Minute)},
	}
	admin := &GatewayPoolAdministration{Store: store}

	first, err := admin.EventsPage(context.Background(), "pool-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" || first.NextCursor == "" {
		t.Fatalf("unexpected first gateway-pool event page: %+v", first)
	}
	second, err := admin.EventsPage(context.Background(), "pool-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "event-a" || second.NextCursor != "" {
		t.Fatalf("unexpected second gateway-pool event page: %+v", second)
	}
	if _, err := admin.EventsPage(context.Background(), "pool-1", 2, "not-a-cursor"); err == nil {
		t.Fatal("invalid gateway-pool event cursor was accepted")
	}
}
