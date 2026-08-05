package materialisation

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/audience/cohort"
	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/segment"
)

func testDefinition() audiencefilter.Group {
	return audiencefilter.Group{Join: audiencefilter.JoinAnd, Rules: []audiencefilter.Rule{{DefinitionCode: "COUNTRY", Operator: audiencefilter.OperatorIn, Values: []any{"NG"}}}}
}

func TestScheduleAndCancelMaterialisation(t *testing.T) {
	repo := NewMemoryMaterialisationRepository()
	svc := &MaterialisationService{Repository: repo, Clock: func() time.Time { return time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC) }}
	job, err := svc.Schedule(context.Background(), ScheduleMaterialisation{CampaignID: "campaign-1", Definition: testDefinition(), DefinitionVersion: 1, Eligibility: cohort.EligibilityContext{OrganisationID: "org-1", PurposeID: "purpose-1", Channel: "WHATSAPP"}, ConsentPolicyVersion: "consent-v1", ConfigurationVersion: "config-v1", RequestedBy: "user-1", ExpectedCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != MaterialisationPending || job.ExpectedCount != 2 {
		t.Fatalf("unexpected job %#v", job)
	}
	cancelled, err := svc.Cancel(context.Background(), job.ID, "user-2", "campaign audience no longer required", job.Version)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != MaterialisationCancelled {
		t.Fatalf("status=%s", cancelled.Status)
	}
}

func TestWorkerMaterialisesInRestartSafePages(t *testing.T) {
	ctx := context.Background()
	jobs := NewMemoryMaterialisationRepository()
	snapshots := segment.NewMemoryRepository()
	registry, _ := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	compiler := cohort.NewCompiler(registry)
	queryRepo := &cohort.MemoryQueryRepository{EligibleContactIDs: []string{"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002", "00000000-0000-0000-0000-000000000003"}}
	executions := cohort.NewExecutionService(compiler, queryRepo)
	svc := &MaterialisationService{Repository: jobs}
	job, err := svc.Schedule(ctx, ScheduleMaterialisation{CampaignID: "10000000-0000-0000-0000-000000000001", Definition: testDefinition(), DefinitionVersion: 1, Eligibility: cohort.EligibilityContext{OrganisationID: "10000000-0000-0000-0000-000000000002", PurposeID: "purpose", Channel: "WHATSAPP"}, ConsentPolicyVersion: "v1", ConfigurationVersion: "v1", RequestedBy: "10000000-0000-0000-0000-000000000003", ExpectedCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	worker := &MaterialisationWorker{Repository: jobs, Cohorts: executions, Snapshots: snapshots, WorkerID: "worker-a", BatchSize: 2, ClaimBatch: 1, LeaseDuration: time.Minute}
	if err := worker.runOnce(ctx); err != nil {
		t.Fatal(err)
	}
	first, _ := jobs.Get(ctx, job.ID)
	if first.ProcessedCount != 2 || first.Status != MaterialisationRunning {
		t.Fatalf("first=%#v", first)
	}
	if err := worker.runOnce(ctx); err != nil {
		t.Fatal(err)
	}
	second, _ := jobs.Get(ctx, job.ID)
	if second.ProcessedCount != 3 {
		t.Fatalf("second=%#v", second)
	}
	if err := worker.runOnce(ctx); err != nil {
		t.Fatal(err)
	}
	final, _ := jobs.Get(ctx, job.ID)
	if final.Status != MaterialisationCompleted || final.SnapshotID == "" {
		t.Fatalf("final=%#v", final)
	}
	stored, err := snapshots.Get(ctx, final.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.EligibleCount != 3 {
		t.Fatalf("count=%d", stored.EligibleCount)
	}
}

func TestWorkerFailsWhenEligibleCountChanges(t *testing.T) {
	ctx := context.Background()
	jobs := NewMemoryMaterialisationRepository()
	snapshots := segment.NewMemoryRepository()
	registry, _ := audiencefilter.NewRegistry(audiencefilter.DefaultDefinitions()...)
	executions := cohort.NewExecutionService(cohort.NewCompiler(registry), &cohort.MemoryQueryRepository{EligibleContactIDs: []string{"a"}})
	svc := &MaterialisationService{Repository: jobs}
	job, err := svc.Schedule(ctx, ScheduleMaterialisation{CampaignID: "c", Definition: testDefinition(), DefinitionVersion: 1, Eligibility: cohort.EligibilityContext{OrganisationID: "o", PurposeID: "p", Channel: "WHATSAPP"}, ConsentPolicyVersion: "v1", ConfigurationVersion: "v1", RequestedBy: "u", ExpectedCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	worker := &MaterialisationWorker{Repository: jobs, Cohorts: executions, Snapshots: snapshots, WorkerID: "w", BatchSize: 10, ClaimBatch: 1, LeaseDuration: time.Minute}
	_ = worker.runOnce(ctx)
	_ = worker.runOnce(ctx)
	got, _ := jobs.Get(ctx, job.ID)
	if got.Status != MaterialisationFailed {
		t.Fatalf("status=%s", got.Status)
	}
}

func TestScheduleIsIdempotentAndRejectsDifferentActiveRequest(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryMaterialisationRepository()
	svc := &MaterialisationService{Repository: repo}
	input := ScheduleMaterialisation{CampaignID: "campaign-1", Definition: testDefinition(), DefinitionVersion: 1, Eligibility: cohort.EligibilityContext{OrganisationID: "org-1", PurposeID: "purpose-1", Channel: "WHATSAPP"}, ConsentPolicyVersion: "consent-v1", ConfigurationVersion: "config-v1", RequestedBy: "user-1", ExpectedCount: 2}
	first, err := svc.Schedule(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Schedule(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.RequestFingerprint != first.RequestFingerprint {
		t.Fatalf("idempotent replay created a different job: first=%#v second=%#v", first, second)
	}
	input.ExpectedCount = 3
	if _, err := svc.Schedule(ctx, input); !errors.Is(err, ErrMaterialisationConflict) {
		t.Fatalf("expected active campaign conflict, got %v", err)
	}
}

func TestCancellationRequiresReasonAndPreservesEvidence(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryMaterialisationRepository()
	svc := &MaterialisationService{Repository: repo}
	job, err := svc.Schedule(ctx, ScheduleMaterialisation{CampaignID: "campaign-1", Definition: testDefinition(), DefinitionVersion: 1, Eligibility: cohort.EligibilityContext{OrganisationID: "org-1", PurposeID: "purpose-1", Channel: "WHATSAPP"}, ConsentPolicyVersion: "consent-v1", ConfigurationVersion: "config-v1", RequestedBy: "user-1", ExpectedCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel(ctx, job.ID, "user-2", "no", job.Version); err == nil {
		t.Fatal("expected short cancellation reason to be rejected")
	}
	cancelled, err := svc.Cancel(ctx, job.ID, "user-2", "campaign was withdrawn by the operator", job.Version)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.CancelledBy != "user-2" || cancelled.CancellationReason == "" || cancelled.CompletedAt == nil {
		t.Fatalf("cancellation evidence missing: %#v", cancelled)
	}
}

func TestLeaseRenewalRejectsStaleOwnerAndToken(t *testing.T) {
	ctx := context.Background()
	repo := NewMemoryMaterialisationRepository()
	svc := &MaterialisationService{Repository: repo}
	job, err := svc.Schedule(ctx, ScheduleMaterialisation{CampaignID: "campaign-1", Definition: testDefinition(), DefinitionVersion: 1, Eligibility: cohort.EligibilityContext{OrganisationID: "org-1", PurposeID: "purpose-1", Channel: "WHATSAPP"}, ConsentPolicyVersion: "consent-v1", ConfigurationVersion: "config-v1", RequestedBy: "user-1", ExpectedCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claimed, err := repo.Claim(ctx, "worker-a", 1, time.Minute, now)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v %#v", err, claimed)
	}
	if err := repo.Renew(ctx, job.ID, "worker-b", claimed[0].LeaseToken, now.Add(time.Second), time.Minute); !errors.Is(err, ErrMaterialisationConflict) {
		t.Fatalf("expected stale owner conflict, got %v", err)
	}
	if err := repo.Renew(ctx, job.ID, "worker-a", claimed[0].LeaseToken+1, now.Add(time.Second), time.Minute); !errors.Is(err, ErrMaterialisationConflict) {
		t.Fatalf("expected stale token conflict, got %v", err)
	}
	if err := repo.Renew(ctx, job.ID, "worker-a", claimed[0].LeaseToken, now.Add(time.Second), time.Minute); err != nil {
		t.Fatal(err)
	}
}
