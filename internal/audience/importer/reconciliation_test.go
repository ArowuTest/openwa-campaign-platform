package importer

import (
	"context"
	"testing"
	"time"
)

func TestReconciliationServiceClosesBalancedImportIdempotently(t *testing.T) {
	now := time.Date(2026, 8, 5, 13, 0, 0, 0, time.UTC)
	importsRepo := NewMemoryImportRepository()
	importsRepo.items["imp-1"] = ImportBatch{ID: "imp-1", Status: ImportCompletedWithExceptions, UploadedRows: 100, ValidRows: 80, InvalidRows: 10, DuplicateRows: 5, SuppressedRows: 5, InsertedContacts: 50, UpdatedContacts: 30}
	conflictsRepo := NewMemoryConflictRepository()
	resolved := conflictsRepo.Add(ProfileConflict{AudienceImportID: "imp-1", Status: ConflictPending})
	if _, err := conflictsRepo.Resolve(context.Background(), resolved.ID, ResolutionKeepExisting, "retain verified existing value", "reviewer-1", 1, now); err != nil {
		t.Fatal(err)
	}
	service := &ReconciliationService{
		Imports:    &ImportService{Repository: importsRepo},
		Conflicts:  &ConflictService{Repository: conflictsRepo},
		Repository: NewMemoryReconciliationRepository(), Clock: func() time.Time { return now },
	}
	record, created, err := service.Close(context.Background(), "imp-1", "reviewer-1", "balanced totals and resolved conflicts")
	if err != nil {
		t.Fatal(err)
	}
	if !created || record.EvidenceHash == "" || !record.Evidence.ReadyForClosure {
		t.Fatalf("unexpected record: %+v", record)
	}
	replayed, created, err := service.Close(context.Background(), "imp-1", "reviewer-1", "balanced totals and resolved conflicts")
	if err != nil {
		t.Fatal(err)
	}
	if created || replayed.ID != record.ID {
		t.Fatalf("expected idempotent replay: %+v", replayed)
	}
}

func TestReconciliationServiceRejectsPendingConflicts(t *testing.T) {
	importsRepo := NewMemoryImportRepository()
	importsRepo.items["imp-1"] = ImportBatch{ID: "imp-1", Status: ImportCompletedWithExceptions, UploadedRows: 10, ValidRows: 10, InsertedContacts: 10}
	conflictsRepo := NewMemoryConflictRepository()
	conflictsRepo.Add(ProfileConflict{AudienceImportID: "imp-1", Status: ConflictPending})
	service := &ReconciliationService{Imports: &ImportService{Repository: importsRepo}, Conflicts: &ConflictService{Repository: conflictsRepo}, Repository: NewMemoryReconciliationRepository()}
	if _, _, err := service.Close(context.Background(), "imp-1", "reviewer-1", "attempt premature closure"); err != ErrReconciliationNotReady {
		t.Fatalf("expected not ready, got %v", err)
	}
}
