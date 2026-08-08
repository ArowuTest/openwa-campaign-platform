package platformpolicy

import (
	"context"
	"testing"
	"time"
)

func TestConfigurationEventPaginationContinuesNewestFirst(t *testing.T) {
	store := NewMemoryStore()
	store.configurations["cfg-1"] = Configuration{ID: "cfg-1"}
	base := time.Date(2026, 8, 7, 23, 0, 0, 0, time.UTC)
	store.configurationEvents["cfg-1"] = []Event{
		{ID: "event-a", ObjectID: "cfg-1", OccurredAt: base},
		{ID: "event-b", ObjectID: "cfg-1", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", ObjectID: "cfg-1", OccurredAt: base.Add(2 * time.Minute)},
	}
	admin := &ConfigurationAdministration{Store: store}
	first, err := admin.EventsPage(context.Background(), "cfg-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" || first.NextCursor == "" {
		t.Fatalf("unexpected configuration-event page: %+v", first)
	}
	second, err := admin.EventsPage(context.Background(), "cfg-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "event-a" || second.NextCursor != "" {
		t.Fatalf("unexpected configuration-event continuation: %+v", second)
	}
	if _, err := admin.EventsPage(context.Background(), "cfg-1", 2, "not-a-cursor"); err == nil {
		t.Fatal("invalid configuration-event cursor was accepted")
	}
}

func TestMaintenanceEventPaginationContinuesNewestFirst(t *testing.T) {
	store := NewMemoryStore()
	store.maintenance["maintenance-1"] = MaintenanceWindow{ID: "maintenance-1"}
	base := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	store.maintenanceEvents["maintenance-1"] = []Event{
		{ID: "event-a", ObjectID: "maintenance-1", OccurredAt: base},
		{ID: "event-b", ObjectID: "maintenance-1", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", ObjectID: "maintenance-1", OccurredAt: base.Add(2 * time.Minute)},
	}
	admin := &MaintenanceAdministration{Store: store}
	first, err := admin.EventsPage(context.Background(), "maintenance-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" || first.NextCursor == "" {
		t.Fatalf("unexpected maintenance-event page: %+v", first)
	}
	second, err := admin.EventsPage(context.Background(), "maintenance-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "event-a" || second.NextCursor != "" {
		t.Fatalf("unexpected maintenance-event continuation: %+v", second)
	}
	if _, err := admin.EventsPage(context.Background(), "maintenance-1", 2, "not-a-cursor"); err == nil {
		t.Fatal("invalid maintenance-event cursor was accepted")
	}
}
