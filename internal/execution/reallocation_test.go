package execution

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestShardReallocationRequiresPendingUnsubmittedShard(t *testing.T) {
	repo := NewMemoryShardRepository()
	repo.Shards["s1"] = DispatchShard{ID: "s1", CampaignID: "c1", RoutingPlanID: "rp1", AssignedSenderPoolID: "p1", Status: ShardPending, LeaseVersion: 2, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	svc := &ReallocationAdministration{Store: repo, Clock: func() time.Time { return time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC) }}
	got, err := svc.Reallocate(context.Background(), "s1", "p2", "actor", "capacity incident", "INC-42")
	if err != nil {
		t.Fatal(err)
	}
	if got.FromSenderPoolID != "p1" || got.ToSenderPoolID != "p2" || got.NewLeaseVersion != 3 {
		t.Fatalf("unexpected event: %+v", got)
	}
	if repo.Shards["s1"].AssignedSenderPoolID != "p2" {
		t.Fatal("shard route not updated")
	}
	history, err := svc.List(context.Background(), "s1")
	if err != nil || len(history) != 1 {
		t.Fatalf("history=%v err=%v", history, err)
	}
}

func TestShardReallocationRejectsRunningOrTerminalWork(t *testing.T) {
	repo := NewMemoryShardRepository()
	repo.Shards["running"] = DispatchShard{ID: "running", CampaignID: "c", RoutingPlanID: "rp", AssignedSenderPoolID: "p1", Status: ShardRunning, LeaseOwner: "w", LeaseVersion: 1}
	repo.Shards["terminal"] = DispatchShard{ID: "terminal", CampaignID: "c", RoutingPlanID: "rp", AssignedSenderPoolID: "p1", Status: ShardPending, TerminalCount: 1, LeaseVersion: 1}
	svc := &ReallocationAdministration{Store: repo}
	for _, id := range []string{"running", "terminal"} {
		_, err := svc.Reallocate(context.Background(), id, "p2", "actor", "capacity incident", "INC-42")
		if !errors.Is(err, ErrReallocationUnsafe) {
			t.Fatalf("%s: expected unsafe, got %v", id, err)
		}
	}
}

func TestShardReallocationRequiresEvidenceAndReason(t *testing.T) {
	svc := &ReallocationAdministration{Store: NewMemoryShardRepository()}
	if _, err := svc.Reallocate(context.Background(), "s", "p", "actor", "short", ""); !errors.Is(err, ErrReallocationInvalid) {
		t.Fatalf("expected invalid, got %v", err)
	}
}
