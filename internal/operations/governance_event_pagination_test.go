package operations

import (
	"context"
	"testing"
	"time"
)

func TestReportingPrivacyEventPaginationHasStableContinuation(t *testing.T) {
	store := NewMemoryReportingPrivacyStore()
	store.policies["policy-1"] = ReportingPrivacyPolicy{ID: "policy-1"}
	base := time.Date(2026, 8, 7, 21, 0, 0, 0, time.UTC)
	store.events["policy-1"] = []ReportingPrivacyEvent{
		{ID: "event-a", PolicyID: "policy-1", OccurredAt: base},
		{ID: "event-b", PolicyID: "policy-1", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", PolicyID: "policy-1", OccurredAt: base.Add(2 * time.Minute)},
	}
	admin := &ReportingPrivacyAdministration{Store: store}
	first, err := admin.EventsPage(context.Background(), "policy-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" || first.NextCursor == "" {
		t.Fatalf("unexpected reporting-privacy event page: %+v", first)
	}
	second, err := admin.EventsPage(context.Background(), "policy-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "event-a" || second.NextCursor != "" {
		t.Fatalf("unexpected reporting-privacy continuation: %+v", second)
	}
	if _, err := admin.EventsPage(context.Background(), "policy-1", 2, "not-a-cursor"); err == nil {
		t.Fatal("invalid reporting-privacy event cursor was accepted")
	}
}

func TestAlertPolicyEventPaginationHasStableContinuation(t *testing.T) {
	store := NewMemoryAlertStore()
	store.policies["alert-policy-1"] = AlertPolicy{ID: "alert-policy-1"}
	base := time.Date(2026, 8, 7, 22, 0, 0, 0, time.UTC)
	store.policyEvents["alert-policy-1"] = []AlertPolicyEvent{
		{ID: "event-a", PolicyID: "alert-policy-1", OccurredAt: base},
		{ID: "event-b", PolicyID: "alert-policy-1", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", PolicyID: "alert-policy-1", OccurredAt: base.Add(2 * time.Minute)},
	}
	admin := &AlertAdministration{Store: store}
	first, err := admin.PolicyEventsPage(context.Background(), "alert-policy-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" || first.NextCursor == "" {
		t.Fatalf("unexpected alert-policy event page: %+v", first)
	}
	second, err := admin.PolicyEventsPage(context.Background(), "alert-policy-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "event-a" || second.NextCursor != "" {
		t.Fatalf("unexpected alert-policy continuation: %+v", second)
	}
	if _, err := admin.PolicyEventsPage(context.Background(), "alert-policy-1", 2, "not-a-cursor"); err == nil {
		t.Fatal("invalid alert-policy event cursor was accepted")
	}
}
