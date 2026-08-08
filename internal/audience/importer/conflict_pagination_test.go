package importer

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConflictPagesContinueAcrossCreatedAtAndID(t *testing.T) {
	repo := NewMemoryConflictRepository()
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	for _, item := range []ProfileConflict{
		{ID: "conflict-c", AudienceImportID: "import-a", Status: ConflictPending, CreatedAt: base.Add(time.Minute)},
		{ID: "conflict-a", AudienceImportID: "import-a", Status: ConflictPending, CreatedAt: base},
		{ID: "conflict-b", AudienceImportID: "import-a", Status: ConflictPending, CreatedAt: base},
	} {
		repo.Add(item)
	}
	service := &ConflictService{Repository: repo}
	first, err := service.ListPage(context.Background(), "import-a", ConflictPending, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "conflict-a" || first.Items[1].ID != "conflict-b" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second, err := service.ListPage(context.Background(), "import-a", ConflictPending, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "conflict-c" {
		t.Fatalf("unexpected second page: %#v", second)
	}
	if _, err := service.ListPage(context.Background(), "import-a", ConflictPending, 2, "invalid"); !errors.Is(err, ErrInvalidConflictCursor) {
		t.Fatalf("expected invalid cursor rejection, got %v", err)
	}
}
