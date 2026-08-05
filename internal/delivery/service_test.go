package delivery

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRepositoryAppliesConcurrentDuplicateEventOnce(t *testing.T) {
	recipient := Recipient{ID: "r1", Status: StatusAuthorised, UpdatedAt: time.Now().UTC()}
	repo := NewMemoryRepository(recipient)
	event := Event{DeduplicationKey: "provider:1", Type: EventSubmitting, OccurredAt: time.Now().UTC()}
	var wg sync.WaitGroup
	changed := make(chan bool, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, c, e := repo.ApplyEvent(context.Background(), "r1", event)
			changed <- c
			errs <- e
		}()
	}
	wg.Wait()
	close(changed)
	close(errs)
	count := 0
	for c := range changed {
		if c {
			count++
		}
	}
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if count != 1 {
		t.Fatalf("expected one applied event, got %d", count)
	}
	final, _ := repo.Get(context.Background(), "r1")
	if final.AttemptCount != 1 {
		t.Fatalf("attempts=%d", final.AttemptCount)
	}
}

func TestRepositoryRejectsDedupKeyWithDifferentPayload(t *testing.T) {
	now := time.Now().UTC()
	repo := NewMemoryRepository(Recipient{ID: "r1", Status: StatusAuthorised, UpdatedAt: now})
	first := Event{DeduplicationKey: "provider:event:1", Type: EventSubmitting, OccurredAt: now}
	if _, _, err := repo.ApplyEvent(context.Background(), "r1", first); err != nil {
		t.Fatal(err)
	}
	second := Event{DeduplicationKey: "provider:event:1", Type: EventFailedPermanent, ErrorCode: "DIFFERENT", OccurredAt: now}
	if _, _, err := repo.ApplyEvent(context.Background(), "r1", second); err != ErrEventDedupMismatch {
		t.Fatalf("expected dedup mismatch, got %v", err)
	}
}
