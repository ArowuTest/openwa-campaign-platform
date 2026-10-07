package cohort

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/jobs"
)

type estimateGovernanceQueryRepository struct {
	count func()
}

func (*estimateGovernanceQueryRepository) Count(context.Context, CompiledQuery) (int64, error) {
	return 0, errors.New("expected breakdown query")
}

func (*estimateGovernanceQueryRepository) Members(context.Context, CompiledQuery, int) ([]string, error) {
	return nil, errors.New("unexpected member query")
}

func (r *estimateGovernanceQueryRepository) CountBreakdown(context.Context, CompiledQuery) (EligibilityBreakdown, error) {
	if r.count != nil {
		r.count()
	}
	return EligibilityBreakdown{MatchedProfiles: 42, ConsentEligible: 42, Unsuppressed: 42, Eligible: 42}, nil
}

func estimateWorkerGovernanceEvidence() EstimateGovernanceEvidence {
	return EstimateGovernanceEvidence{
		ConsentReviewID:           "44444444-4444-4444-8444-444444444444",
		ConsentReviewVersion:      7,
		ConsentWordingVersion:     "v7",
		OrganisationPolicyID:      "55555555-5555-4555-8555-555555555555",
		OrganisationPolicyVersion: 9,
	}
}

func newEstimateWorkerGovernanceFixture(t *testing.T, queries QueryRepository, evidence EstimateEvidenceResolver) (*EstimateWorker, *MemoryEstimateRepository, EstimateJobRecord) {
	t.Helper()
	base := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	queue := jobs.NewMemoryRepository()
	repo := NewMemoryEstimateRepository(queue)
	record, _, err := repo.Schedule(context.Background(), estimateTestRequest(), base)
	if err != nil {
		t.Fatal(err)
	}
	worker := &EstimateWorker{
		Queue: queue, Estimates: repo, Cohorts: testExecutionService(queries), Evidence: evidence,
		WorkerID: "governance-worker", ClaimBatch: 1, LeaseDuration: time.Minute,
		Clock: func() time.Time { return base.Add(time.Second) },
	}
	return worker, repo, record
}

// A count can run after policy rotation/review changes. Publishing its old
// evidence label would falsely claim the result used that governance version.
func TestEstimateWorkerRejectsGovernanceChangesDuringCount(t *testing.T) {
	tests := []struct {
		name   string
		change func(*EstimateGovernanceEvidence)
	}{
		{"review ID", func(e *EstimateGovernanceEvidence) { e.ConsentReviewID = "44444444-4444-4444-8444-444444444445" }},
		{"review version", func(e *EstimateGovernanceEvidence) { e.ConsentReviewVersion = 8 }},
		{"wording version", func(e *EstimateGovernanceEvidence) { e.ConsentWordingVersion = "v8" }},
		{"policy ID", func(e *EstimateGovernanceEvidence) { e.OrganisationPolicyID = "55555555-5555-4555-8555-555555555556" }},
		{"policy version", func(e *EstimateGovernanceEvidence) { e.OrganisationPolicyVersion = 10 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := estimateWorkerGovernanceEvidence()
			queries := &estimateGovernanceQueryRepository{count: func() { test.change(&evidence) }}
			resolver := estimateEvidenceResolverFunc(func(context.Context, EstimateJobRecord) (EstimateGovernanceEvidence, error) {
				return evidence, nil
			})
			worker, repo, record := newEstimateWorkerGovernanceFixture(t, queries, resolver)
			processed, err := worker.Process(context.Background())
			if err == nil || processed != 0 {
				t.Fatalf("governance changed during count but worker published: processed=%d err=%v", processed, err)
			}
			stored, err := repo.Get(context.Background(), record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Result != nil || stored.Job.Status != jobs.StatusPending {
				t.Fatalf("changed governance must leave no result and retryable job: %+v", stored)
			}
		})
	}
}

func TestEstimateWorkerRejectsGovernanceUnavailableAfterCount(t *testing.T) {
	counted := false
	queries := &estimateGovernanceQueryRepository{count: func() { counted = true }}
	resolver := estimateEvidenceResolverFunc(func(context.Context, EstimateJobRecord) (EstimateGovernanceEvidence, error) {
		if counted {
			return EstimateGovernanceEvidence{}, errors.New("policy retired during count")
		}
		return estimateWorkerGovernanceEvidence(), nil
	})
	worker, repo, record := newEstimateWorkerGovernanceFixture(t, queries, resolver)
	processed, err := worker.Process(context.Background())
	if err == nil || processed != 0 {
		t.Fatalf("unavailable post-count governance was published: processed=%d err=%v", processed, err)
	}
	stored, err := repo.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Result != nil || stored.Job.Status != jobs.StatusPending {
		t.Fatalf("unavailable governance must leave no result and retryable job: %+v", stored)
	}
}

func TestEstimateWorkerPublishesWithUnchangedGovernanceAfterCount(t *testing.T) {
	evidence := estimateWorkerGovernanceEvidence()
	resolver := estimateEvidenceResolverFunc(func(context.Context, EstimateJobRecord) (EstimateGovernanceEvidence, error) {
		return evidence, nil
	})
	worker, repo, record := newEstimateWorkerGovernanceFixture(t, &estimateGovernanceQueryRepository{}, resolver)
	processed, err := worker.Process(context.Background())
	if err != nil || processed != 1 {
		t.Fatalf("processed=%d err=%v", processed, err)
	}
	stored, err := repo.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Result == nil || stored.Result.EligibleCount != 42 || stored.Evidence != evidence || stored.Job.Status != jobs.StatusCompleted {
		t.Fatalf("unchanged governance result=%+v", stored)
	}
}

func TestEstimateWorkerRecoversPersistedResultWithoutGovernanceResolution(t *testing.T) {
	worker, repo, record := newEstimateWorkerGovernanceFixture(t, &estimateGovernanceQueryRepository{}, estimateEvidenceErrorResolver{})
	now := worker.now()
	claimed, err := worker.Queue.Claim(context.Background(), "prior-worker", now, time.Minute, 1, []string{EstimateJobType})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	result := Estimate{EligibleCount: 42, Breakdown: &EligibilityBreakdown{MatchedProfiles: 42, ConsentEligible: 42, Unsuppressed: 42, Eligible: 42}, CalculatedAt: now}
	evidence := estimateWorkerGovernanceEvidence()
	if err := repo.StoreResult(context.Background(), record.ID, claimed[0].LeaseOwner, claimed[0].LeaseVersion, result, evidence, now); err != nil {
		t.Fatal(err)
	}
	// Release the prior lease without completing the persisted result.
	if err := worker.Queue.Fail(context.Background(), record.ID, claimed[0].LeaseOwner, claimed[0].LeaseVersion, now, true, 0, "RESTART", "process interrupted after storing result"); err != nil {
		t.Fatal(err)
	}
	worker.Cohorts = &ExecutionService{}
	processed, err := worker.Process(context.Background())
	if err != nil || processed != 1 {
		t.Fatalf("recovery unnecessarily resolved/recounted governance: processed=%d err=%v", processed, err)
	}
	stored, err := repo.Get(context.Background(), record.ID)
	if err != nil || stored.Result == nil || stored.Result.EligibleCount != 42 || stored.Evidence != evidence || stored.Job.Status != jobs.StatusCompleted {
		t.Fatalf("recovered=%+v err=%v", stored, err)
	}
}
