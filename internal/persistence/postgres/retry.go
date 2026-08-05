package postgres

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"
)

const (
	SQLStateSerializationFailure = "40001"
	SQLStateDeadlockDetected     = "40P01"
)

type sqlStateCarrier interface {
	SQLState() string
}

// RetryPolicy controls bounded retries for transactions rejected by PostgreSQL
// concurrency control. It must never be used for arbitrary business failures.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Sleep       func(context.Context, time.Duration) error
	Jitter      func(time.Duration) time.Duration
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 4, BaseDelay: 20 * time.Millisecond, MaxDelay: 500 * time.Millisecond}
}

func RetryValue[T any](ctx context.Context, policy RetryPolicy, operation func() (T, error)) (T, error) {
	var zero T
	if operation == nil {
		return zero, errors.New("transaction operation is required")
	}
	policy = normalisePolicy(policy)
	var lastErr error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		value, err := operation()
		if err == nil {
			return value, nil
		}
		lastErr = err
		if !IsRetryableTransactionError(err) || attempt == policy.MaxAttempts {
			return zero, err
		}
		delay := policy.BaseDelay << (attempt - 1)
		if delay > policy.MaxDelay {
			delay = policy.MaxDelay
		}
		delay = policy.Jitter(delay)
		if sleepErr := policy.Sleep(ctx, delay); sleepErr != nil {
			return zero, sleepErr
		}
	}
	return zero, fmt.Errorf("transaction retry exhausted: %w", lastErr)
}

func IsRetryableTransactionError(err error) bool {
	var carrier sqlStateCarrier
	if !errors.As(err, &carrier) {
		return false
	}
	switch carrier.SQLState() {
	case SQLStateSerializationFailure, SQLStateDeadlockDetected:
		return true
	default:
		return false
	}
}

func normalisePolicy(policy RetryPolicy) RetryPolicy {
	if policy.MaxAttempts <= 0 || policy.MaxAttempts > 10 {
		policy.MaxAttempts = 4
	}
	if policy.BaseDelay <= 0 {
		policy.BaseDelay = 20 * time.Millisecond
	}
	if policy.MaxDelay <= 0 || policy.MaxDelay < policy.BaseDelay {
		policy.MaxDelay = 500 * time.Millisecond
	}
	if policy.Sleep == nil {
		policy.Sleep = sleepContext
	}
	if policy.Jitter == nil {
		policy.Jitter = addJitter
	}
	return policy
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// addJitter adds up to 25% cryptographically sourced jitter. Failure to obtain
// entropy is safe: the deterministic delay is retained.
func addJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	maximum := int64(delay / 4)
	if maximum <= 0 {
		return delay
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(maximum+1))
	if err != nil {
		return delay
	}
	return delay + time.Duration(value.Int64())
}
