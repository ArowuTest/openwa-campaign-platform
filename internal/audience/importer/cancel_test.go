package importer

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPreviewReadyImportCanCancelBeforeMerge(t *testing.T) {
	now := time.Date(2026, 8, 8, 19, 0, 0, 0, time.UTC)
	repo := NewMemoryImportRepository()
	batch := ImportBatch{ID: "import-1", Status: ImportPreviewReady, Version: 4, UploadedBy: "maker", UpdatedAt: now.Add(-time.Minute)}
	repo.items[batch.ID] = batch
	service := &ImportService{Repository: repo, Clock: func() time.Time { return now }}

	cancelled, err := service.Cancel(context.Background(), batch.ID, "operator", "preview shows the wrong audience source", batch.Version)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != ImportCancelled || cancelled.Version != 5 || cancelled.FailureReason != "preview shows the wrong audience source" {
		t.Fatalf("unexpected cancelled import: %+v", cancelled)
	}
}

func TestApprovedImportCannotBeCancelledAsPreview(t *testing.T) {
	repo := NewMemoryImportRepository()
	repo.items["import-1"] = ImportBatch{ID: "import-1", Status: ImportApproved, Version: 5, UploadedBy: "maker"}
	service := &ImportService{Repository: repo}
	_, err := service.Cancel(context.Background(), "import-1", "operator", "cancel after approval is unsafe", 5)
	if !errors.Is(err, ErrImportTransition) {
		t.Fatalf("expected transition rejection, got %v", err)
	}
}
