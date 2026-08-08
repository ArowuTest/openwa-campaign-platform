package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAdministrationEventPageContinuesNewestFirst(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	repo.adminEvents = []AdministrationEvent{
		{ID: "event-a", JobID: "job-a", Action: "RETRY_DEAD_LETTER", OccurredAt: base},
		{ID: "event-b", JobID: "job-a", Action: "RETRY_DEAD_LETTER", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", JobID: "job-a", Action: "RETRY_DEAD_LETTER", OccurredAt: base.Add(2 * time.Minute)},
	}
	service := &AdministrationService{Repository: repo}
	first, err := service.EventsPage(context.Background(), "job-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second, err := service.EventsPage(context.Background(), "job-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "event-a" {
		t.Fatalf("unexpected second page: %#v", second)
	}
	if _, err := service.EventsPage(context.Background(), "job-a", 2, "invalid"); !errors.Is(err, ErrInvalidAdministrationEventCursor) {
		t.Fatalf("invalid administration-event cursor accepted: %v", err)
	}
}
