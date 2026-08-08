package organisation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOrganisationPageContinuesNewestFirst(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	repo.items["org-a"] = Organisation{ID: "org-a", CreatedAt: base}
	repo.items["org-b"] = Organisation{ID: "org-b", CreatedAt: base.Add(time.Minute)}
	repo.items["org-c"] = Organisation{ID: "org-c", CreatedAt: base.Add(time.Minute)}
	service := NewService(repo)
	first, err := service.ListPage(context.Background(), 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "org-c" || first.Items[1].ID != "org-b" {
		t.Fatalf("unexpected first organisation page: %#v", first)
	}
	second, err := service.ListPage(context.Background(), 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "org-a" {
		t.Fatalf("unexpected second organisation page: %#v", second)
	}
	if _, err := service.ListPage(context.Background(), 2, "invalid"); !errors.Is(err, ErrInvalidOrganisationCursor) {
		t.Fatalf("invalid organisation cursor accepted: %v", err)
	}
}
