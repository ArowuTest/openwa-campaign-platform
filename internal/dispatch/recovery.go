package dispatch

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

type QueueRepairRepository interface {
	RepairMissing(context.Context, time.Time, int) (int, error)
}

type QueueRepairRunner struct {
	Repository   QueueRepairRepository
	Batch        int
	PollInterval time.Duration
	active       atomic.Int64
}

func (r *QueueRepairRunner) Active() int64 {
	if r == nil {
		return 0
	}
	return r.active.Load()
}
func (r *QueueRepairRunner) Run(ctx context.Context) error {
	if r == nil || r.Repository == nil {
		return errors.New("queue repair repository is required")
	}
	if r.Batch <= 0 || r.Batch > 10000 {
		r.Batch = 1000
	}
	if r.PollInterval <= 0 {
		r.PollInterval = 30 * time.Second
	}
	ticker := time.NewTicker(r.PollInterval)
	defer ticker.Stop()
	for {
		r.active.Add(1)
		_, err := r.Repository.RepairMissing(ctx, time.Now().UTC(), r.Batch)
		r.active.Add(-1)
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("repair dispatch queue: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
