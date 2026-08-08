package contactlife

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLifecycleEventPagesContinueNewestFirst(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository(Record{ContactID: "contact-a", Version: 1})
	repo.events["contact-a"] = []Event{
		{ID: "event-a", ContactID: "contact-a", OccurredAt: base},
		{ID: "event-b", ContactID: "contact-a", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", ContactID: "contact-a", OccurredAt: base.Add(2 * time.Minute)},
	}
	service := &Service{Repository: repo}
	first, err := service.EventsPage(context.Background(), "contact-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second, err := service.EventsPage(context.Background(), "contact-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "event-a" {
		t.Fatalf("unexpected second page: %#v", second)
	}
	if _, err := service.EventsPage(context.Background(), "contact-a", 2, "invalid"); !errors.Is(err, ErrInvalidLifecycleEventCursor) {
		t.Fatalf("expected invalid cursor rejection, got %v", err)
	}
}
