package campaign

import (
	"context"
	"errors"
	"testing"
)

func TestMaterialChangePageContinuesBySequence(t *testing.T) {
	repo := NewMemoryRepository()
	repo.items["campaign-a"] = Campaign{ID: "campaign-a"}
	repo.events["campaign-a"] = []MaterialChangeEvent{
		{ID: "change-1", CampaignID: "campaign-a", Sequence: 1},
		{ID: "change-2", CampaignID: "campaign-a", Sequence: 2},
		{ID: "change-3", CampaignID: "campaign-a", Sequence: 3},
	}
	service := NewService(repo)
	first, err := service.ListMaterialChangesPage(context.Background(), "campaign-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].Sequence != 1 || first.Items[1].Sequence != 2 {
		t.Fatalf("unexpected first material-change page: %#v", first)
	}
	second, err := service.ListMaterialChangesPage(context.Background(), "campaign-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].Sequence != 3 {
		t.Fatalf("unexpected second material-change page: %#v", second)
	}
	if _, err := service.ListMaterialChangesPage(context.Background(), "campaign-a", 2, "invalid"); !errors.Is(err, ErrInvalidMaterialChangeCursor) {
		t.Fatalf("invalid material-change cursor accepted: %v", err)
	}
}
