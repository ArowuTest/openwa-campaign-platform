package segment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDefinitionVersionPageContinuesNewestFirst(t *testing.T) {
	repo := NewMemoryDefinitionRepository()
	repo.items["segment-a"] = Definition{ID: "segment-a", Version: 3}
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo.versions["segment-a"] = []DefinitionVersion{
		{SegmentID: "segment-a", Version: 1, CreatedAt: base},
		{SegmentID: "segment-a", Version: 2, CreatedAt: base.Add(time.Minute)},
		{SegmentID: "segment-a", Version: 3, CreatedAt: base.Add(2 * time.Minute)},
	}
	service := &DefinitionService{Repository: repo}
	first, err := service.VersionsPage(context.Background(), "segment-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].Version != 3 || first.Items[1].Version != 2 {
		t.Fatalf("unexpected first version page: %#v", first)
	}
	second, err := service.VersionsPage(context.Background(), "segment-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].Version != 1 {
		t.Fatalf("unexpected second version page: %#v", second)
	}
	if _, err := service.VersionsPage(context.Background(), "segment-a", 2, "invalid"); !errors.Is(err, ErrInvalidVersionCursor) {
		t.Fatalf("invalid segment-version cursor accepted: %v", err)
	}
}
