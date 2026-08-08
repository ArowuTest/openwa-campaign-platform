package message

import (
	"context"
	"errors"
	"testing"
)

func TestMessageVersionPageContinuesNewestFirst(t *testing.T) {
	repo := NewMemoryRepository()
	repo.items["message-1"] = Version{ID: "message-1", CampaignID: "campaign-a", Version: 1}
	repo.items["message-2"] = Version{ID: "message-2", CampaignID: "campaign-a", Version: 2}
	repo.items["message-3"] = Version{ID: "message-3", CampaignID: "campaign-a", Version: 3}
	repo.byCampaign["campaign-a"] = []string{"message-1", "message-2", "message-3"}
	service := NewService(repo)
	first, err := service.ListByCampaignPage(context.Background(), "campaign-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].Version != 3 || first.Items[1].Version != 2 {
		t.Fatalf("unexpected first message-version page: %#v", first)
	}
	second, err := service.ListByCampaignPage(context.Background(), "campaign-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].Version != 1 {
		t.Fatalf("unexpected second message-version page: %#v", second)
	}
	if _, err := service.ListByCampaignPage(context.Background(), "campaign-a", 2, "invalid"); !errors.Is(err, ErrInvalidMessageVersionCursor) {
		t.Fatalf("invalid message-version cursor accepted: %v", err)
	}
}
