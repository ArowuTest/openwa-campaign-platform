package operations

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOperationalAlertPageContinuesWithoutDuplicates(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	store := NewMemoryAlertStore()
	store.alerts["alert-a"] = Alert{ID: "alert-a", Status: AlertActive, Severity: SeverityWarning, LastObservedAt: base}
	store.alerts["alert-b"] = Alert{ID: "alert-b", Status: AlertActive, Severity: SeverityWarning, LastObservedAt: base.Add(time.Minute)}
	store.alerts["alert-c"] = Alert{ID: "alert-c", Status: AlertActive, Severity: SeverityCritical, LastObservedAt: base.Add(2 * time.Minute)}
	admin := &AlertAdministration{Store: store}
	first, err := admin.ListAlertsPage(context.Background(), "", "", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "alert-c" || first.Items[1].ID != "alert-b" {
		t.Fatalf("unexpected first alert page: %#v", first)
	}
	second, err := admin.ListAlertsPage(context.Background(), "", "", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "alert-a" {
		t.Fatalf("unexpected second alert page: %#v", second)
	}
	if _, err := admin.ListAlertsPage(context.Background(), "", "", 2, "invalid"); !errors.Is(err, ErrInvalidOperationalAlertCursor) {
		t.Fatalf("invalid alert cursor accepted: %v", err)
	}
}

func TestOperationalNotificationPageContinuesWithoutDuplicates(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	store := NewMemoryAlertStore()
	store.notifications["notification-a"] = Notification{ID: "notification-a", Status: "PENDING", CreatedAt: base}
	store.notifications["notification-b"] = Notification{ID: "notification-b", Status: "PENDING", CreatedAt: base.Add(time.Minute)}
	store.notifications["notification-c"] = Notification{ID: "notification-c", Status: "DELIVERED", CreatedAt: base.Add(2 * time.Minute)}
	admin := &AlertAdministration{Store: store}
	first, err := admin.NotificationsPage(context.Background(), "", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "notification-c" || first.Items[1].ID != "notification-b" {
		t.Fatalf("unexpected first notification page: %#v", first)
	}
	second, err := admin.NotificationsPage(context.Background(), "", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "notification-a" {
		t.Fatalf("unexpected second notification page: %#v", second)
	}
	if _, err := admin.NotificationsPage(context.Background(), "", 2, "invalid"); !errors.Is(err, ErrInvalidOperationalNotificationCursor) {
		t.Fatalf("invalid notification cursor accepted: %v", err)
	}
}

func TestOperationalAlertEventPageContinuesOldestFirst(t *testing.T) {
	base := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	store := NewMemoryAlertStore()
	store.alertEvents["alert-a"] = []AlertEvent{
		{ID: "event-a", AlertID: "alert-a", EventType: "TRIGGERED", OccurredAt: base},
		{ID: "event-b", AlertID: "alert-a", EventType: "OBSERVED", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", AlertID: "alert-a", EventType: "OBSERVED", OccurredAt: base.Add(2 * time.Minute)},
	}
	admin := &AlertAdministration{Store: store}
	first, err := admin.AlertEventsPage(context.Background(), "alert-a", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != "event-a" || first.Items[1].ID != "event-b" {
		t.Fatalf("unexpected first alert-event page: %#v", first)
	}
	second, err := admin.AlertEventsPage(context.Background(), "alert-a", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != "event-c" {
		t.Fatalf("unexpected second alert-event page: %#v", second)
	}
	if _, err := admin.AlertEventsPage(context.Background(), "alert-a", 2, "invalid"); !errors.Is(err, ErrInvalidOperationalAlertEventCursor) {
		t.Fatalf("invalid alert-event cursor accepted: %v", err)
	}
}
