package execution

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestShardLeaseFencingAndCompletion(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryShardRepository()
	repo.Shards["s1"] = DispatchShard{ID: "s1", CampaignID: "c1", Ordinal: 0, TargetSize: 10000, RecipientCount: 10, TerminalCount: 10, Status: ShardPending, CreatedAt: now, UpdatedAt: now}
	items, err := repo.Claim(context.Background(), "worker-a", now, time.Minute, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("claim: %v %#v", err, items)
	}
	stale := items[0]
	stale.LeaseVersion--
	if _, err := repo.Refresh(context.Background(), stale, now.Add(time.Second), time.Second); !errors.Is(err, ErrShardLeaseConflict) {
		t.Fatalf("expected stale fence rejection, got %v", err)
	}
	completed, err := repo.Refresh(context.Background(), items[0], now.Add(2*time.Second), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != ShardCompleted || completed.CompletedAt == nil {
		t.Fatalf("unexpected completion: %#v", completed)
	}
}

func TestShardCompletesWithExceptions(t *testing.T) {
	now := time.Now().UTC()
	repo := NewMemoryShardRepository()
	repo.Shards["s1"] = DispatchShard{ID: "s1", CampaignID: "c1", RecipientCount: 4, TerminalCount: 4, UnknownCount: 1, Status: ShardPending}
	items, _ := repo.Claim(context.Background(), "worker", now, time.Minute, 1)
	out, err := repo.Refresh(context.Background(), items[0], now.Add(time.Second), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != ShardExceptions {
		t.Fatalf("want exceptions, got %s", out.Status)
	}
}

func TestShardRunnerReleasesIncompleteShardForLaterRefresh(t *testing.T) {
	now := time.Now().UTC()
	repo := NewMemoryShardRepository()
	repo.Shards["s1"] = DispatchShard{ID: "s1", CampaignID: "c1", RecipientCount: 100, TerminalCount: 20, Status: ShardPending, CreatedAt: now, UpdatedAt: now}
	runner := &ShardRunner{Repository: repo, Owner: "worker", TargetSize: 10000, Lease: time.Minute, ClaimBatch: 2}
	if err := runner.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := repo.Shards["s1"].Status; got != ShardPending {
		t.Fatalf("expected pending refresh, got %s", got)
	}
	if repo.Shards["s1"].LeaseOwner != "" {
		t.Fatal("lease was not released")
	}
}
