package execution

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestShardReallocationPageContinuesOldestFirst(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryShardRepository()
	repo.Reallocations["shard-a"] = []ShardReallocation{
		{ID: "reallocation-a", DispatchShardID: "shard-a", CreatedAt: base},
		{ID: "reallocation-b", DispatchShardID: "shard-a", CreatedAt: base},
		{ID: "reallocation-c", DispatchShardID: "shard-a", CreatedAt: base.Add(time.Minute)},
	}
	service := &ReallocationAdministration{Store: repo}
	first, err := service.ListPage(context.Background(), "shard-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "reallocation-a" || first.Items[1].ID != "reallocation-b" {
		t.Fatalf("unexpected first shard-reallocation page: %#v", first)
	}
	second, err := service.ListPage(context.Background(), "shard-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "reallocation-c" {
		t.Fatalf("unexpected second shard-reallocation page: %#v", second)
	}
	if _, err := service.ListPage(context.Background(), "shard-a", 2, "invalid"); !errors.Is(err, ErrInvalidReallocationCursor) {
		t.Fatalf("invalid shard-reallocation cursor accepted: %v", err)
	}
}
