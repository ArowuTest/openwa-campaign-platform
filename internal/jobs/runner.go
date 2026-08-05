package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Handler func(context.Context, Job) error

type RetryableError struct {
	Code       string
	Err        error
	RetryAfter time.Duration
}

func (e RetryableError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Err.Error()
}
func (e RetryableError) Unwrap() error { return e.Err }

type Runner struct {
	Repository       Repository
	Owner            string
	Types            []string
	Concurrency      int
	ClaimBatch       int
	Lease            time.Duration
	PollInterval     time.Duration
	OperationTimeout time.Duration
	ShutdownGrace    time.Duration
	Handler          Handler
	active           atomic.Int64
}

func (r *Runner) Run(ctx context.Context) error {
	if r.Repository == nil || r.Handler == nil || r.Owner == "" {
		return errors.New("runner repository, handler and owner are required")
	}
	if r.Concurrency <= 0 {
		r.Concurrency = 4
	}
	if r.ClaimBatch <= 0 || r.ClaimBatch > r.Concurrency {
		r.ClaimBatch = r.Concurrency
	}
	if r.Lease <= 0 {
		r.Lease = 2 * time.Minute
	}
	if r.PollInterval <= 0 {
		r.PollInterval = 500 * time.Millisecond
	}
	if r.OperationTimeout <= 0 {
		r.OperationTimeout = boundedOperationTimeout(r.Lease)
	}
	if r.ShutdownGrace <= 0 {
		r.ShutdownGrace = r.Lease
		if r.ShutdownGrace > 30*time.Second {
			r.ShutdownGrace = 30 * time.Second
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	semaphore := make(chan struct{}, r.Concurrency)
	fatal := make(chan error, 1)
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
			if !sleep(runCtx, r.PollInterval) {
				continue
			}
			continue
		}
		limit := r.ClaimBatch
		if limit > available {
			limit = available
		}
		claimed, err := r.Repository.Claim(runCtx, r.Owner, time.Now().UTC(), r.Lease, limit, r.Types)
		if err != nil {
			return fmt.Errorf("claim jobs: %w", err)
		}
		if len(claimed) == 0 {
			if !sleep(runCtx, r.PollInterval) {
				continue
			}
			continue
		}
		for _, job := range claimed {
			semaphore <- struct{}{}
			r.active.Add(1)
			workers.Add(1)
			go func(job Job) {
				defer func() { <-semaphore; r.active.Add(-1); workers.Done() }()
				if err := r.runJob(runCtx, job); err != nil {
					select {
					case fatal <- err:
						cancel()
					default:
					}
				}
			}(job)
		}
	}
}

func (r *Runner) runJob(ctx context.Context, job Job) error {
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	handlerDone := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				handlerDone <- fmt.Errorf("job handler panic: %v", recovered)
			}
		}()
		handlerDone <- r.Handler(jobCtx, job)
	}()

	interval := r.Lease / 3
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var handlerErr error
	for {
		select {
		case handlerErr = <-handlerDone:
			cancel()
			goto handled
		case now := <-ticker.C:
			opCtx, opCancel := context.WithTimeout(context.Background(), r.OperationTimeout)
			err := r.Repository.Renew(opCtx, job.ID, r.Owner, job.LeaseVersion, now.UTC(), r.Lease)
			opCancel()
			if err != nil {
				cancel()
				// A correctly implemented handler must observe cancellation. Waiting avoids
				// continuing side effects after ownership has been lost.
				select {
				case <-handlerDone:
				case <-time.After(r.ShutdownGrace):
					return fmt.Errorf("handler did not stop after lease loss for job %s", job.ID)
				}
				return fmt.Errorf("renew job %s lease: %w", job.ID, err)
			}
		case <-ctx.Done():
			cancel()
			select {
			case <-handlerDone:
			case <-time.After(r.ShutdownGrace):
			}
			return nil
		}
	}

handled:
	now := time.Now().UTC()
	if handlerErr == nil {
		opCtx, opCancel := context.WithTimeout(context.Background(), r.OperationTimeout)
		err := r.Repository.Complete(opCtx, job.ID, r.Owner, job.LeaseVersion, now)
		opCancel()
		if err != nil {
			return fmt.Errorf("complete job %s: %w", job.ID, err)
		}
		return nil
	}
	var retry RetryableError
	if errors.As(handlerErr, &retry) {
		after := retry.RetryAfter
		if after <= 0 {
			after = backoff(job.AttemptCount)
		}
		opCtx, opCancel := context.WithTimeout(context.Background(), r.OperationTimeout)
		err := r.Repository.Fail(opCtx, job.ID, r.Owner, job.LeaseVersion, now, true, after, retry.Code, handlerErr.Error())
		opCancel()
		if err != nil {
			return fmt.Errorf("record retryable job %s failure: %w", job.ID, err)
		}
		return nil
	}
	opCtx, opCancel := context.WithTimeout(context.Background(), r.OperationTimeout)
	err := r.Repository.Fail(opCtx, job.ID, r.Owner, job.LeaseVersion, now, false, 0, "PERMANENT_FAILURE", handlerErr.Error())
	opCancel()
	if err != nil {
		return fmt.Errorf("record permanent job %s failure: %w", job.ID, err)
	}
	return nil
}

func (r *Runner) Active() int64 { return r.active.Load() }
func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}
func sleep(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func boundedOperationTimeout(lease time.Duration) time.Duration {
	timeout := lease / 2
	if timeout < 100*time.Millisecond {
		timeout = 100 * time.Millisecond
	}
	if timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	return timeout
}
