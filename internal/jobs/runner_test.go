package jobs

import (
	"context"
	"testing"
	"time"
)

func claimedJobForRunner(t *testing.T, repository *MemoryRepository, now time.Time) Job {
	t.Helper()
	job, err := NewJob(EnqueueInput{Type: "TEST", DedupKey: "runner-test", Payload: map[string]string{"value": "one"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.Enqueue(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	claimed, err := repository.Claim(context.Background(), "worker-1", now, time.Second, 1, []string{"TEST"})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: count=%d err=%v", len(claimed), err)
	}
	return claimed[0]
}

func TestRunnerConvertsHandlerPanicToDurableFailure(t *testing.T) {
	now := time.Now().UTC()
	repository := NewMemoryRepository()
	job := claimedJobForRunner(t, repository, now)
	runner := Runner{
		Repository: repository, Owner: "worker-1", Lease: time.Second,
		OperationTimeout: 100 * time.Millisecond, ShutdownGrace: 100 * time.Millisecond,
		Handler: func(context.Context, Job) error { panic("unexpected handler defect") },
	}
	if err := runner.runJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusDeadLetter || stored.LastErrorCode != "PERMANENT_FAILURE" {
		t.Fatalf("panic was not durably quarantined: %+v", stored)
	}
}

func TestRunnerShutdownDoesNotWaitForeverForNonCooperativeHandler(t *testing.T) {
	now := time.Now().UTC()
	repository := NewMemoryRepository()
	job := claimedJobForRunner(t, repository, now)
	release := make(chan struct{})
	runner := Runner{
		Repository: repository, Owner: "worker-1", Lease: time.Second,
		OperationTimeout: 20 * time.Millisecond, ShutdownGrace: 20 * time.Millisecond,
		Handler: func(context.Context, Job) error {
			<-release // deliberately ignores context cancellation
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := runner.runJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("shutdown was not bounded: %v", elapsed)
	}
	close(release)
}
