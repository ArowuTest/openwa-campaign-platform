package cohort

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/jobs"
)

func estimateTestDefinition() audiencefilter.Group {
	return audiencefilter.Group{
		Join:  audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}}},
	}
}

func estimateTestRequest() EstimateJobRequest {
	return EstimateJobRequest{
		OrganisationID:  "11111111-1111-4111-8111-111111111111",
		PurposeID:       "22222222-2222-4222-8222-222222222222",
		Channel:         "WHATSAPP",
		Definition:      estimateTestDefinition(),
		RequestedBy:     "33333333-3333-4333-8333-333333333333",
		ClientRequestID: "cohort-estimate-request-0001",
	}
}

func TestMemoryEstimateRepositoryScheduleReplayAndConflict(t *testing.T) {
	now := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	queue := jobs.NewMemoryRepository()
	repo := NewMemoryEstimateRepository(queue)
	request := estimateTestRequest()

	first, created, err := repo.Schedule(context.Background(), request, now)
	if err != nil || !created {
		t.Fatalf("schedule created=%v err=%v", created, err)
	}
	if first.ID == "" || first.Job.Status != jobs.StatusPending || first.AsOf != now {
		t.Fatalf("unexpected first estimate: %+v", first)
	}

	replay, created, err := repo.Schedule(context.Background(), request, now.Add(time.Minute))
	if err != nil || created {
		t.Fatalf("replay created=%v err=%v", created, err)
	}
	if replay.ID != first.ID || !replay.AsOf.Equal(first.AsOf) {
		t.Fatalf("replay changed identity/evidence time: first=%+v replay=%+v", first, replay)
	}

	conflict := request
	conflict.Definition = audiencefilter.Group{
		Join:  audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"GH"}}},
	}
	if _, _, err := repo.Schedule(context.Background(), conflict, now.Add(2*time.Minute)); !errors.Is(err, ErrEstimateReplayConflict) {
		t.Fatalf("expected replay conflict, got %v", err)
	}
}

func TestMemoryEstimateRepositoryStoresResultOnlyUnderLiveLease(t *testing.T) {
	now := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	queue := jobs.NewMemoryRepository()
	repo := NewMemoryEstimateRepository(queue)
	record, _, err := repo.Schedule(context.Background(), estimateTestRequest(), now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := queue.Claim(context.Background(), "worker-a", now.Add(time.Second), 2*time.Minute, 1, []string{EstimateJobType})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: items=%d err=%v", len(claimed), err)
	}
	lease := claimed[0]
	result := Estimate{
		EligibleCount: 42,
		Breakdown:     &EligibilityBreakdown{MatchedProfiles: 50, ConsentEligible: 48, ConsentExcluded: 2, Unsuppressed: 45, SuppressionExcluded: 3, Eligible: 42, FrequencyCapExcluded: 3},
		CalculatedAt:  now.Add(3 * time.Second),
	}
	evidence := EstimateGovernanceEvidence{
		ConsentReviewID:           "44444444-4444-4444-8444-444444444444",
		ConsentReviewVersion:      7,
		ConsentWordingVersion:     "v7",
		OrganisationPolicyID:      "55555555-5555-4555-8555-555555555555",
		OrganisationPolicyVersion: 9,
	}
	if err := repo.StoreResult(context.Background(), record.ID, lease.LeaseOwner, lease.LeaseVersion, result, evidence, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Result == nil || stored.Result.EligibleCount != 42 || stored.Evidence != evidence {
		t.Fatalf("stored=%+v", stored)
	}

	// The old lease must not be able to overwrite after takeover.
	if err := queue.Fail(context.Background(), lease.ID, lease.LeaseOwner, lease.LeaseVersion, now.Add(5*time.Second), true, 0, "TEST", "retry"); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := queue.Claim(context.Background(), "worker-b", now.Add(6*time.Second), 2*time.Minute, 1, []string{EstimateJobType})
	if err != nil || len(reclaimed) != 1 {
		t.Fatalf("reclaim: items=%d err=%v", len(reclaimed), err)
	}
	if err := repo.StoreResult(context.Background(), record.ID, lease.LeaseOwner, lease.LeaseVersion, result, evidence, now.Add(7*time.Second)); !errors.Is(err, jobs.ErrLeaseConflict) {
		t.Fatalf("stale lease stored result: %v", err)
	}
}

func TestMemoryEstimateRepositoryListsRecoverableJobsNewestFirst(t *testing.T) {
	base := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	queue := jobs.NewMemoryRepository()
	repo := NewMemoryEstimateRepository(queue)
	for index := 0; index < 3; index++ {
		request := estimateTestRequest()
		request.ClientRequestID = "cohort-estimate-page-" + string(rune('a'+index))
		request.RequestedBy = "33333333-3333-4333-8333-" + []string{"333333333331", "333333333332", "333333333333"}[index]
		if _, _, err := repo.Schedule(context.Background(), request, base.Add(time.Duration(index)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	first, err := repo.ListPage(context.Background(), estimateTestRequest().OrganisationID, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || !first.Items[0].CreatedAt.After(first.Items[1].CreatedAt) {
		t.Fatalf("first page=%+v", first)
	}
	second, err := repo.ListPage(context.Background(), estimateTestRequest().OrganisationID, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("second page=%+v", second)
	}
}

type estimateEvidenceErrorResolver struct{}

func (estimateEvidenceErrorResolver) ResolveEstimateEvidence(context.Context, EstimateJobRecord) (EstimateGovernanceEvidence, error) {
	return EstimateGovernanceEvidence{}, errors.New("governance evidence unavailable")
}

func TestEstimateWorkerDoesNotStrandLaterClaimedJobsAfterEarlierFailure(t *testing.T) {
	base := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	queue := jobs.NewMemoryRepository()
	repo := NewMemoryEstimateRepository(queue)

	firstRequest := estimateTestRequest()
	firstRequest.ClientRequestID = "cohort-estimate-batch-a"
	first, _, err := repo.Schedule(context.Background(), firstRequest, base)
	if err != nil {
		t.Fatal(err)
	}

	secondRequest := estimateTestRequest()
	secondRequest.ClientRequestID = "cohort-estimate-batch-b"
	secondRequest.RequestedBy = "33333333-3333-4333-8333-333333333334"
	second, _, err := repo.Schedule(context.Background(), secondRequest, base.Add(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	repo.mu.Lock()
	secondRecord := repo.items[second.ID]
	secondRecord.Result = &Estimate{
		EligibleCount: 1,
		Breakdown:     &EligibilityBreakdown{MatchedProfiles: 1, ConsentEligible: 1, Unsuppressed: 1, Eligible: 1},
		CalculatedAt:  base,
	}
	repo.items[second.ID] = secondRecord
	repo.mu.Unlock()

	worker := &EstimateWorker{
		Queue:         queue,
		Estimates:     repo,
		Cohorts:       &ExecutionService{},
		Evidence:      estimateEvidenceErrorResolver{},
		WorkerID:      "worker-batch",
		ClaimBatch:    2,
		LeaseDuration: time.Minute,
		Clock:         func() time.Time { return base.Add(time.Second) },
	}

	processed, err := worker.Process(context.Background())
	if err == nil {
		t.Fatal("expected the first job failure to be reported")
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1 successful later job", processed)
	}

	firstJob, err := queue.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstJob.Status != jobs.StatusPending {
		t.Fatalf("failed job status=%s, want PENDING", firstJob.Status)
	}
	secondJob, err := queue.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.Status != jobs.StatusCompleted {
		t.Fatalf("later claimed job status=%s, want COMPLETED", secondJob.Status)
	}
}

type estimateEvidenceResolverFunc func(context.Context, EstimateJobRecord) (EstimateGovernanceEvidence, error)

func (f estimateEvidenceResolverFunc) ResolveEstimateEvidence(ctx context.Context, record EstimateJobRecord) (EstimateGovernanceEvidence, error) {
	return f(ctx, record)
}

type estimateBlockingRenewQueue struct {
	jobs.Repository
	started chan context.Context
	release chan struct{}
}

func (q *estimateBlockingRenewQueue) Renew(ctx context.Context, _ string, _ string, _ int64, _ time.Time, _ time.Duration) error {
	select {
	case q.started <- ctx:
	default:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.release:
		return errors.New("test renewal released")
	}
}

// An uncancellable renewal must not prevent the worker from joining its heartbeat
// after the caller cancels otherwise cooperative estimate work.
func TestEstimateWorkerProcessCancelsInFlightRenewal(t *testing.T) {
	base := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	queue := jobs.NewMemoryRepository()
	repo := NewMemoryEstimateRepository(queue)
	record, _, err := repo.Schedule(context.Background(), estimateTestRequest(), base)
	if err != nil {
		t.Fatal(err)
	}
	blocking := &estimateBlockingRenewQueue{
		Repository: queue,
		started:    make(chan context.Context, 1),
		release:    make(chan struct{}),
	}
	worker := &EstimateWorker{
		Queue:     blocking,
		Estimates: repo,
		Cohorts:   &ExecutionService{},
		Evidence: estimateEvidenceResolverFunc(func(ctx context.Context, _ EstimateJobRecord) (EstimateGovernanceEvidence, error) {
			<-ctx.Done()
			return EstimateGovernanceEvidence{}, ctx.Err()
		}),
		WorkerID:      "worker-cancel",
		ClaimBatch:    1,
		LeaseDuration: 3 * time.Second,
		Clock:         func() time.Time { return base.Add(time.Second) },
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, processErr := worker.Process(ctx)
		result <- processErr
	}()
	t.Cleanup(func() {
		cancel()
		close(blocking.release)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("process did not exit after test renewal was released")
		}
	})
	select {
	case <-blocking.started:
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat did not start a renewal")
	}
	cancel()
	select {
	case processErr := <-result:
		if !errors.Is(processErr, context.Canceled) {
			t.Fatalf("process err=%v, want context cancellation", processErr)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("process did not stop after cancellation of an in-flight renewal")
	}
	if worker.Active() != 0 {
		t.Fatalf("active=%d, want 0 after process returns", worker.Active())
	}
	stored, err := queue.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != jobs.StatusPending {
		t.Fatalf("cancelled work status=%s, want PENDING retry", stored.Status)
	}
}

// Even without caller cancellation, a database renewal has to have a deadline:
// a live caller is not permission for the lease-management operation to hang.
func TestEstimateWorkerHeartbeatBoundsRenewalDeadline(t *testing.T) {
	blocking := &estimateBlockingRenewQueue{
		started: make(chan context.Context, 1),
		release: make(chan struct{}),
	}
	worker := &EstimateWorker{Queue: blocking, LeaseDuration: 3 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	failures := make(chan error, 1)
	done := make(chan struct{})
	go worker.heartbeat(ctx, cancel, jobs.Job{ID: "deadline-job", LeaseOwner: "worker-deadline", LeaseVersion: 1}, failures, done)
	t.Cleanup(func() {
		cancel()
		close(blocking.release)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("heartbeat did not exit after test renewal was released")
		}
	})
	select {
	case renewalCtx := <-blocking.started:
		deadline, ok := renewalCtx.Deadline()
		if !ok || deadline.After(time.Now().Add(10*time.Second)) {
			t.Fatalf("renewal deadline=%v present=%v, want deadline within 10 seconds", deadline, ok)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat did not start a renewal")
	}
}

// Simulate a long first job whose own lease was kept alive. A later job must
// receive its lease when execution is ready, not expire while waiting in a batch.
func TestEstimateWorkerDoesNotExpireWaitingJobsBehindLongRunningWork(t *testing.T) {
	base := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	queue := jobs.NewMemoryRepository()
	repo := NewMemoryEstimateRepository(queue)
	firstRequest := estimateTestRequest()
	firstRequest.ClientRequestID = "cohort-estimate-long-a"
	first, _, err := repo.Schedule(context.Background(), firstRequest, base)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := estimateTestRequest()
	secondRequest.ClientRequestID = "cohort-estimate-long-b"
	second, _, err := repo.Schedule(context.Background(), secondRequest, base.Add(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	secondRecord := repo.items[second.ID]
	secondRecord.Result = &Estimate{
		EligibleCount: 1,
		Breakdown:     &EligibilityBreakdown{MatchedProfiles: 1, ConsentEligible: 1, Unsuppressed: 1, Eligible: 1},
		CalculatedAt:  base,
	}
	repo.items[second.ID] = secondRecord
	repo.mu.Unlock()

	var clock atomic.Int64
	clock.Store(base.Add(time.Second).UnixNano())
	cause := errors.New("first job governance failure")
	worker := &EstimateWorker{
		Queue:     queue,
		Estimates: repo,
		Cohorts:   &ExecutionService{},
		Evidence: estimateEvidenceResolverFunc(func(ctx context.Context, record EstimateJobRecord) (EstimateGovernanceEvidence, error) {
			if record.ID != first.ID {
				return EstimateGovernanceEvidence{}, errors.New("persisted second result should bypass evidence")
			}
			// A successful heartbeat keeps the active first job live beyond the
			// original one-minute batch lease; its waiting peer is not renewed.
			if err := queue.Renew(ctx, record.ID, record.Job.LeaseOwner, record.Job.LeaseVersion, base.Add(time.Second), 3*time.Minute); err != nil {
				return EstimateGovernanceEvidence{}, err
			}
			clock.Store(base.Add(2 * time.Minute).UnixNano())
			return EstimateGovernanceEvidence{}, cause
		}),
		WorkerID:      "worker-long-batch",
		ClaimBatch:    2,
		LeaseDuration: time.Minute,
		Clock:         func() time.Time { return time.Unix(0, clock.Load()).UTC() },
	}
	processed, err := worker.Process(context.Background())
	if !errors.Is(err, cause) {
		t.Fatalf("process err=%v, want first job governance failure", err)
	}
	if errors.Is(err, jobs.ErrLeaseConflict) {
		t.Fatalf("later job expired while waiting for serial execution: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d, want 1 successful later job", processed)
	}
	firstJob, err := queue.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstJob.Status != jobs.StatusPending {
		t.Fatalf("first job status=%s, want PENDING", firstJob.Status)
	}
	secondJob, err := queue.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if secondJob.Status != jobs.StatusCompleted || secondJob.AttemptCount != 1 {
		t.Fatalf("later job status=%s attempts=%d, want COMPLETED in one attempt", secondJob.Status, secondJob.AttemptCount)
	}
}

func TestEstimateWorkerProcessDoesNotClaimAfterCancellation(t *testing.T) {
	base := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	queue := jobs.NewMemoryRepository()
	repo := NewMemoryEstimateRepository(queue)
	record, _, err := repo.Schedule(context.Background(), estimateTestRequest(), base)
	if err != nil {
		t.Fatal(err)
	}
	worker := &EstimateWorker{
		Queue: queue, Estimates: repo, Cohorts: &ExecutionService{}, Evidence: estimateEvidenceErrorResolver{},
		WorkerID: "worker-already-cancelled", ClaimBatch: 1, Clock: func() time.Time { return base.Add(time.Second) },
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	processed, err := worker.Process(ctx)
	if processed != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("processed=%d err=%v, want zero work and cancellation", processed, err)
	}
	stored, err := queue.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != jobs.StatusPending || stored.AttemptCount != 0 {
		t.Fatalf("cancelled caller claimed work: status=%s attempts=%d", stored.Status, stored.AttemptCount)
	}
}

func TestEstimateWorkerProcessStopsAtBatchLimit(t *testing.T) {
	base := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	queue := jobs.NewMemoryRepository()
	repo := NewMemoryEstimateRepository(queue)
	var records []EstimateJobRecord
	for index := 0; index < 3; index++ {
		request := estimateTestRequest()
		request.ClientRequestID = "cohort-estimate-limit-" + string(rune('a'+index))
		record, _, err := repo.Schedule(context.Background(), request, base.Add(time.Duration(index)*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		repo.mu.Lock()
		record.Result = &Estimate{EligibleCount: 1, CalculatedAt: base}
		repo.items[record.ID] = record
		repo.mu.Unlock()
		records = append(records, record)
	}
	worker := &EstimateWorker{
		Queue: queue, Estimates: repo, Cohorts: &ExecutionService{}, Evidence: estimateEvidenceErrorResolver{},
		WorkerID: "worker-limit", ClaimBatch: 2, Clock: func() time.Time { return base.Add(time.Second) },
	}
	processed, err := worker.Process(context.Background())
	if err != nil || processed != 2 {
		t.Fatalf("processed=%d err=%v, want two completed jobs", processed, err)
	}
	untouched, err := queue.Get(context.Background(), records[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	if untouched.Status != jobs.StatusPending || untouched.AttemptCount != 0 {
		t.Fatalf("job beyond batch limit was claimed: status=%s attempts=%d", untouched.Status, untouched.AttemptCount)
	}
}
