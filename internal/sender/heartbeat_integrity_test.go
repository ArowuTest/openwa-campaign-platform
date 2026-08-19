package sender

import (
	"context"
	"testing"
	"time"
)

func TestHeartbeatSessionCannotRewriteGovernedCapacityOrReduceUsage(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	svc := &GovernanceService{Store: store}
	pool, err := svc.CreatePool(ctx, Pool{Name: "heartbeat integrity", MaxMessagesPerMinute: 100, DailyCapacity: 1000}, "actor", "approved pool")
	if err != nil {
		t.Fatal(err)
	}
	node, err := svc.RegisterNode(ctx, Node{Name: "worker-heartbeat", Status: "READY", Capacity: 1}, "actor", "approved node")
	if err != nil {
		t.Fatal(err)
	}
	session := registerReadySession(t, ctx, svc, store, GovernedSession{
		NodeID: node.ID, PoolID: pool.ID, MaskedMSISDN: "+234 *** 1000", EngineType: "openwa",
		SafeMessagesPerMinute: 25, SafeDailyCapacity: 500, InFlightLimit: 2,
	})
	now := time.Date(2026, 8, 9, 17, 30, 0, 0, time.UTC)
	first, err := store.HeartbeatSession(ctx, session.ID, session.Version, GovernedSession{
		Status: StatusReady, EngineVersion: "engine-1",
		SafeMessagesPerMinute: 999, SafeDailyCapacity: 9999, InFlightLimit: 99, SentToday: 120,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.SafeMessagesPerMinute != 25 || first.SafeDailyCapacity != 500 || first.InFlightLimit != 2 {
		t.Fatalf("heartbeat rewrote governed capacity: %+v", first)
	}
	if first.SentToday != 120 {
		t.Fatalf("heartbeat did not advance observed usage: %d", first.SentToday)
	}
	second, err := store.HeartbeatSession(ctx, session.ID, first.Version, GovernedSession{
		Status: StatusPairing, EngineVersion: "engine-2",
		SafeMessagesPerMinute: 1, SafeDailyCapacity: 1, InFlightLimit: 1, SentToday: 10,
	}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != StatusReady || second.SafeMessagesPerMinute != 25 || second.SafeDailyCapacity != 500 || second.InFlightLimit != 2 {
		t.Fatalf("later heartbeat rewrote governed capacity: %+v", second)
	}
	if second.SentToday != 120 {
		t.Fatalf("heartbeat reduced observed usage from 120 to %d", second.SentToday)
	}
	third, err := store.HeartbeatSession(ctx, session.ID, second.Version, GovernedSession{
		Status: StatusReady, EngineVersion: "engine-3", SentToday: 5,
	}, now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if third.SentToday != 5 {
		t.Fatalf("new UTC day did not reset observed usage: %d", third.SentToday)
	}
}
