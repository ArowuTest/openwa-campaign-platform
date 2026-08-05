package orchestration

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
)

func mustRecipients(t *testing.T, store EvidenceStore, campaignID string) []delivery.Recipient {
	t.Helper()
	items, err := store.ListRecipients(context.Background(), campaignID, "", "", 5_000)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func mustOutbox(t *testing.T, store EvidenceStore) []Outbox {
	t.Helper()
	items, err := store.ListOutbox(context.Background(), time.Time{}, "", 5_000)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestEvidenceCursorsRequireCompleteKeysets(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.ListRecipients(context.Background(), "campaign", "contact", "", 10); err == nil {
		t.Fatal("expected incomplete recipient cursor to fail")
	}
	if _, err := store.ListRecipients(context.Background(), "campaign", "", "recipient", 10); err == nil {
		t.Fatal("expected orphan recipient cursor id to fail")
	}
	if _, err := store.ListOutbox(context.Background(), time.Now().UTC(), "", 10); err == nil {
		t.Fatal("expected incomplete outbox cursor to fail")
	}
	if _, err := store.ListOutbox(context.Background(), time.Time{}, "outbox", 10); err == nil {
		t.Fatal("expected orphan outbox cursor id to fail")
	}
}

func TestEvidencePaginationIsStableAndBounded(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	store.recipients["r-2"] = delivery.Recipient{ID: "r-2", CampaignID: "campaign", ContactID: "contact-b"}
	store.recipients["r-1"] = delivery.Recipient{ID: "r-1", CampaignID: "campaign", ContactID: "contact-a"}
	store.recipients["r-3"] = delivery.Recipient{ID: "r-3", CampaignID: "other", ContactID: "contact-c"}
	store.outbox["one"] = Outbox{ID: "o-1", DedupKey: "one", CreatedAt: now}
	store.outbox["two"] = Outbox{ID: "o-2", DedupKey: "two", CreatedAt: now}

	first, err := store.ListRecipients(context.Background(), "campaign", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ID != "r-1" {
		t.Fatalf("unexpected first recipient page: %+v", first)
	}
	second, err := store.ListRecipients(context.Background(), "campaign", first[0].ContactID, first[0].ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].ID != "r-2" {
		t.Fatalf("unexpected second recipient page: %+v", second)
	}

	outboxFirst, err := store.ListOutbox(context.Background(), time.Time{}, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(outboxFirst) != 1 || outboxFirst[0].ID != "o-1" {
		t.Fatalf("unexpected first outbox page: %+v", outboxFirst)
	}
	outboxSecond, err := store.ListOutbox(context.Background(), outboxFirst[0].CreatedAt, outboxFirst[0].ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(outboxSecond) != 1 || outboxSecond[0].ID != "o-2" {
		t.Fatalf("unexpected second outbox page: %+v", outboxSecond)
	}
}
