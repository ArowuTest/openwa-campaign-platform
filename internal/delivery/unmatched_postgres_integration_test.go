package delivery

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLUnmatchedEventStorePersistsAndDeduplicates(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DELIVERY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_DELIVERY_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store := &PostgreSQLUnmatchedEventStore{DB: db}
	now := time.Now().UTC()
	id := "evt-unmatched-" + now.Format("20060102150405.000000000")
	value := UnmatchedEvent{ProviderEventID: id, ProviderMessageID: "provider-missing", ClientReference: "client-missing", EventType: "message.delivered", Payload: []byte(`{"eventId":"` + id + `"}`), OccurredAt: now.Add(-time.Second), ReceivedAt: now}
	if err := store.Enqueue(ctx, value); err != nil {
		t.Fatal(err)
	}
	matched, err := store.Match(ctx, value)
	if err != nil || !matched {
		t.Fatalf("exact unmatched evidence match=%v err=%v", matched, err)
	}
	if err := store.Enqueue(ctx, value); err != nil {
		t.Fatal(err)
	}
	conflict := value
	conflict.EventType = "message.read"
	conflict.Payload = []byte(`{"eventId":"` + id + `","eventType":"message.read"}`)
	if err := store.Enqueue(ctx, conflict); err == nil {
		t.Fatal("expected unmatched event replay conflict")
	}
	if matched, err := store.Match(ctx, conflict); !errors.Is(err, ErrEventDedupMismatch) || matched {
		t.Fatalf("conflicting unmatched evidence match=%v err=%v", matched, err)
	}
	missing := value
	missing.ProviderEventID += "-absent"
	if matched, err := store.Match(ctx, missing); err != nil || matched {
		t.Fatalf("absent unmatched evidence match=%v err=%v", matched, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM unmatched_delivery_events WHERE provider_event_id=$1`, id)
	})
	items, err := store.List(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range items {
		if item.ProviderEventID == id {
			count++
			if item.ProviderMessageID != "provider-missing" || item.EventType != "message.delivered" {
				t.Fatalf("unexpected item %+v", item)
			}
		}
	}
	if count != 1 {
		t.Fatalf("unmatched event occurrences=%d want 1", count)
	}
	if err := store.Resolve(ctx, id, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	items, err = store.List(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ProviderEventID == id {
			t.Fatalf("resolved unmatched event remained pending: %+v", item)
		}
	}
	if err := store.Enqueue(ctx, conflict); err == nil {
		t.Fatal("resolved event identifier reuse with different evidence must remain a conflict")
	}
}
