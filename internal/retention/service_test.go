package retention

import (
	"context"
	"errors"
	"testing"
	"time"
)

func activatePolicy(t *testing.T, admin *Administration, policy Policy, maker string) Policy {
	t.Helper()
	value, err := admin.Create(context.Background(), policy, maker, "create retention policy")
	if err != nil {
		t.Fatal(err)
	}
	value, err = admin.Submit(context.Background(), value.ID, value.Version, maker+"-submit", "submit retention policy")
	if err != nil {
		t.Fatal(err)
	}
	value, err = admin.Decide(context.Background(), value.ID, value.Version, true, maker+"-approve", "approve retention policy")
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestRetentionPolicyFutureReplacementPreservesCurrentPolicy(t *testing.T) {
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	admin := &Administration{Store: store, Clock: func() time.Time { return now }}
	first := activatePolicy(t, admin, Policy{Name: "inbound retention", ObjectType: ObjectInboundContent, Action: ActionAnonymise, ScopeType: ScopePlatform, RetentionDays: 30, EffectiveFrom: now.Add(-time.Hour)}, "first")
	future := now.Add(24 * time.Hour)
	second := activatePolicy(t, admin, Policy{Name: "future inbound retention", ObjectType: ObjectInboundContent, Action: ActionAnonymise, ScopeType: ScopePlatform, RetentionDays: 45, EffectiveFrom: future}, "second")
	stored, _ := store.GetPolicy(context.Background(), first.ID)
	if stored.Status != StatusActive || stored.EffectiveTo == nil || !stored.EffectiveTo.Equal(future) {
		t.Fatalf("current policy retired early: %#v", stored)
	}
	events, err := store.ListEvents(context.Background(), first.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	foundSuperseded := false
	for _, event := range events {
		if event.EventType == "SUPERSEDED" && event.Evidence["supersededById"] == second.ID {
			foundSuperseded = true
		}
	}
	if !foundSuperseded {
		t.Fatalf("retention supersession evidence missing: %#v", events)
	}
	if second.Status != StatusActive {
		t.Fatalf("future replacement not approved: %#v", second)
	}

	backdated, _ := admin.Create(context.Background(), Policy{Name: "backdated", ObjectType: ObjectInboundContent, Action: ActionAnonymise, ScopeType: ScopePlatform, RetentionDays: 10, EffectiveFrom: now.Add(-2 * time.Hour)}, "third", "create backdated retention")
	backdated, _ = admin.Submit(context.Background(), backdated.ID, backdated.Version, "third-submit", "submit backdated retention")
	if _, err := admin.Decide(context.Background(), backdated.ID, backdated.Version, true, "third-approve", "approve backdated retention"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected overlap conflict, got %v", err)
	}
}

type fixedExecutor struct {
	evidence map[string]any
	err      error
}

func (f fixedExecutor) Execute(context.Context, Job, time.Time) (map[string]any, error) {
	return f.evidence, f.err
}

func TestRetentionWorkerFencesReclaimedJobsAndHonoursRetryAvailability(t *testing.T) {
	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	if err := store.AddJob(Job{ID: "job-1", PolicyID: "policy", ObjectType: ObjectIncident, ObjectID: "incident", Action: ActionReviewRequired, Status: JobPending, CreatedAt: now, UpdatedAt: now, AvailableAt: now}); err != nil {
		t.Fatal(err)
	}
	first, err := store.ClaimJobs(context.Background(), "worker-a", now, time.Minute, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: %#v %v", first, err)
	}
	later := now.Add(2 * time.Minute)
	second, err := store.ClaimJobs(context.Background(), "worker-b", later, time.Minute, 1)
	if err != nil || len(second) != 1 {
		t.Fatalf("reclaim: %#v %v", second, err)
	}
	if err = store.CompleteJob(context.Background(), first[0], map[string]any{"bad": true}, later); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale worker completed reclaimed job: %v", err)
	}
	if err = store.FailJob(context.Background(), second[0], "FAILED", "safe", later, time.Minute); err != nil {
		t.Fatal(err)
	}
	immediate, _ := store.ClaimJobs(context.Background(), "worker-c", later, time.Minute, 1)
	if len(immediate) != 0 {
		t.Fatalf("retry claimed before availability: %#v", immediate)
	}
	retry, _ := store.ClaimJobs(context.Background(), "worker-c", later.Add(time.Minute), time.Minute, 1)
	if len(retry) != 1 {
		t.Fatalf("retry not reclaimable at availability: %#v", retry)
	}
}

func TestRetentionWorkerCompletesAndHoldsReviewJobs(t *testing.T) {
	now := time.Date(2026, 8, 6, 11, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	if err := store.AddJob(Job{ID: "complete", PolicyID: "p", ObjectType: ObjectInboundContent, ObjectID: "x", Action: ActionAnonymise, Status: JobPending, CreatedAt: now, UpdatedAt: now, AvailableAt: now}); err != nil {
		t.Fatal(err)
	}
	worker := &Worker{Store: store, Executor: fixedExecutor{evidence: map[string]any{"done": true}}, WorkerID: "worker", Lease: time.Minute, Batch: 10, Clock: func() time.Time { return now }}
	if count, err := worker.Process(context.Background()); err != nil || count != 1 {
		t.Fatalf("complete process: %d %v", count, err)
	}
	completed, _ := store.ListJobs(context.Background(), JobCompleted, 10)
	if len(completed) != 1 || completed[0].Evidence["done"] != true {
		t.Fatalf("completion evidence missing: %#v", completed)
	}

	if err := store.AddJob(Job{ID: "review", PolicyID: "p", ObjectType: ObjectAuditEvent, ObjectID: "y", Action: ActionDelete, Status: JobPending, CreatedAt: now, UpdatedAt: now, AvailableAt: now}); err != nil {
		t.Fatal(err)
	}
	worker.Executor = fixedExecutor{err: ReviewRequiredError{Reason: "legal review required", Evidence: map[string]any{"held": true}}}
	if _, err := worker.Process(context.Background()); err != nil {
		t.Fatal(err)
	}
	held, _ := store.ListJobs(context.Background(), JobHeldReview, 10)
	if len(held) != 1 || held[0].LastErrorReference != "legal review required" {
		t.Fatalf("held review missing: %#v", held)
	}
}

func TestRetentionJobPageContinuesWithoutDuplicates(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	store := NewMemoryStore()
	for _, job := range []Job{
		{ID: "job-a", Status: JobPending, CreatedAt: base, UpdatedAt: base},
		{ID: "job-b", Status: JobPending, CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute)},
		{ID: "job-c", Status: JobPending, CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base.Add(2 * time.Minute)},
	} {
		if err := store.AddJob(job); err != nil {
			t.Fatal(err)
		}
	}
	admin := &Administration{Store: store}
	first, err := admin.JobsPage(context.Background(), "", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "job-c" || first.Items[1].ID != "job-b" {
		t.Fatalf("unexpected first retention-job page: %#v", first)
	}
	second, err := admin.JobsPage(context.Background(), "", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "job-a" {
		t.Fatalf("unexpected second retention-job page: %#v", second)
	}
	if _, err := admin.JobsPage(context.Background(), "", 2, "invalid"); !errors.Is(err, ErrInvalidRetentionJobCursor) {
		t.Fatalf("invalid retention-job cursor accepted: %v", err)
	}
}
