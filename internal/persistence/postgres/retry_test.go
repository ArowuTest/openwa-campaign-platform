package postgres

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stateError struct{ state string }

func (e stateError) Error() string    { return e.state }
func (e stateError) SQLState() string { return e.state }

type wrappedStateError struct{ err error }

func (e wrappedStateError) Error() string { return "wrapped: " + e.err.Error() }
func (e wrappedStateError) Unwrap() error { return e.err }

func TestRetryValueRetriesOnlyConcurrencySQLStates(t *testing.T) {
	attempts := 0
	delays := []time.Duration{}
	value, err := RetryValue(context.Background(), RetryPolicy{
		MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond,
		Jitter: func(delay time.Duration) time.Duration { return delay },
		Sleep:  func(_ context.Context, delay time.Duration) error { delays = append(delays, delay); return nil },
	}, func() (string, error) {
		attempts++
		if attempts < 3 {
			return "", wrappedStateError{err: stateError{state: SQLStateSerializationFailure}}
		}
		return "committed", nil
	})
	if err != nil || value != "committed" || attempts != 3 {
		t.Fatalf("unexpected retry result value=%q attempts=%d err=%v", value, attempts, err)
	}
	if len(delays) != 2 || delays[0] != time.Millisecond || delays[1] != 2*time.Millisecond {
		t.Fatalf("unexpected delays: %v", delays)
	}
}

func TestRetryValueDoesNotRetryBusinessFailure(t *testing.T) {
	business := errors.New("consent is expired")
	attempts := 0
	_, err := RetryValue(context.Background(), DefaultRetryPolicy(), func() (int, error) {
		attempts++
		return 0, business
	})
	if !errors.Is(err, business) || attempts != 1 {
		t.Fatalf("business error was retried: attempts=%d err=%v", attempts, err)
	}
}

func TestRetryValueHonoursContextDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	_, err := RetryValue(ctx, DefaultRetryPolicy(), func() (int, error) {
		attempts++
		return 0, stateError{state: SQLStateDeadlockDetected}
	})
	if !errors.Is(err, context.Canceled) || attempts != 0 {
		t.Fatalf("context was not honoured: attempts=%d err=%v", attempts, err)
	}
}
