package consent

import (
	"context"
	"testing"
	"time"
)

func TestPurposeServiceFiltersActiveReviewScopedPurposesAndPaginates(t *testing.T) {
	base := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryPurposeRepository(
		Purpose{ID: "00000000-0000-4000-8000-000000000003", OrganisationID: "org-1", ConsentReviewID: "review-1", Code: "P3", Name: "Three", Channel: "WHATSAPP", WordingVersion: "v1", Active: true, CreatedAt: base.Add(3 * time.Minute), UpdatedAt: base},
		Purpose{ID: "00000000-0000-4000-8000-000000000002", OrganisationID: "org-1", ConsentReviewID: "review-1", Code: "P2", Name: "Two", Channel: "WHATSAPP", WordingVersion: "v1", Active: true, CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base},
		Purpose{ID: "00000000-0000-4000-8000-000000000001", OrganisationID: "org-1", ConsentReviewID: "review-1", Code: "P1", Name: "One", Channel: "WHATSAPP", WordingVersion: "v1", Active: true, CreatedAt: base.Add(time.Minute), UpdatedAt: base},
		Purpose{ID: "00000000-0000-4000-8000-000000000004", OrganisationID: "org-1", ConsentReviewID: "review-1", Code: "OLD", Name: "Inactive", Channel: "WHATSAPP", WordingVersion: "v1", Active: false, CreatedAt: base.Add(4 * time.Minute), UpdatedAt: base},
		Purpose{ID: "00000000-0000-4000-8000-000000000005", OrganisationID: "org-2", ConsentReviewID: "review-2", Code: "OTHER", Name: "Other", Channel: "WHATSAPP", WordingVersion: "v1", Active: true, CreatedAt: base.Add(5 * time.Minute), UpdatedAt: base},
	)
	service := &PurposeService{Repository: repo}
	first, err := service.ListPage(context.Background(), "org-1", "review-1", true, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].Code != "P3" || first.Items[1].Code != "P2" || first.NextCursor == "" {
		t.Fatalf("first page=%+v", first)
	}
	second, err := service.ListPage(context.Background(), "org-1", "review-1", true, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].Code != "P1" || second.NextCursor != "" {
		t.Fatalf("second page=%+v", second)
	}
	all, err := service.ListPage(context.Background(), "org-1", "review-1", false, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 4 {
		t.Fatalf("include inactive count=%d want=4", len(all.Items))
	}
}

func TestPurposeServiceRejectsMalformedCursor(t *testing.T) {
	service := &PurposeService{Repository: NewMemoryPurposeRepository()}
	if _, err := service.ListPage(context.Background(), "", "", true, 10, "%%%"); err != ErrInvalidPurposeListCursor {
		t.Fatalf("expected invalid cursor, got %v", err)
	}
}
