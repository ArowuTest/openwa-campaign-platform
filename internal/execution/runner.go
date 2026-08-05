package execution

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"campaign-platform/internal/campaign"
)

type LeaseRepository interface {
	ClaimDue(context.Context, string, time.Duration, time.Time, int) ([]campaign.Campaign, error)
	ClaimActive(context.Context, string, time.Duration, time.Time, int) ([]campaign.Campaign, error)
	Release(context.Context, string, string, time.Time) error
}
type Runner struct {
	Repository   LeaseRepository
	Coordinator  *Coordinator
	Owner        string
	Lease        time.Duration
	PollInterval time.Duration
	Batch        int
	active       atomic.Int64
}

func (r *Runner) Active() int64 { return r.active.Load() }
func (r *Runner) Run(ctx context.Context) error {
	if r == nil || r.Repository == nil || r.Coordinator == nil || r.Owner == "" {
		return errors.New("execution runner dependencies are required")
	}
	if r.Lease <= 0 {
		r.Lease = 30 * time.Second
	}
	if r.PollInterval <= 0 {
		r.PollInterval = 2 * time.Second
	}
	if r.Batch <= 0 || r.Batch > 100 {
		r.Batch = 20
	}
	ticker := time.NewTicker(r.PollInterval)
	defer ticker.Stop()
	for {
		if err := r.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (r *Runner) runOnce(ctx context.Context) error {
	now := r.Coordinator.now()
	due, err := r.Repository.ClaimDue(ctx, r.Owner, r.Lease, now, r.Batch)
	if err != nil {
		return err
	}
	for _, c := range due {
		r.active.Add(1)
		_, _, _ = r.Coordinator.Start(ctx, c.ID, "execution-scheduler", "scheduled start", c.Version)
		_ = r.Repository.Release(context.WithoutCancel(ctx), c.ID, r.Owner, r.Coordinator.now())
		r.active.Add(-1)
	}
	active, err := r.Repository.ClaimActive(ctx, r.Owner, r.Lease, now, r.Batch)
	if err != nil {
		return err
	}
	for _, c := range active {
		r.active.Add(1)
		_, _, _ = r.Coordinator.AssessAndComplete(ctx, c.ID, "execution-scheduler", c.Version)
		_ = r.Repository.Release(context.WithoutCancel(ctx), c.ID, r.Owner, r.Coordinator.now())
		r.active.Add(-1)
	}
	return nil
}
