package outbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PermanentError marks an outbox record that cannot succeed without changing
// its persisted payload or event type. Such records are quarantined rather
// than retried forever.
type PermanentError struct{ Err error }

func (e PermanentError) Error() string {
	if e.Err == nil {
		return "permanent outbox publication failure"
	}
	return e.Err.Error()
}
func (e PermanentError) Unwrap() error { return e.Err }

type Runner struct {
	Repository       Repository
	Publisher        *Publisher
	Owner            string
	Lease            time.Duration
	Batch            int
	Concurrency      int
	PollInterval     time.Duration
	OperationTimeout time.Duration
	ShutdownGrace    time.Duration
	active           atomic.Int64
}

func (r *Runner) Run(ctx context.Context) error {
	if r == nil || r.Repository == nil || r.Publisher == nil || strings.TrimSpace(r.Owner) == "" {
		return errors.New("outbox runner dependencies are required")
	}
	if r.Lease <= 0 {
		r.Lease = 2 * time.Minute
	}
	if r.Concurrency <= 0 {
		r.Concurrency = 4
	}
	if r.Batch <= 0 || r.Batch > 1000 {
		r.Batch = r.Concurrency
	}
	if r.Batch > r.Concurrency {
		r.Batch = r.Concurrency
	}
	if r.PollInterval <= 0 {
		r.PollInterval = 500 * time.Millisecond
	}
	if r.OperationTimeout <= 0 {
		r.OperationTimeout = boundedOutboxOperationTimeout(r.Lease)
	}
	if r.ShutdownGrace <= 0 {
		r.ShutdownGrace = r.Lease
		if r.ShutdownGrace > 30*time.Second {
			r.ShutdownGrace = 30 * time.Second
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	fatal := make(chan error, 1)
	semaphore := make(chan struct{}, r.Concurrency)
	var workers sync.WaitGroup
	defer workers.Wait()

	for {
		select {
		case <-runCtx.Done():
			if ctx.Err() != nil {
				return nil
			}
			select {
			case err := <-fatal:
				return err
			default:
				return runCtx.Err()
			}
		case err := <-fatal:
			cancel()
			return err
		default:
		}

		available := r.Concurrency - int(r.active.Load())
		if available <= 0 {
			if !wait(runCtx, r.PollInterval) {
				continue
			}
			continue
		}
		limit := r.Batch
		if limit > available {
			limit = available
		}
		items, err := r.Repository.Claim(runCtx, r.Owner, time.Now().UTC(), r.Lease, limit)
		if err != nil {
			return fmt.Errorf("claim outbox: %w", err)
		}
		if len(items) == 0 {
			if !wait(runCtx, r.PollInterval) {
				continue
			}
			continue
		}
		for _, item := range items {
			semaphore <- struct{}{}
			r.active.Add(1)
			workers.Add(1)
			go func(item Record) {
				defer func() { <-semaphore; r.active.Add(-1); workers.Done() }()
				if err := r.publishOne(runCtx, item); err != nil {
					select {
					case fatal <- err:
						cancel()
					default:
					}
				}
			}(item)
		}
	}
}

func (r *Runner) publishOne(ctx context.Context, item Record) error {
	publishCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result <- PermanentError{Err: fmt.Errorf("outbox publisher panic: %v", recovered)}
			}
		}()
		result <- r.Publisher.Publish(publishCtx, item)
	}()

	interval := r.Lease / 3
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var publishErr error
	for {
		select {
		case publishErr = <-result:
			cancel()
			goto handled
		case now := <-ticker.C:
			opCtx, opCancel := context.WithTimeout(context.Background(), r.OperationTimeout)
			err := r.Repository.Renew(opCtx, item.ID, r.Owner, item.LeaseVersion, now.UTC(), r.Lease)
			opCancel()
			if err != nil {
				cancel()
				select {
				case <-result:
				case <-time.After(r.ShutdownGrace):
					return fmt.Errorf("publisher did not stop after lease loss for outbox %s", item.ID)
				}
				return fmt.Errorf("renew outbox %s lease: %w", item.ID, err)
			}
		case <-ctx.Done():
			cancel()
			select {
			case <-result:
			case <-time.After(r.ShutdownGrace):
			}
			return nil
		}
	}

handled:
	now := time.Now().UTC()
	if publishErr == nil {
		opCtx, opCancel := context.WithTimeout(context.Background(), r.OperationTimeout)
		err := r.Repository.Complete(opCtx, item.ID, r.Owner, item.LeaseVersion, now)
		opCancel()
		if err != nil {
			return fmt.Errorf("complete outbox %s: %w", item.ID, err)
		}
		return nil
	}
	var permanent PermanentError
	retryable := !errors.As(publishErr, &permanent)
	after := time.Duration(1<<min(item.AttemptCount, 8)) * time.Second
	opCtx, opCancel := context.WithTimeout(context.Background(), r.OperationTimeout)
	err := r.Repository.Fail(opCtx, item.ID, r.Owner, item.LeaseVersion, now, retryable, after, "PUBLISH_FAILED", safeDetail(publishErr))
	opCancel()
	if err != nil {
		return fmt.Errorf("record outbox %s failure: %w", item.ID, err)
	}
	return nil
}

func (r *Runner) Active() int64 { return r.active.Load() }

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func safeDetail(err error) string {
	if err == nil {
		return ""
	}
	value := strings.TrimSpace(err.Error())
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}
func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func boundedOutboxOperationTimeout(lease time.Duration) time.Duration {
	timeout := lease / 2
	if timeout < 100*time.Millisecond {
		timeout = 100 * time.Millisecond
	}
	if timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	return timeout
}
