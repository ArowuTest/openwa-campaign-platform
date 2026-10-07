package campaign

import (
	"context"
	"testing"
	"time"
)

func TestCampaignFilteredListIsOrganisationAndStatusScoped(t *testing.T) {
	repo := NewMemoryRepository()
	base := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	for _, item := range []Campaign{
		{ID: "00000000-0000-4000-8000-000000000004", OrganisationID: "11111111-1111-4111-8111-111111111111", Status: StatusAudienceBuilding, CreatedAt: base.Add(3 * time.Minute)},
		{ID: "00000000-0000-4000-8000-000000000003", OrganisationID: "11111111-1111-4111-8111-111111111111", Status: StatusAudienceBuilding, CreatedAt: base.Add(2 * time.Minute)},
		{ID: "00000000-0000-4000-8000-000000000002", OrganisationID: "11111111-1111-4111-8111-111111111111", Status: StatusDraft, CreatedAt: base.Add(time.Minute)},
		{ID: "00000000-0000-4000-8000-000000000001", OrganisationID: "22222222-2222-4222-8222-222222222222", Status: StatusAudienceBuilding, CreatedAt: base},
	} {
		if err := repo.Create(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(repo)
	filter := ListFilter{OrganisationID: "11111111-1111-4111-8111-111111111111", Status: StatusAudienceBuilding}
	first, err := service.ListFiltered(context.Background(), filter, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.Items[0].ID != "00000000-0000-4000-8000-000000000004" || first.NextCursor == "" {
		t.Fatalf("unexpected first filtered page: %+v", first)
	}
	second, err := service.ListFiltered(context.Background(), filter, 1, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "00000000-0000-4000-8000-000000000003" || second.NextCursor != "" {
		t.Fatalf("unexpected second filtered page: %+v", second)
	}
	for _, item := range append(append([]Campaign{}, first.Items...), second.Items...) {
		if item.OrganisationID != filter.OrganisationID || item.Status != filter.Status {
			t.Fatalf("filter leaked campaign: %+v", item)
		}
	}
	if _, err := service.ListFiltered(context.Background(), ListFilter{Status: Status("NOT_A_STATUS")}, 10, ""); err == nil {
		t.Fatal("invalid campaign status filter was accepted")
	}
}

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
