package segment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDefinitionPageContinuesInStableUpdatedOrder(t *testing.T) {
	repo := NewMemoryDefinitionRepository()
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo.items["segment-a"] = Definition{ID: "segment-a", OrganisationID: "org-a", UpdatedAt: base}
	repo.items["segment-b"] = Definition{ID: "segment-b", OrganisationID: "org-a", UpdatedAt: base.Add(time.Minute)}
	repo.items["segment-c"] = Definition{ID: "segment-c", OrganisationID: "org-a", UpdatedAt: base.Add(time.Minute)}
	service := &DefinitionService{Repository: repo}
	first, err := service.ListPage(context.Background(), "org-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "segment-b" || first.Items[1].ID != "segment-c" {
		t.Fatalf("unexpected first segment page: %#v", first)
	}
	second, err := service.ListPage(context.Background(), "org-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "segment-a" {
		t.Fatalf("unexpected second segment page: %#v", second)
	}
	if _, err := service.ListPage(context.Background(), "org-a", 2, "invalid"); !errors.Is(err, ErrInvalidDefinitionCursor) {
		t.Fatalf("invalid segment cursor accepted: %v", err)
	}
}
