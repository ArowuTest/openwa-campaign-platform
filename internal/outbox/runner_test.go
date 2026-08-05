package outbox

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"campaign-platform/internal/jobs"
)

type panickingJobRepository struct{ jobs.Repository }

func (panickingJobRepository) Enqueue(context.Context, jobs.Job) (jobs.Job, bool, error) {
	panic("job queue defect")
}

func claimedOutboxRecord(t *testing.T, repository *MemoryRepository, now time.Time) Record {
	t.Helper()
	record := Record{
		ID: "outbox-1", DedupKey: "recipient:1", AggregateType: "CAMPAIGN_RECIPIENT",
		AggregateID: "recipient-1", EventType: "CAMPAIGN_RECIPIENT_AUTHORISED",
		Payload: json.RawMessage(`{"campaignRecipientId":"recipient-1"}`),
		Status:  StatusPending, AvailableAt: now, MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
	}
	repository.items[record.ID] = record
	claimed, err := repository.Claim(context.Background(), "publisher-1", now, time.Second, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: count=%d err=%v", len(claimed), err)
	}
	return claimed[0]
}

func TestOutboxRunnerConvertsPublisherPanicToPermanentFailure(t *testing.T) {
	now := time.Now().UTC()
	repository := NewMemoryRepository()
	record := claimedOutboxRecord(t, repository, now)
	publisher := &Publisher{Outbox: repository, Jobs: jobs.NewService(panickingJobRepository{})}
	runner := Runner{
		Repository: repository, Publisher: publisher, Owner: "publisher-1", Lease: time.Second,
		OperationTimeout: 100 * time.Millisecond, ShutdownGrace: 100 * time.Millisecond,
	}
	if err := runner.publishOne(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusFailed || stored.LastErrorCode != "PUBLISH_FAILED" {
		t.Fatalf("publisher panic was not quarantined: %+v", stored)
	}
}
