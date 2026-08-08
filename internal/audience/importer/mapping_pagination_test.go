package importer

import (
	"context"
	"testing"
	"time"
)

func TestMappingEventPaginationHasStableContinuation(t *testing.T) {
	store := NewMemoryMappingStore()
	store.items["mapping-1"] = MappingDefinition{ID: "mapping-1"}
	base := time.Date(2026, 8, 7, 20, 0, 0, 0, time.UTC)
	store.events["mapping-1"] = []MappingEvent{
		{ID: "event-a", MappingID: "mapping-1", OccurredAt: base},
		{ID: "event-b", MappingID: "mapping-1", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", MappingID: "mapping-1", OccurredAt: base.Add(2 * time.Minute)},
	}
	admin := &MappingAdministration{Store: store}
	first, err := admin.EventsPage(context.Background(), "mapping-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" || first.NextCursor == "" {
		t.Fatalf("unexpected first mapping-event page: %+v", first)
	}
	second, err := admin.EventsPage(context.Background(), "mapping-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "event-a" || second.NextCursor != "" {
		t.Fatalf("unexpected second mapping-event page: %+v", second)
	}
	if _, err := admin.EventsPage(context.Background(), "mapping-1", 2, "not-a-cursor"); err == nil {
		t.Fatal("invalid mapping-event cursor was accepted")
	}
}
