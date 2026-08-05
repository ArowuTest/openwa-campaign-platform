package jobs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEnqueueIsIdempotentAndClaimIsExclusive(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	first, created, err := service.Enqueue(context.Background(), EnqueueInput{Type: "SEND", DedupKey: "campaign:1", Payload: map[string]string{"x": "y"}})
	if err != nil || !created {
		t.Fatalf("enqueue: %v %v", created, err)
	}
	second, created, err := service.Enqueue(context.Background(), EnqueueInput{Type: "SEND", DedupKey: "campaign:1", Payload: map[string]string{"x": "y"}})
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("duplicate enqueue: %+v %v", second, err)
	}
	_, _, err = service.Enqueue(context.Background(), EnqueueInput{Type: "SEND", DedupKey: "campaign:1", Payload: map[string]string{"x": "different"}})
	if !errors.Is(err, ErrDedupMismatch) {
		t.Fatalf("expected payload mismatch, got %v", err)
	}
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	now := time.Now().UTC()
	for _, owner := range []string{"a", "b"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			jobs, _ := repository.Claim(context.Background(), owner, now, time.Minute, 1, nil)
			counts <- len(jobs)
		}(owner)
	}
	wg.Wait()
	close(counts)
	total := 0
	for count := range counts {
		total += count
	}
	if total != 1 {
		t.Fatalf("expected one exclusive claim, got %d", total)
	}
}

func TestExpiredLeaseCanBeReclaimed(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	job, _, _ := service.Enqueue(context.Background(), EnqueueInput{Type: "SEND", DedupKey: "1", Payload: nil})
	now := time.Now().UTC()
	claimed, _ := repository.Claim(context.Background(), "a", now, time.Second, 1, nil)
	if len(claimed) != 1 {
		t.Fatal("not claimed")
	}
	reclaimed, _ := repository.Claim(context.Background(), "b", now.Add(2*time.Second), time.Minute, 1, nil)
	if len(reclaimed) != 1 || reclaimed[0].ID != job.ID {
		t.Fatal("expired lease was not reclaimed")
	}
}

func TestRunnerHonoursConcurrencyBound(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	for i := 0; i < 20; i++ {
		_, _, _ = service.Enqueue(context.Background(), EnqueueInput{Type: "TEST", DedupKey: string(rune('a' + i)), Payload: nil})
	}
	var active atomic.Int64
	var peak atomic.Int64
	handler := func(ctx context.Context, _ Job) error {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if current <= old || peak.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runner := Runner{Repository: repository, Owner: "worker", Types: []string{"TEST"}, Concurrency: 3, ClaimBatch: 3, PollInterval: time.Millisecond, Handler: handler}
	go runner.Run(ctx)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		count, _ := repository.PendingCount(context.Background(), []string{"TEST"}, time.Now())
		if count == 0 && runner.Active() == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if peak.Load() > 3 {
		t.Fatalf("concurrency exceeded: %d", peak.Load())
	}
}

func TestPermanentFailureMovesDirectlyToDeadLetter(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	job, _, err := service.Enqueue(context.Background(), EnqueueInput{Type: "TEST", DedupKey: "permanent", Payload: nil, MaxAttempts: 10})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claimed, err := repository.Claim(context.Background(), "worker", now, time.Minute, 1, nil)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v %d", err, len(claimed))
	}
	if err := repository.Fail(context.Background(), job.ID, "worker", claimed[0].LeaseVersion, now, false, 0, "BAD_INPUT", "invalid"); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusDeadLetter {
		t.Fatalf("status=%s", stored.Status)
	}
}

func TestRunnerRenewsLeaseForLongJob(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	job, _, err := service.Enqueue(context.Background(), EnqueueInput{Type: "LONG", DedupKey: "long", Payload: nil})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runner := Runner{Repository: repository, Owner: "worker", Types: []string{"LONG"}, Concurrency: 1, ClaimBatch: 1, Lease: 150 * time.Millisecond, PollInterval: time.Millisecond, Handler: func(context.Context, Job) error { time.Sleep(420 * time.Millisecond); return nil }}
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stored, _ := repository.Get(context.Background(), job.ID)
		if stored.Status == StatusCompleted {
			cancel()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stored, _ := repository.Get(context.Background(), job.ID)
	if stored.Status != StatusCompleted {
		t.Fatalf("status=%s attempts=%d", stored.Status, stored.AttemptCount)
	}
	<-done
}

func TestStaleLeaseCannotCompleteAfterSameOwnerReclaims(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	job, _, err := service.Enqueue(context.Background(), EnqueueInput{Type: "SEND", DedupKey: "fenced", Payload: nil})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	first, err := repository.Claim(context.Background(), "worker", now, time.Second, 1, nil)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: %v len=%d", err, len(first))
	}
	second, err := repository.Claim(context.Background(), "worker", now.Add(2*time.Second), time.Minute, 1, nil)
	if err != nil || len(second) != 1 {
		t.Fatalf("second claim: %v len=%d", err, len(second))
	}
	if second[0].LeaseVersion <= first[0].LeaseVersion {
		t.Fatalf("lease version did not advance: first=%d second=%d", first[0].LeaseVersion, second[0].LeaseVersion)
	}
	if err := repository.Complete(context.Background(), job.ID, "worker", first[0].LeaseVersion, now.Add(3*time.Second)); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("stale lease completed job: %v", err)
	}
	if err := repository.Complete(context.Background(), job.ID, "worker", second[0].LeaseVersion, now.Add(3*time.Second)); err != nil {
		t.Fatalf("current lease could not complete job: %v", err)
	}
}

func TestProcessingJobCannotBeCancelledBehindWorkerLease(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	job, _, err := service.Enqueue(context.Background(), EnqueueInput{Type: "SEND", DedupKey: "cancel-processing", Payload: nil})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claimed, err := repository.Claim(context.Background(), "worker", now, time.Minute, 1, []string{"SEND"})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v count=%d", err, len(claimed))
	}
	if err := repository.Cancel(context.Background(), job.ID, now.Add(time.Second)); !errors.Is(err, ErrJobInProgress) {
		t.Fatalf("processing job was cancelled unsafely: %v", err)
	}
	stored, err := repository.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusProcessing || stored.LeaseVersion != claimed[0].LeaseVersion {
		t.Fatalf("lease evidence was altered: %+v", stored)
	}
}
