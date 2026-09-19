package jobs

import (
	"context"
	"errors"
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

func TestRunnerRetryPolicyAppliesBoundedJitterAndNeverRetriesOutcomeUnknown(t *testing.T) {
	now := time.Now().UTC()
	repository := NewMemoryRepository()
	job := claimedJobForRunner(t, repository, now)
	policy := RetryPolicy{
		BaseDelay: time.Second,
		MaxDelay:  8 * time.Second,
		Jitter: func(jobID string, attempt int, base time.Duration) time.Duration {
			if jobID != job.ID || attempt != 1 || base != time.Second {
				t.Fatalf("unexpected jitter input id=%s attempt=%d base=%v", jobID, attempt, base)
			}
			return 250 * time.Millisecond
		},
	}
	runner := Runner{
		Repository: repository, Owner: "worker-1", Lease: time.Second,
		OperationTimeout: 100 * time.Millisecond, ShutdownGrace: 100 * time.Millisecond,
		RetryPolicy: policy,
		Handler: func(context.Context, Job) error {
			return RetryableError{Code: "TRANSIENT_PROVIDER", Err: errors.New("temporary")}
		},
	}
	started := time.Now().UTC()
	if err := runner.runJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	finished := time.Now().UTC()
	stored, err := repository.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusPending {
		t.Fatalf("retryable status=%s", stored.Status)
	}
	wantDelay := time.Second + 250*time.Millisecond
	if stored.AvailableAt.Before(started.Add(wantDelay)) || stored.AvailableAt.After(finished.Add(wantDelay)) {
		t.Fatalf("retry schedule=%v want within [%v,%v]", stored.AvailableAt, started.Add(wantDelay), finished.Add(wantDelay))
	}

	unknownRepo := NewMemoryRepository()
	unknownJob := claimedJobForRunner(t, unknownRepo, now)
	unknownRunner := Runner{
		Repository: unknownRepo, Owner: "worker-1", Lease: time.Second,
		OperationTimeout: 100 * time.Millisecond, ShutdownGrace: 100 * time.Millisecond,
		RetryPolicy: policy,
		Handler: func(context.Context, Job) error {
			return RetryableError{Code: " outcome_unknown ", Err: errors.New("ambiguous submission")}
		},
	}
	if err := unknownRunner.runJob(context.Background(), unknownJob); err != nil {
		t.Fatal(err)
	}
	unknownStored, err := unknownRepo.Get(context.Background(), unknownJob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unknownStored.Status != StatusDeadLetter || unknownStored.LastErrorCode != "OUTCOME_UNKNOWN" {
		t.Fatalf("ambiguous outcome was retryable: %+v", unknownStored)
	}
}

func TestRunnerRetryAfterCannotBypassGovernedPositiveFloor(t *testing.T) {
	now := time.Now().UTC()
	repository := NewMemoryRepository()
	job := claimedJobForRunner(t, repository, now)
	runner := Runner{
		Repository: repository, Owner: "worker-1", Lease: time.Second,
		OperationTimeout: 100 * time.Millisecond, ShutdownGrace: 100 * time.Millisecond,
		RetryPolicy: RetryPolicy{BaseDelay: time.Second, MaxDelay: 8 * time.Second},
		Handler: func(context.Context, Job) error {
			return RetryableError{Code: "TRANSIENT_PROVIDER", RetryAfter: time.Nanosecond, Err: errors.New("retry later")}
		},
	}
	started := time.Now().UTC()
	if err := runner.runJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AvailableAt.Before(started.Add(100 * time.Millisecond)) {
		t.Fatalf("RetryAfter bypassed governed positive floor: availableAt=%v started=%v", stored.AvailableAt, started)
	}
}

func TestParseRetryCategoryPolicyRejectsUnknownOutcomeAndBuildsOverrides(t *testing.T) {
	policy, err := ParseRetryCategoryPolicy("RATE_LIMITED,10s,2m,25;TRANSIENT_PROVIDER,2s,30s,10")
	if err != nil {
		t.Fatal(err)
	}
	if len(policy) != 2 || policy["RATE_LIMITED"].BaseDelay != 10*time.Second || policy["TRANSIENT_PROVIDER"].MaxDelay != 30*time.Second {
		t.Fatalf("policy=%+v", policy)
	}
	if _, err := ParseRetryCategoryPolicy("OUTCOME_UNKNOWN,1s,2s,0"); err == nil {
		t.Fatal("OUTCOME_UNKNOWN category override must be rejected")
	}
	if _, err := ParseRetryCategoryPolicy(" outcome_unknown ,1s,2s,0"); err == nil {
		t.Fatal("OUTCOME_UNKNOWN category override must be rejected after canonicalization")
	}
}

func TestRunnerRetryDelayNeverCollapsesToZero(t *testing.T) {
	r := Runner{RetryPolicy: RetryPolicy{
		BaseDelay: time.Second,
		MaxDelay:  time.Second,
		Jitter: func(string, int, time.Duration) time.Duration {
			return -time.Second
		},
	}}
	if got := r.retryDelay(Job{ID: "job-zero-jitter", AttemptCount: 1}, "TRANSIENT_PROVIDER"); got <= 0 {
		t.Fatalf("retry delay=%v must remain positive under maximum negative jitter", got)
	}
}

func TestRunnerRetryPolicySupportsFailureCategoryOverrides(t *testing.T) {
	r := Runner{RetryPolicy: RetryPolicy{BaseDelay: time.Second, MaxDelay: 8 * time.Second, ByCode: map[string]RetryClassPolicy{"RATE_LIMITED": {BaseDelay: 10 * time.Second, MaxDelay: 30 * time.Second}}}}
	job := Job{ID: "job-rate", AttemptCount: 2}
	if got := r.retryDelay(job, "RATE_LIMITED"); got != 20*time.Second {
		t.Fatalf("category delay=%v want 20s", got)
	}
	if got := r.retryDelay(job, "TRANSIENT_PROVIDER"); got != 2*time.Second {
		t.Fatalf("default delay=%v want 2s", got)
	}
}

func TestRunnerRecognisesPointerRetryableError(t *testing.T) {
	now := time.Now().UTC()
	repository := NewMemoryRepository()
	job := claimedJobForRunner(t, repository, now)
	runner := Runner{
		Repository: repository, Owner: "worker-1", Lease: time.Second,
		OperationTimeout: 100 * time.Millisecond, ShutdownGrace: 100 * time.Millisecond,
		Handler: func(context.Context, Job) error {
			return &RetryableError{Code: "TRANSIENT_PROVIDER", Err: errors.New("temporary")}
		},
	}
	if err := runner.runJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusPending || stored.LastErrorCode != "TRANSIENT_PROVIDER" {
		t.Fatalf("pointer retryable error was not classified as retryable: %+v", stored)
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
