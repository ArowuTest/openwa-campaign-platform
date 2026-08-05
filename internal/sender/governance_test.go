package sender

import (
	"context"
	"testing"
	"time"
)

func TestGovernedSenderCapacityAndConflicts(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	svc := &GovernanceService{Store: store}
	p, err := svc.CreatePool(ctx, Pool{Name: "Nigeria primary", MaxMessagesPerMinute: 120, DailyCapacity: 10000, ReservedCapacity: 1000}, "actor", "approved pool")
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.RegisterNode(ctx, Node{Name: "worker-a", Status: "READY", Capacity: 5}, "actor", "approved node")
	if err != nil {
		t.Fatal(err)
	}
	s, err := svc.RegisterSession(ctx, GovernedSession{NodeID: n.ID, PoolID: p.ID, MaskedMSISDN: "+234 801 *** 1234", EngineType: "openwa", Status: StatusReady, SafeMessagesPerMinute: 50, SafeDailyCapacity: 5000, InFlightLimit: 2}, []byte("cipher"), "actor", "approved sender")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s, err = store.HeartbeatSession(ctx, s.ID, s.Version, GovernedSession{Status: StatusReady, EngineVersion: "test", SafeMessagesPerMinute: 50, SafeDailyCapacity: 5000, InFlightLimit: 2, SentToday: 500}, now)
	if err != nil {
		t.Fatal(err)
	}
	n, err = store.HeartbeatNode(ctx, n.ID, n.Version, Node{Status: "READY", BuildVersion: "test", Capacity: 5}, now)
	if err != nil {
		t.Fatal(err)
	}
	cap, err := store.Capacity(ctx, p.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if cap.ReadySessions != 1 || cap.AvailableMessagesPerMinute != 50 || cap.AvailableDailyCapacity != 3500 {
		t.Fatalf("unexpected capacity %#v", cap)
	}
	if _, err := svc.TransitionSession(ctx, s.ID, s.Version-1, StatusPaused, "actor", "pause"); err != ErrSenderConflict {
		t.Fatalf("expected conflict got %v", err)
	}
}
