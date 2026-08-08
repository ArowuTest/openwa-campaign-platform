package consent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConsentEventPageContinuesWithoutDuplicates(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	repo := NewMemoryLedgerRepository()
	repo.events = []ConsentEvent{
		{ID: "event-a", ContactID: "contact-a", OccurredAt: base},
		{ID: "event-b", ContactID: "contact-b", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", ContactID: "contact-a", OccurredAt: base.Add(2 * time.Minute)},
	}
	service := NewLedgerService(repo)
	first, err := service.EventsPage(context.Background(), "", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second, err := service.EventsPage(context.Background(), "", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "event-a" {
		t.Fatalf("unexpected second page: %#v", second)
	}
	if _, err := service.EventsPage(context.Background(), "", 2, "invalid"); !errors.Is(err, ErrInvalidConsentEventCursor) {
		t.Fatalf("invalid cursor accepted: %v", err)
	}
}
