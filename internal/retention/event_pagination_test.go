package retention

import (
	"context"
	"testing"
	"time"
)

func TestRetentionPolicyEventPaginationContinuesNewestFirst(t *testing.T) {
	store := NewMemoryStore()
	store.policies["policy-1"] = Policy{ID: "policy-1"}
	base := time.Date(2026, 8, 8, 3, 0, 0, 0, time.UTC)
	store.events["policy-1"] = []Event{
		{ID: "event-a", PolicyID: "policy-1", OccurredAt: base},
		{ID: "event-b", PolicyID: "policy-1", OccurredAt: base.Add(time.Minute)},
		{ID: "event-c", PolicyID: "policy-1", OccurredAt: base.Add(2 * time.Minute)},
	}
	admin := &Administration{Store: store}
	first, err := admin.EventsPage(context.Background(), "policy-1", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "event-c" || first.Items[1].ID != "event-b" || first.NextCursor == "" {
		t.Fatalf("unexpected retention event page: %+v", first)
	}
	second, err := admin.EventsPage(context.Background(), "policy-1", 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "event-a" || second.NextCursor != "" {
		t.Fatalf("unexpected retention continuation: %+v", second)
	}
	if _, err := admin.EventsPage(context.Background(), "policy-1", 2, "not-a-cursor"); err == nil {
		t.Fatal("invalid retention-event cursor accepted")
	}
}
