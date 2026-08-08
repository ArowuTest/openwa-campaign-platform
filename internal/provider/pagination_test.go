package provider

import (
	"context"
	"testing"
)

func TestProviderCapabilityEventPaginationContinuesByEventID(t *testing.T) {
	store := NewMemoryStore()
	store.items["definition-1"] = Definition{ID: "definition-1"}
	store.events["definition-1"] = []Event{
		{ID: 1, DefinitionID: "definition-1"},
		{ID: 2, DefinitionID: "definition-1"},
		{ID: 3, DefinitionID: "definition-1"},
	}
	service := &Service{Store: store}
	first, err := service.EventsPage(context.Background(), "definition-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != 3 || first.Items[1].ID != 2 || first.NextCursor == "" {
		t.Fatalf("unexpected provider event page: %+v", first)
	}
	second, err := service.EventsPage(context.Background(), "definition-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != 1 || second.NextCursor != "" {
		t.Fatalf("unexpected provider continuation: %+v", second)
	}
	if _, err := service.EventsPage(context.Background(), "definition-1", 2, "not-a-cursor"); err == nil {
		t.Fatal("invalid provider-event cursor accepted")
	}
}
