package jobs

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
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

type RetryClassPolicy struct {
	BaseDelay time.Duration
	MaxDelay  time.Duration
	Jitter    func(jobID string, attempt int, base time.Duration) time.Duration
}

type RetryPolicy struct {
	BaseDelay time.Duration
	MaxDelay  time.Duration
	Jitter    func(jobID string, attempt int, base time.Duration) time.Duration
	ByCode    map[string]RetryClassPolicy
}

func ParseRetryCategoryPolicy(raw string) (map[string]RetryClassPolicy, error) {
	out := map[string]RetryClassPolicy{}
	for _, entry := range strings.Split(strings.TrimSpace(raw), ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, ",")
		if len(parts) != 4 {
			return nil, fmt.Errorf("invalid retry category policy %q", entry)
		}
		code := canonicalRetryCode(parts[0])
		if code == "" || code == "OUTCOME_UNKNOWN" {
			return nil, fmt.Errorf("invalid retry category code %q", code)
		}
		base, err := time.ParseDuration(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("retry category %s base delay: %w", code, err)
		}
		maxDelay, err := time.ParseDuration(strings.TrimSpace(parts[2]))
		if err != nil {
			return nil, fmt.Errorf("retry category %s max delay: %w", code, err)
		}
		percent, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil || percent < 0 || percent > 100 || base <= 0 || maxDelay < base || maxDelay > time.Hour {
			return nil, fmt.Errorf("invalid retry category policy %q", entry)
		}
		out[code] = RetryClassPolicy{BaseDelay: base, MaxDelay: maxDelay, Jitter: DeterministicJitter(percent)}
	}
	return out, nil
}

func DeterministicJitter(percent int) func(string, int, time.Duration) time.Duration {
	if percent <= 0 {
		return nil
	}
	if percent > 100 {
		percent = 100
	}
	return func(jobID string, attempt int, base time.Duration) time.Duration {
		h := fnv.New64a()
		_, _ = fmt.Fprintf(h, "%s:%d", jobID, attempt)
		span := int64(base) * int64(percent) / 100
		if span <= 0 {
			return 0
		}
		return time.Duration(int64(h.Sum64()%uint64(2*span+1)) - span)
	}
}

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
	RetryPolicy      RetryPolicy
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
	if retry, ok := retryableErrorFrom(handlerErr); ok {
		code := canonicalRetryCode(retry.Code)
		retryable := code != "OUTCOME_UNKNOWN"
		after := retry.RetryAfter
		if retryable {
			if after <= 0 {
				after = r.retryDelay(job, code)
			} else {
				after = r.boundRequestedRetryDelay(code, after)
			}
		}
		opCtx, opCancel := context.WithTimeout(context.Background(), r.OperationTimeout)
		err := r.Repository.Fail(opCtx, job.ID, r.Owner, job.LeaseVersion, now, retryable, after, code, handlerErr.Error())
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

func (r *Runner) retryDelay(job Job, code string) time.Duration {
	base, maxDelay, jitter := r.retryPolicyFor(code)
	attempt := job.AttemptCount
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for i := 1; i < attempt && delay < maxDelay; i++ {
		if delay > maxDelay/2 {
			delay = maxDelay
			break
		}
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	if jitter != nil {
		jitterValue := jitter(job.ID, attempt, delay)
		if jitterValue > maxDelay-delay {
			jitterValue = maxDelay - delay
		}
		if jitterValue < -delay {
			jitterValue = -delay
		}
		delay += jitterValue
	}
	floor := retryDelayFloor(base, maxDelay)
	if delay < floor {
		delay = floor
	}
	return delay
}

func (r *Runner) boundRequestedRetryDelay(code string, requested time.Duration) time.Duration {
	base, maxDelay, _ := r.retryPolicyFor(code)
	floor := retryDelayFloor(base, maxDelay)
	if requested < floor {
		return floor
	}
	if requested > maxDelay {
		return maxDelay
	}
	return requested
}

func (r *Runner) retryPolicyFor(code string) (time.Duration, time.Duration, func(string, int, time.Duration) time.Duration) {
	base, maxDelay, jitter := r.RetryPolicy.BaseDelay, r.RetryPolicy.MaxDelay, r.RetryPolicy.Jitter
	if class, ok := r.RetryPolicy.ByCode[canonicalRetryCode(code)]; ok {
		if class.BaseDelay > 0 {
			base = class.BaseDelay
		}
		if class.MaxDelay > 0 {
			maxDelay = class.MaxDelay
		}
		if class.Jitter != nil {
			jitter = class.Jitter
		}
	}
	if base <= 0 {
		base = time.Second
	}
	if maxDelay <= 0 {
		maxDelay = 128 * time.Second
	}
	if maxDelay < base {
		maxDelay = base
	}
	return base, maxDelay, jitter
}

func retryDelayFloor(base, maxDelay time.Duration) time.Duration {
	floor := base / 10
	if floor < time.Millisecond {
		floor = time.Millisecond
	}
	if floor > maxDelay {
		floor = maxDelay
	}
	if floor <= 0 {
		return time.Nanosecond
	}
	return floor
}

func canonicalRetryCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func retryableErrorFrom(err error) (RetryableError, bool) {
	var value RetryableError
	if errors.As(err, &value) {
		return value, true
	}
	var pointer *RetryableError
	if errors.As(err, &pointer) && pointer != nil {
		return *pointer, true
	}
	return RetryableError{}, false
}

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
