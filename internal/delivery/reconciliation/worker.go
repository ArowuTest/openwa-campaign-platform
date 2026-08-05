package reconciliation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Worker struct {
	Repository      Repository
	Calculator      Calculator
	Owner           string
	Concurrency     int
	ClaimBatch      int
	Lease           time.Duration
	PollInterval    time.Duration
	MatchInterval   time.Duration
	DriftInterval   time.Duration
	FailureInterval time.Duration
	ShutdownGrace   time.Duration
	OnDrift         func(Observation)
	OnError         func(Work, error)
	active          atomic.Int64
}

func (w *Worker) Run(ctx context.Context) error {
	if w == nil || w.Repository == nil || w.Calculator == nil || w.Owner == "" {
		return errors.New("metric reconciliation worker dependencies are required")
	}
	if w.Concurrency <= 0 {
		w.Concurrency = 2
	}
	if w.ClaimBatch <= 0 || w.ClaimBatch > w.Concurrency {
		w.ClaimBatch = w.Concurrency
	}
	if w.Lease <= 0 {
		w.Lease = time.Minute
	}
	if w.PollInterval <= 0 {
		w.PollInterval = 5 * time.Second
	}
	if w.MatchInterval <= 0 {
		w.MatchInterval = time.Minute
	}
	if w.DriftInterval <= 0 {
		w.DriftInterval = 10 * time.Second
	}
	if w.FailureInterval <= 0 {
		w.FailureInterval = time.Minute
	}
	if w.ShutdownGrace <= 0 {
		w.ShutdownGrace = 30 * time.Second
	}
	semaphore := make(chan struct{}, w.Concurrency)
	var group sync.WaitGroup
	defer group.Wait()
	for {
		if ctx.Err() != nil {
			return nil
		}
		available := w.Concurrency - int(w.active.Load())
		if available <= 0 {
			if !wait(ctx, w.PollInterval) {
				return nil
			}
			continue
		}
		limit := w.ClaimBatch
		if limit > available {
			limit = available
		}
		items, err := w.Repository.Claim(ctx, w.Owner, time.Now().UTC(), w.Lease, limit)
		if err != nil {
			return fmt.Errorf("claim metric reconciliation: %w", err)
		}
		if len(items) == 0 {
			if !wait(ctx, w.PollInterval) {
				return nil
			}
			continue
		}
		for _, item := range items {
			semaphore <- struct{}{}
			w.active.Add(1)
			group.Add(1)
			go func(work Work) { defer func() { <-semaphore; w.active.Add(-1); group.Done() }(); w.process(ctx, work) }(item)
		}
	}
}

type calculationOutcome struct {
	obs Observation
	err error
}

func (w *Worker) process(ctx context.Context, work Work) {
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan calculationOutcome, 1)
	go func() {
		obs, err := w.Calculator.Observe(processCtx, work.CampaignID, time.Now().UTC())
		result <- calculationOutcome{obs: obs, err: err}
	}()
	interval := w.Lease / 3
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case outcome := <-result:
			w.persistOutcome(work, outcome)
			return
		case now := <-ticker.C:
			if err := w.Repository.Renew(context.Background(), work, now.UTC(), w.Lease); err != nil {
				cancel()
				if w.OnError != nil {
					w.OnError(work, err)
				}
				w.waitForCalculator(result)
				return
			}
		case <-ctx.Done():
			// Cancellation and a calculator result can become ready together. Drain the
			// completed result and persist a substantive outcome before exiting, so a
			// query failure is not lost merely because it also triggered shutdown.
			cancel()
			if outcome, ok := w.waitForCalculator(result); ok {
				if outcome.err == nil || (!errors.Is(outcome.err, context.Canceled) && !errors.Is(outcome.err, context.DeadlineExceeded)) {
					w.persistOutcome(work, outcome)
				}
			}
			return
		}
	}
}

func (w *Worker) persistOutcome(work Work, outcome calculationOutcome) {
	now := time.Now().UTC()
	if outcome.err != nil {
		if err := w.Repository.Fail(context.Background(), work, now, outcome.err, w.FailureInterval); err != nil {
			if w.OnError != nil {
				w.OnError(work, err)
			}
			return
		}
		if w.OnError != nil {
			w.OnError(work, outcome.err)
		}
		return
	}
	next := w.MatchInterval
	if !outcome.obs.Matched {
		next = w.DriftInterval
		if w.OnDrift != nil {
			w.OnDrift(outcome.obs)
		}
	}
	if err := w.Repository.Record(context.Background(), work, outcome.obs, now, next); err != nil && w.OnError != nil {
		w.OnError(work, err)
	}
}

func (w *Worker) waitForCalculator(result <-chan calculationOutcome) (calculationOutcome, bool) {
	timer := time.NewTimer(w.ShutdownGrace)
	defer timer.Stop()
	select {
	case outcome := <-result:
		return outcome, true
	case <-timer.C:
		return calculationOutcome{}, false
	}
}

func (w *Worker) Active() int64 {
	if w == nil {
		return 0
	}
	return w.active.Load()
}
func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
