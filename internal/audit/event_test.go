package audit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRecorderBuildsVerifiableConcurrentChain(t *testing.T) {
	repository := NewMemoryRepository()
	recorder := NewRecorder(repository)
	var wg sync.WaitGroup
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := recorder.Record(context.Background(), Input{
				ActorType: "USER", ActorID: "user-1", Action: "TEST",
				ObjectType: "OBJECT", ObjectID: "object-1", CorrelationID: "request-1",
			})
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := repository.Verify(context.Background()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	items, _ := repository.List(context.Background(), 0, 100)
	if len(items) != 20 {
		t.Fatalf("expected 20 events, got %d", len(items))
	}
}

func TestAuditSummaryIsBounded(t *testing.T) {
	_, err := New(Input{ActorType: "USER", ActorID: "u", Action: "A", ObjectType: "O", ObjectID: "1",
		CorrelationID: "c", Before: make([]byte, 70<<10)})
	if err == nil {
		t.Fatal("expected oversized summary to fail")
	}
}

func TestAuditAppendReplayReturnsOriginalEvent(t *testing.T) {
	repository := NewMemoryRepository()
	event, err := New(Input{ActorType: "USER", ActorID: "u", Action: "CREATE", ObjectType: "CAMPAIGN", ObjectID: "c", CorrelationID: "request-1", OccurredAt: time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	first, err := repository.Append(context.Background(), event, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repository.Append(context.Background(), event, first.Sequence, first.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != first.ID || replayed.Sequence != first.Sequence || replayed.Hash != first.Hash {
		t.Fatalf("replay did not return original evidence: first=%+v replay=%+v", first, replayed)
	}
	items, _ := repository.List(context.Background(), 0, 10)
	if len(items) != 1 {
		t.Fatalf("replay duplicated audit event: %d", len(items))
	}
}

func TestAuditAppendRejectsSameIDWithDifferentIntent(t *testing.T) {
	repository := NewMemoryRepository()
	event, err := New(Input{ActorType: "USER", ActorID: "u", Action: "CREATE", ObjectType: "CAMPAIGN", ObjectID: "c", CorrelationID: "request-1", OccurredAt: time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	first, err := repository.Append(context.Background(), event, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	mutated := event
	mutated.Action = "DELETE"
	if _, err := repository.Append(context.Background(), mutated, first.Sequence, first.Hash); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}
