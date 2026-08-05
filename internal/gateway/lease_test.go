package gateway

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSessionLeaseHasExactlyOneOwner(t *testing.T) {
	store := NewMemoryLeaseStore()
	now := time.Now().UTC()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, worker := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(worker string) {
			defer wg.Done()
			_, err := store.Acquire(context.Background(), "session-1", worker, worker+"-token", now, time.Minute)
			results <- err
		}(worker)
	}
	wg.Wait()
	close(results)
	success, held := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if err == ErrLeaseHeld {
			held++
		}
	}
	if success != 1 || held != 1 {
		t.Fatalf("expected one owner and one rejection; success=%d held=%d", success, held)
	}
}
func TestExpiredSessionLeaseCanMove(t *testing.T) {
	store := NewMemoryLeaseStore()
	now := time.Now().UTC()
	_, _ = store.Acquire(context.Background(), "s", "a", "t1", now, time.Second)
	lease, err := store.Acquire(context.Background(), "s", "b", "t2", now.Add(2*time.Second), time.Minute)
	if err != nil || lease.WorkerID != "b" {
		t.Fatalf("reacquire: %+v %v", lease, err)
	}
}

func TestLeaseValidationUsesFencingVersion(t *testing.T) {
	store := NewMemoryLeaseStore()
	now := time.Now().UTC()
	first, err := store.Acquire(context.Background(), "session", "worker", "token", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Acquire(context.Background(), "session", "worker", "token", now.Add(2*time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Validate(context.Background(), first, now.Add(3*time.Second)); err != ErrLeaseLost {
		t.Fatalf("stale fenced lease validated: %v", err)
	}
	if err := store.Validate(context.Background(), second, now.Add(3*time.Second)); err != nil {
		t.Fatalf("current lease rejected: %v", err)
	}
}
