package materialisation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMaterialisationPageContinuesNewestFirst(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryMaterialisationRepository()
	repo.jobs["job-a"] = MaterialisationJob{ID: "job-a", CampaignID: "campaign-a", RequestedAt: base}
	repo.jobs["job-b"] = MaterialisationJob{ID: "job-b", CampaignID: "campaign-a", RequestedAt: base.Add(time.Minute)}
	repo.jobs["job-c"] = MaterialisationJob{ID: "job-c", CampaignID: "campaign-a", RequestedAt: base.Add(2 * time.Minute)}
	service := &MaterialisationService{Repository: repo}
	first, err := service.ListPage(context.Background(), "campaign-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "job-c" || first.Items[1].ID != "job-b" {
		t.Fatalf("unexpected first materialisation page: %#v", first)
	}
	second, err := service.ListPage(context.Background(), "campaign-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "job-a" {
		t.Fatalf("unexpected second materialisation page: %#v", second)
	}
	if _, err := service.ListPage(context.Background(), "campaign-a", 2, "invalid"); !errors.Is(err, ErrInvalidMaterialisationCursor) {
		t.Fatalf("invalid materialisation cursor accepted: %v", err)
	}
}
