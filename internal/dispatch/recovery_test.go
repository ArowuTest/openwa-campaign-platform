package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"
)

type repairRepo struct {
	calls int
	err   error
}

func (r *repairRepo) RepairMissing(context.Context, time.Time, int) (int, error) {
	r.calls++
	return 2, r.err
}
func TestQueueRepairRunnerValidatesDependencies(t *testing.T) {
	if err := (&QueueRepairRunner{}).Run(context.Background()); err == nil {
		t.Fatal("expected dependency error")
	}
}
func TestQueueRepairRunnerStopsOnRepositoryFailure(t *testing.T) {
	repo := &repairRepo{err: errors.New("db down")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := (&QueueRepairRunner{Repository: repo, PollInterval: time.Millisecond}).Run(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if repo.calls != 1 {
		t.Fatalf("calls=%d", repo.calls)
	}
}
