package testmessage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSendPageContinuesNewestFirst(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	repo.sends["send-a"] = Send{ID: "send-a", CampaignID: "campaign-a", CreatedAt: base}
	repo.sends["send-b"] = Send{ID: "send-b", CampaignID: "campaign-a", CreatedAt: base.Add(time.Minute)}
	repo.sends["send-c"] = Send{ID: "send-c", CampaignID: "campaign-a", CreatedAt: base.Add(2 * time.Minute)}
	service := &Service{Repository: repo}
	first, err := service.ListSendsPage(context.Background(), "campaign-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "send-c" || first.Items[1].ID != "send-b" {
		t.Fatalf("unexpected first send page: %#v", first)
	}
	second, err := service.ListSendsPage(context.Background(), "campaign-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "send-a" {
		t.Fatalf("unexpected second send page: %#v", second)
	}
	if _, err := service.ListSendsPage(context.Background(), "campaign-a", 2, "invalid"); !errors.Is(err, ErrInvalidSendCursor) {
		t.Fatalf("invalid test-send cursor accepted: %v", err)
	}
}
