package reconciliation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
)

type memoryRepository struct {
	mu        sync.Mutex
	available []Work
	recorded  []Observation
	failed    int
	renewed   int
}

func (r *memoryRepository) Claim(_ context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]Work, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.available) == 0 {
		return nil, nil
	}
	if limit > len(r.available) {
		limit = len(r.available)
	}
	out := append([]Work(nil), r.available[:limit]...)
	r.available = r.available[limit:]
	for i := range out {
		out[i].Lease = Lease{Owner: owner, Version: 1, ExpiresAt: now.Add(lease)}
	}
	return out, nil
}
func (r *memoryRepository) Renew(context.Context, Work, time.Time, time.Duration) error {
	r.mu.Lock()
	r.renewed++
	r.mu.Unlock()
	return nil
}
func (r *memoryRepository) Record(_ context.Context, _ Work, o Observation, _ time.Time, _ time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = append(r.recorded, o)
	return nil
}
func (r *memoryRepository) Fail(context.Context, Work, time.Time, error, time.Duration) error {
	r.mu.Lock()
	r.failed++
	r.mu.Unlock()
	return nil
}

type calculatorFunc func(context.Context, string, time.Time) (Observation, error)

func (f calculatorFunc) Observe(c context.Context, id string, t time.Time) (Observation, error) {
	return f(c, id, t)
}

func TestWorkerRecordsDriftWithoutOverwritingMetrics(t *testing.T) {
	repo := &memoryRepository{available: []Work{{CampaignID: "campaign-1"}}}
	drift := make(chan Observation, 1)
	ctx, cancel := context.WithCancel(context.Background())
	worker := &Worker{Repository: repo, Calculator: calculatorFunc(func(context.Context, string, time.Time) (Observation, error) {
		return Observation{CampaignID: "campaign-1", Canonical: delivery.Metrics{DeliveredTotal: 2}, Stored: delivery.Metrics{DeliveredTotal: 1}, Matched: false}, nil
	}), Owner: "metrics-1", Concurrency: 1, ClaimBatch: 1, PollInterval: time.Millisecond, OnDrift: func(o Observation) { drift <- o; cancel() }}
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case o := <-drift:
		if o.Matched {
			t.Fatal("expected drift")
		}
	default:
		t.Fatal("drift callback missing")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.recorded) != 1 || repo.recorded[0].Stored.DeliveredTotal != 1 {
		t.Fatalf("unexpected record: %+v", repo.recorded)
	}
}

func TestWorkerPersistsCalculatorFailure(t *testing.T) {
	repo := &memoryRepository{available: []Work{{CampaignID: "campaign-1"}}}
	ctx, cancel := context.WithCancel(context.Background())
	worker := &Worker{Repository: repo, Calculator: calculatorFunc(func(context.Context, string, time.Time) (Observation, error) {
		cancel()
		return Observation{}, errors.New("query failed")
	}), Owner: "metrics-1", Concurrency: 1, ClaimBatch: 1, PollInterval: time.Millisecond}
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.failed != 1 {
		t.Fatalf("expected one failure, got %d", repo.failed)
	}
}
