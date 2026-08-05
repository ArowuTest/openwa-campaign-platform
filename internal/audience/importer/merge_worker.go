package importer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type MergeWork struct {
	ImportID string
	Lease    ValidationLease
}

type MergeWorkRepository interface {
	ClaimMergeReady(context.Context, string, time.Time, time.Duration, int) ([]MergeWork, error)
	RenewMergeLease(context.Context, MergeWork, time.Time, time.Duration) error
	ReleaseMergeLease(context.Context, MergeWork, time.Time, error, time.Duration) error
}

type MergeWorker struct {
	Repository     MergeWorkRepository
	Merger         *MergeService
	WorkerID       string
	Concurrency    int
	ClaimBatch     int
	LeaseDuration  time.Duration
	PollInterval   time.Duration
	FailureBackoff time.Duration
	OnError        func(MergeWork, error)
	active         atomic.Int64
}

func (w *MergeWorker) Run(ctx context.Context) error {
	if w == nil || w.Repository == nil || w.Merger == nil || w.WorkerID == "" {
		return errors.New("merge worker dependencies are required")
	}
	if w.Concurrency <= 0 {
		w.Concurrency = 2
	}
	if w.ClaimBatch <= 0 || w.ClaimBatch > w.Concurrency {
		w.ClaimBatch = w.Concurrency
	}
	if w.LeaseDuration <= 0 {
		w.LeaseDuration = 2 * time.Minute
	}
	if w.PollInterval <= 0 {
		w.PollInterval = time.Second
	}
	if w.FailureBackoff <= 0 {
		w.FailureBackoff = 30 * time.Second
	}
	sem := make(chan struct{}, w.Concurrency)
	var group sync.WaitGroup
	defer group.Wait()
	for {
		if ctx.Err() != nil {
			return nil
		}
		available := w.Concurrency - int(w.active.Load())
		if available <= 0 {
			if !sleepValidation(ctx, w.PollInterval) {
				return nil
			}
			continue
		}
		limit := w.ClaimBatch
		if limit > available {
			limit = available
		}
		items, err := w.Repository.ClaimMergeReady(ctx, w.WorkerID, time.Now().UTC(), w.LeaseDuration, limit)
		if err != nil {
			return fmt.Errorf("claim audience imports for merge: %w", err)
		}
		if len(items) == 0 {
			if !sleepValidation(ctx, w.PollInterval) {
				return nil
			}
			continue
		}
		for _, item := range items {
			sem <- struct{}{}
			w.active.Add(1)
			group.Add(1)
			go func(work MergeWork) { defer func() { <-sem; w.active.Add(-1); group.Done() }(); w.process(ctx, work) }(item)
		}
	}
}

func (w *MergeWorker) process(ctx context.Context, work MergeWork) {
	done := make(chan error, 1)
	go func() { _, err := w.Merger.Merge(ctx, work.ImportID); done <- err }()
	interval := w.LeaseDuration / 3
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				_ = w.Repository.ReleaseMergeLease(context.Background(), work, time.Now().UTC(), err, w.FailureBackoff)
				if w.OnError != nil {
					w.OnError(work, err)
				}
			}
			return
		case now := <-ticker.C:
			if err := w.Repository.RenewMergeLease(context.Background(), work, now.UTC(), w.LeaseDuration); err != nil {
				if w.OnError != nil {
					w.OnError(work, err)
				}
				return
			}
		case <-ctx.Done():
			select {
			case err := <-done:
				if err != nil {
					_ = w.Repository.ReleaseMergeLease(context.Background(), work, time.Now().UTC(), err, w.FailureBackoff)
					if w.OnError != nil {
						w.OnError(work, err)
					}
				}
			case <-time.After(5 * time.Second):
			}
			return
		}
	}
}

func (w *MergeWorker) Active() int64 {
	if w == nil {
		return 0
	}
	return w.active.Load()
}
