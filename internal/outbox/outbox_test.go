package outbox

import (
	"campaign-platform/internal/jobs"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func fixture() Record {
	payload, _ := json.Marshal(map[string]any{"campaignRecipientId": "recipient-1"})
	return Record{ID: "outbox-1", DedupKey: "dispatch:key", AggregateType: "CAMPAIGN_RECIPIENT", AggregateID: "recipient-1", EventType: "CAMPAIGN_RECIPIENT_AUTHORISED", Payload: payload, Status: StatusPending, AvailableAt: time.Now().Add(-time.Second), CreatedAt: time.Now().Add(-time.Minute)}
}
func TestConcurrentClaimHasOneOwner(t *testing.T) {
	repo := NewMemoryRepository(fixture())
	var wg sync.WaitGroup
	wins := make(chan int, 2)
	for _, owner := range []string{"a", "b"} {
		owner := owner
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := repo.Claim(context.Background(), owner, time.Now(), time.Minute, 1)
			if err != nil {
				t.Error(err)
			}
			wins <- len(items)
		}()
	}
	wg.Wait()
	close(wins)
	total := 0
	for n := range wins {
		total += n
	}
	if total != 1 {
		t.Fatalf("claimed %d", total)
	}
}
func TestPublisherReplayCreatesOneDurableJob(t *testing.T) {
	out := fixture()
	jobRepo := jobs.NewMemoryRepository()
	publisher := &Publisher{Outbox: NewMemoryRepository(out), Jobs: jobs.NewService(jobRepo)}
	if err := publisher.Publish(context.Background(), out); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(context.Background(), out); err != nil {
		t.Fatal(err)
	}
	count, err := jobRepo.PendingCount(context.Background(), []string{"DISPATCH_CAMPAIGN_RECIPIENT"}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one job, got %d", count)
	}
}

func TestStaleLeaseCannotPublishAfterSameOwnerReclaims(t *testing.T) {
	repo := NewMemoryRepository(fixture())
	now := time.Now().UTC()
	first, err := repo.Claim(context.Background(), "publisher", now, time.Second, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: %v len=%d", err, len(first))
	}
	second, err := repo.Claim(context.Background(), "publisher", now.Add(2*time.Second), time.Minute, 1)
	if err != nil || len(second) != 1 {
		t.Fatalf("second claim: %v len=%d", err, len(second))
	}
	if second[0].LeaseVersion <= first[0].LeaseVersion {
		t.Fatalf("lease version did not advance: first=%d second=%d", first[0].LeaseVersion, second[0].LeaseVersion)
	}
	if err := repo.Complete(context.Background(), first[0].ID, "publisher", first[0].LeaseVersion, now.Add(3*time.Second)); err != ErrLeaseConflict {
		t.Fatalf("stale publisher completed record: %v", err)
	}
	if err := repo.Complete(context.Background(), second[0].ID, "publisher", second[0].LeaseVersion, now.Add(3*time.Second)); err != nil {
		t.Fatalf("current publisher failed: %v", err)
	}
}
