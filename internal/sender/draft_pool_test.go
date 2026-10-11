package sender

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
)

func TestDraftPoolCanonicalUniqueIDs(t *testing.T) {
	store := NewMemoryGovernanceStore()
	seen := map[string]bool{}
	uuid := regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")
	for n := 0; n < 60; n++ {
		pool, err := store.CreatePool(context.Background(), Pool{Name: fmt.Sprintf("pool-%d", n), Status: "ACTIVE", DailyCapacity: 10, ReservedCapacity: 2}, "actor", "pool creation")
		if err != nil {
			t.Fatal(err)
		}
		if !uuid.MatchString(pool.ID) || seen[pool.ID] {
			t.Fatalf("noncanonical or duplicate pool ID %q", pool.ID)
		}
		seen[pool.ID] = true
		if pool.Version != 1 || pool.Status != "ACTIVE" || pool.DailyCapacity != 10 || pool.ReservedCapacity != 2 {
			t.Fatalf("supplied pool fields changed: %+v", pool)
		}
	}
}

func TestDraftPoolDirectLookup(t *testing.T) {
	store := NewMemoryGovernanceStore()
	pool, err := store.CreatePool(context.Background(), Pool{Name: "lookup", Status: "PAUSED"}, "actor", "pool creation")
	if err != nil {
		t.Fatal(err)
	}
	reader, ok := any(store).(interface {
		GetPool(context.Context, string) (Pool, error)
	})
	if !ok {
		t.Fatal("direct pool lookup is unavailable")
	}
	got, err := reader.GetPool(context.Background(), pool.ID)
	if err != nil || got.ID != pool.ID || got.Status != "PAUSED" || got.Version != pool.Version {
		t.Fatalf("lookup=%+v err=%v", got, err)
	}
	if _, err := reader.GetPool(context.Background(), "missing"); !errors.Is(err, ErrSenderNotFound) {
		t.Fatalf("missing lookup=%v", err)
	}
}
