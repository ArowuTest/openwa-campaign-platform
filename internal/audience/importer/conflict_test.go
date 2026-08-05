package importer

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConflictServiceRequiresReasonAndOptimisticVersion(t *testing.T) {
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryConflictRepository()
	item := repo.Add(ProfileConflict{AudienceImportID: "import-1", MaskedMSISDN: "+234 *** 1234", Field: "state", ExistingValue: "Lagos", IncomingValue: "Ogun", CreatedAt: now})
	service := &ConflictService{Repository: repo, Clock: func() time.Time { return now.Add(time.Minute) }}
	if _, err := service.Resolve(context.Background(), item.ID, ResolutionUseIncoming, "short", "reviewer-1", 1); err == nil {
		t.Fatal("expected short reason to be rejected")
	}
	if _, err := service.Resolve(context.Background(), item.ID, ResolutionUseIncoming, "verified against source evidence", "reviewer-1", 2); !errors.Is(err, ErrConflictVersion) {
		t.Fatalf("expected optimistic version conflict, got %v", err)
	}
	resolved, err := service.Resolve(context.Background(), item.ID, ResolutionKeepExisting, "verified against source evidence", "reviewer-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != ConflictResolved || resolved.Version != 2 || resolved.Resolution != ResolutionKeepExisting {
		t.Fatalf("unexpected resolution: %+v", resolved)
	}
	if _, err := service.Resolve(context.Background(), item.ID, ResolutionUseIncoming, "later contradictory decision", "reviewer-2", 2); !errors.Is(err, ErrConflictState) {
		t.Fatalf("expected terminal conflict state, got %v", err)
	}
}

func TestManualConflictPolicyCapturesDifferencesWithoutOverwriting(t *testing.T) {
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryMergeRepository()
	first := approvedMemoryImport(now)
	first.ID = "import-first"
	first.Candidates = []ContactCandidate{protectedMergeCandidate(7, now.Add(-time.Hour))}
	repo.Seed(first)
	service := MergeService{Repository: repo, Clock: func() time.Time { return now }}
	if _, err := service.Merge(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}

	second := approvedMemoryImport(now)
	second.ID = "import-second"
	second.UpdatePolicy = UpdateManualConflict
	candidate := protectedMergeCandidate(7, now)
	candidate.State = "Ogun"
	candidate.LGA = "Abeokuta South"
	age := 35
	candidate.ReportedAge = &age
	second.Candidates = []ContactCandidate{candidate}
	repo.Seed(second)
	result, err := service.Merge(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Conflicts != 3 {
		t.Fatalf("expected 3 conflicts, got %+v", result)
	}
	stored := repo.contacts[string(candidate.LookupHMAC)].Candidate
	if stored.State != "Lagos" || stored.LGA != "Ikeja" || stored.ReportedAge == nil || *stored.ReportedAge != 27 {
		t.Fatalf("manual conflict policy overwrote canonical profile: %+v", stored)
	}
	items, err := repo.ConflictRepository().List(context.Background(), second.ID, ConflictPending, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 pending conflict records, got %d", len(items))
	}
}

func TestInsertOnlyPolicyNeverChangesExistingContact(t *testing.T) {
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryMergeRepository()
	first := approvedMemoryImport(now)
	first.ID = "insert-only-first"
	first.Candidates = []ContactCandidate{protectedMergeCandidate(11, now.Add(-time.Hour))}
	repo.Seed(first)
	service := MergeService{Repository: repo, Clock: func() time.Time { return now }}
	if _, err := service.Merge(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	second := approvedMemoryImport(now)
	second.ID = "insert-only-second"
	second.UpdatePolicy = UpdateInsertOnly
	candidate := protectedMergeCandidate(11, now)
	candidate.State = "Ogun"
	second.Candidates = []ContactCandidate{candidate}
	repo.Seed(second)
	result, err := service.Merge(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.UpdatedContacts != 0 || result.InsertedContacts != 0 {
		t.Fatalf("insert-only modified existing contact: %+v", result)
	}
	if got := repo.contacts[string(candidate.LookupHMAC)].Candidate.State; got != "Lagos" {
		t.Fatalf("insert-only overwrote state: %s", got)
	}
}

func TestTrustedSourcePolicyUsesGovernedTrustBeforeRecency(t *testing.T) {
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	repo := NewMemoryMergeRepository()
	repo.SetSourceTrust("org-1", "CRM_VERIFIED", 90)
	repo.SetSourceTrust("org-1", "EVENT_FORM", 20)
	first := approvedMemoryImport(now)
	first.ID = "trusted-first"
	first.SourceSystem = "CRM_VERIFIED"
	first.Candidates = []ContactCandidate{protectedMergeCandidate(13, now.Add(-24*time.Hour))}
	repo.Seed(first)
	service := MergeService{Repository: repo, Clock: func() time.Time { return now }}
	if _, err := service.Merge(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}

	lower := approvedMemoryImport(now)
	lower.ID = "trusted-lower"
	lower.SourceSystem = "EVENT_FORM"
	lower.UpdatePolicy = UpdateTrustedSource
	candidate := protectedMergeCandidate(13, now)
	candidate.State = "Ogun"
	lower.Candidates = []ContactCandidate{candidate}
	repo.Seed(lower)
	if _, err := service.Merge(context.Background(), lower.ID); err != nil {
		t.Fatal(err)
	}
	if got := repo.contacts[string(candidate.LookupHMAC)].Candidate.State; got != "Lagos" {
		t.Fatalf("lower-trust source overwrote profile: %s", got)
	}

	higher := approvedMemoryImport(now)
	higher.ID = "trusted-higher"
	higher.SourceSystem = "CRM_VERIFIED"
	higher.UpdatePolicy = UpdateTrustedSource
	higherCandidate := protectedMergeCandidate(13, now.Add(time.Hour))
	higherCandidate.State = "Abuja"
	higher.Candidates = []ContactCandidate{higherCandidate}
	repo.Seed(higher)
	if _, err := service.Merge(context.Background(), higher.ID); err != nil {
		t.Fatal(err)
	}
	if got := repo.contacts[string(higherCandidate.LookupHMAC)].Candidate.State; got != "Abuja" {
		t.Fatalf("equal trusted newer source did not update profile: %s", got)
	}
}

func TestBuildReconciliationDetectsBalancedAndPendingConflicts(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	batch := ImportBatch{ID: "imp-1", Status: ImportCompletedWithExceptions, UploadedRows: 100, ValidRows: 80, InvalidRows: 10, DuplicateRows: 5, SuppressedRows: 5, InsertedContacts: 50, UpdatedContacts: 30}
	result := BuildReconciliation(batch, ConflictSummary{Pending: 2, Resolved: 3, Total: 5}, now)
	if !result.ValidationBalanced || !result.MergeWithinValidRows {
		t.Fatalf("expected balanced reconciliation: %+v", result)
	}
	if result.ReadyForClosure {
		t.Fatal("pending conflicts must prevent closure")
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != "PENDING_PROFILE_CONFLICTS" {
		t.Fatalf("unexpected warnings: %+v", result.Warnings)
	}

	result = BuildReconciliation(batch, ConflictSummary{Resolved: 5, Total: 5}, now)
	if !result.ReadyForClosure {
		t.Fatalf("expected ready for closure: %+v", result)
	}
}

func TestMemoryConflictRepositorySummary(t *testing.T) {
	repository := NewMemoryConflictRepository()
	repository.Add(ProfileConflict{AudienceImportID: "imp-1", Status: ConflictPending})
	resolved := repository.Add(ProfileConflict{AudienceImportID: "imp-1", Status: ConflictPending})
	if _, err := repository.Resolve(context.Background(), resolved.ID, ResolutionKeepExisting, "retain trusted source", "reviewer-1", 1, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	repository.Add(ProfileConflict{AudienceImportID: "other", Status: ConflictPending})
	summary, err := repository.Summary(context.Background(), "imp-1")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Pending != 1 || summary.Resolved != 1 || summary.Total != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestConflictServiceResolveBatchIsAtomic(t *testing.T) {
	repository := NewMemoryConflictRepository()
	first := repository.Add(ProfileConflict{AudienceImportID: "imp-1", Status: ConflictPending})
	second := repository.Add(ProfileConflict{AudienceImportID: "imp-1", Status: ConflictPending})
	service := &ConflictService{Repository: repository, Clock: func() time.Time { return time.Date(2026, 8, 5, 14, 0, 0, 0, time.UTC) }}
	result, err := service.ResolveBatch(context.Background(), []ConflictDecision{
		{ConflictID: first.ID, Resolution: ResolutionKeepExisting, Reason: "retain verified existing value", ExpectedVersion: 1},
		{ConflictID: second.ID, Resolution: ResolutionUseIncoming, Reason: "accept verified incoming value", ExpectedVersion: 1},
	}, "reviewer-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 2 || len(result.Resolved) != 2 {
		t.Fatalf("unexpected batch result: %+v", result)
	}

	third := repository.Add(ProfileConflict{AudienceImportID: "imp-1", Status: ConflictPending})
	fourth := repository.Add(ProfileConflict{AudienceImportID: "imp-1", Status: ConflictPending})
	_, err = service.ResolveBatch(context.Background(), []ConflictDecision{
		{ConflictID: third.ID, Resolution: ResolutionKeepExisting, Reason: "retain verified existing value", ExpectedVersion: 1},
		{ConflictID: fourth.ID, Resolution: ResolutionUseIncoming, Reason: "accept verified incoming value", ExpectedVersion: 99},
	}, "reviewer-1")
	if err != ErrConflictVersion {
		t.Fatalf("expected version error, got %v", err)
	}
	items, err := repository.List(context.Background(), "imp-1", ConflictPending, 100)
	if err != nil {
		t.Fatal(err)
	}
	pending := map[string]bool{}
	for _, item := range items {
		pending[item.ID] = true
	}
	if !pending[third.ID] || !pending[fourth.ID] {
		t.Fatal("failed batch must not partially resolve conflicts")
	}
}
