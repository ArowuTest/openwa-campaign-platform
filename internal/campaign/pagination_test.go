package campaign

import (
	"context"
	"testing"
	"time"
)

func TestCampaignListUsesStableKeysetCursor(t *testing.T) {
	repo := NewMemoryRepository()
	base := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	for _, item := range []Campaign{
		{ID: "00000000-0000-0000-0000-000000000003", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "00000000-0000-0000-0000-000000000002", CreatedAt: base.Add(time.Minute)},
		{ID: "00000000-0000-0000-0000-000000000001", CreatedAt: base},
	} {
		if err := repo.Create(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(repo)
	first, err := service.List(context.Background(), 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	second, err := service.List(context.Background(), 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "00000000-0000-0000-0000-000000000001" || second.NextCursor != "" {
		t.Fatalf("unexpected second page: %+v", second)
	}
	if _, err := service.List(context.Background(), 2, "not-a-cursor"); err == nil {
		t.Fatal("invalid cursor was accepted")
	}
}
