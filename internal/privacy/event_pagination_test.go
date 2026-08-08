package privacy

import (
	"context"
	"testing"
	"time"
)

func TestPrivacyCaseEventPaginationContinuesNewestFirst(t *testing.T) {
	store := NewMemoryRepository()
	store.cases["case-1"] = Case{ID: "case-1"}
	base := time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC)
	store.events["case-1"] = []Event{
		{ID: "event-a", CaseID: "case-1", OccurredAt: base},
		{ID: "event-b", CaseID: "case-1", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", CaseID: "case-1", OccurredAt: base.Add(2 * time.Minute)},
	}
	service := &Service{Repository: store}
	first, err := service.EventsPage(context.Background(), "case-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" || first.NextCursor == "" {
		t.Fatalf("unexpected privacy-case event page: %+v", first)
	}
	second, err := service.EventsPage(context.Background(), "case-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "event-a" || second.NextCursor != "" {
		t.Fatalf("unexpected continuation: %+v", second)
	}
}

func TestPrivacyLegalHoldEventPaginationContinuesNewestFirst(t *testing.T) {
	store := NewMemoryRepository()
	store.holds["hold-1"] = LegalHold{ID: "hold-1"}
	base := time.Date(2026, 8, 8, 2, 0, 0, 0, time.UTC)
	store.holdEvents["hold-1"] = []LegalHoldEvent{
		{ID: "event-a", LegalHoldID: "hold-1", OccurredAt: base},
		{ID: "event-b", LegalHoldID: "hold-1", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", LegalHoldID: "hold-1", OccurredAt: base.Add(2 * time.Minute)},
	}
	service := &Service{Repository: store}
	first, err := service.LegalHoldEventsPage(context.Background(), "hold-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" || first.NextCursor == "" {
		t.Fatalf("unexpected legal-hold event page: %+v", first)
	}
	second, err := service.LegalHoldEventsPage(context.Background(), "hold-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "event-a" || second.NextCursor != "" {
		t.Fatalf("unexpected legal-hold continuation: %+v", second)
	}
}
