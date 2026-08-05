package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/jobs"
)

func TestPublisherAppliesQueueBackpressureBeforeEnqueue(t *testing.T) {
	now := time.Now().UTC()
	repo := jobs.NewMemoryRepository()
	service := jobs.NewService(repo)
	_, _, err := service.Enqueue(context.Background(), jobs.EnqueueInput{Type: "DISPATCH_CAMPAIGN_RECIPIENT", DedupKey: "existing", Payload: map[string]string{"campaignRecipientId": "one"}, AvailableAt: now})
	if err != nil {
		t.Fatal(err)
	}
	publisher := &Publisher{Outbox: NewMemoryRepository(), Jobs: service, Clock: func() time.Time { return now }, QueueBackpressureLimit: 1, BackpressureRetryAfter: 9 * time.Second}
	err = publisher.Publish(context.Background(), Record{DedupKey: "new", EventType: "CAMPAIGN_RECIPIENT_AUTHORISED", Payload: []byte(`{"campaignRecipientId":"two"}`), AvailableAt: now})
	var pressure QueueBackpressureError
	if !errors.As(err, &pressure) {
		t.Fatalf("expected backpressure error, got %v", err)
	}
	if pressure.Pending != 1 || pressure.Limit != 1 || pressure.RetryAfter != 9*time.Second {
		t.Fatalf("unexpected pressure: %#v", pressure)
	}
}
