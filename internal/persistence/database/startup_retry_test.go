package database

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakePinger struct {
	failures int32
	calls    atomic.Int32
}

func (p *fakePinger) PingContext(context.Context) error {
	call := p.calls.Add(1)
	if call <= p.failures {
		return errors.New("database not ready")
	}
	return nil
}
func TestPingUntilReadyRetriesTransientStartupFailures(t *testing.T) {
	pinger := &fakePinger{failures: 2}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := pingUntilReady(ctx, pinger, 50*time.Millisecond, time.Second, time.Millisecond); err != nil {
		t.Fatalf("expected startup retry to recover, got %v", err)
	}
	if got := pinger.calls.Load(); got != 3 {
		t.Fatalf("expected 3 ping attempts, got %d", got)
	}
}

func TestPingUntilReadyFailsAfterStartupDeadline(t *testing.T) {
	pinger := &fakePinger{failures: 100}
	ctx := context.Background()
	started := time.Now()
	err := pingUntilReady(ctx, pinger, 5*time.Millisecond, 25*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("expected startup deadline error")
	}
	if time.Since(started) > 250*time.Millisecond {
		t.Fatalf("startup retry exceeded bounded deadline: %s", time.Since(started))
	}
	if pinger.calls.Load() < 2 {
		t.Fatalf("expected retries before deadline, got %d attempts", pinger.calls.Load())
	}
}
