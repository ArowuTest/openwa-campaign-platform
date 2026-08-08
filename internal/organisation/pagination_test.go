package organisation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOrganisationEventPageContinuesByVersion(t *testing.T) {
	repo := NewMemoryRepository()
	repo.items["org-a"] = Organisation{ID: "org-a", Version: 4}
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo.events["org-a"] = []Event{
		{ID: "event-2", OrganisationID: "org-a", Version: 2, OccurredAt: base},
		{ID: "event-3", OrganisationID: "org-a", Version: 3, OccurredAt: base.Add(time.Minute)},
		{ID: "event-4", OrganisationID: "org-a", Version: 4, OccurredAt: base.Add(2 * time.Minute)},
	}
	service := NewService(repo)
	first, err := service.ListEventsPage(context.Background(), "org-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].Version != 2 || first.Items[1].Version != 3 {
		t.Fatalf("unexpected first organisation-event page: %#v", first)
	}
	second, err := service.ListEventsPage(context.Background(), "org-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].Version != 4 {
		t.Fatalf("unexpected second organisation-event page: %#v", second)
	}
	if _, err := service.ListEventsPage(context.Background(), "org-a", 2, "invalid"); !errors.Is(err, ErrInvalidEventCursor) {
		t.Fatalf("invalid organisation-event cursor accepted: %v", err)
	}
}
