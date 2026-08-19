package execution

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"campaign-platform/internal/campaign"
)

type CampaignLease struct {
	Campaign   campaign.Campaign
	Owner      string
	FenceToken int64
	ExpiresAt  time.Time
}

func (l CampaignLease) Fence() ExecutionLeaseFence {
	return ExecutionLeaseFence{Owner: l.Owner, FenceToken: l.FenceToken}
}

type LeaseRepository interface {
	ClaimDue(context.Context, string, time.Duration, time.Time, int) ([]CampaignLease, error)
	ClaimActive(context.Context, string, time.Duration, time.Time, int) ([]CampaignLease, error)
	Release(context.Context, string, string, int64, time.Time) error
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
	for _, lease := range due {
		c := lease.Campaign
		r.active.Add(1)
		_, _, operationErr := r.Coordinator.StartWithExecutionLease(
			ctx, c.ID, "execution-scheduler", "scheduled start", c.Version, lease.Fence(),
		)
		if operationErr != nil && !errors.Is(operationErr, ErrExecutionLeaseConflict) {
			if eventErr := r.Coordinator.Store.RecordEvent(context.WithoutCancel(ctx), c.ID, "SCHEDULED_START_FAILED", "execution-scheduler", "scheduled start failed", map[string]any{"error": operationErr.Error()}, r.Coordinator.now()); eventErr != nil {
				r.active.Add(-1)
				return fmt.Errorf("record scheduled-start failure for campaign %s: %w", c.ID, eventErr)
			}
		}
		if releaseErr := r.Repository.Release(
			context.WithoutCancel(ctx), c.ID, lease.Owner, lease.FenceToken, r.Coordinator.now(),
		); releaseErr != nil && !errors.Is(releaseErr, ErrExecutionLeaseConflict) {
			r.active.Add(-1)
			return fmt.Errorf("release due campaign %s: %w", c.ID, releaseErr)
		}
		r.active.Add(-1)
	}
	active, err := r.Repository.ClaimActive(ctx, r.Owner, r.Lease, now, r.Batch)
	if err != nil {
		return err
	}
	for _, lease := range active {
		c := lease.Campaign
		r.active.Add(1)
		_, _, operationErr := r.Coordinator.AssessAndCompleteWithExecutionLease(
			ctx, c.ID, "execution-scheduler", c.Version, lease.Fence(),
		)
		if operationErr != nil && !errors.Is(operationErr, ErrExecutionLeaseConflict) {
			if eventErr := r.Coordinator.Store.RecordEvent(context.WithoutCancel(ctx), c.ID, "COMPLETION_ASSESSMENT_FAILED", "execution-scheduler", "completion assessment failed", map[string]any{"error": operationErr.Error()}, r.Coordinator.now()); eventErr != nil {
				r.active.Add(-1)
				return fmt.Errorf("record completion-assessment failure for campaign %s: %w", c.ID, eventErr)
			}
		}
		if releaseErr := r.Repository.Release(
			context.WithoutCancel(ctx), c.ID, lease.Owner, lease.FenceToken, r.Coordinator.now(),
		); releaseErr != nil && !errors.Is(releaseErr, ErrExecutionLeaseConflict) {
			r.active.Add(-1)
			return fmt.Errorf("release active campaign %s: %w", c.ID, releaseErr)
		}
		r.active.Add(-1)
	}
	return nil
}
