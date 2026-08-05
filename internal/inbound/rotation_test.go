package inbound

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type rotationMemory struct {
	mu      sync.Mutex
	run     RotationRun
	batches int
	fail    bool
}

func (m *rotationMemory) Request(_ context.Context, r RotationRun) (RotationRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.run = r
	return r, nil
}
func (m *rotationMemory) GetRun(context.Context, string) (RotationRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.run, nil
}
func (m *rotationMemory) ListRuns(context.Context, int) ([]RotationRun, error) {
	return []RotationRun{m.run}, nil
}
func (m *rotationMemory) ClaimRun(_ context.Context, owner string, now time.Time, lease time.Duration) (RotationRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.run.Status == RotationCompleted {
		return RotationRun{}, ErrNotFound
	}
	m.run.Status = RotationRunning
	m.run.LeaseOwner = owner
	m.run.LeaseVersion++
	m.run.LeaseExpiresAt = now.Add(lease)
	return m.run, nil
}
func (m *rotationMemory) RenewRun(context.Context, RotationRun, time.Time, time.Duration) error {
	return nil
}
func (m *rotationMemory) ProcessRunBatch(_ context.Context, r RotationRun, _ int, now time.Time) (RotationRun, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return r, false, errors.New("decrypt failed")
	}
	m.batches++
	m.run.ProcessedCount += 2
	if m.batches >= 2 {
		m.run.Status = RotationCompleted
		m.run.CompletedAt = &now
		return m.run, true, nil
	}
	return m.run, false, nil
}
func (m *rotationMemory) FailRun(_ context.Context, _ RotationRun, _ time.Time, _ error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.run.Status = RotationFailed
	m.run.FailedCount++
	return nil
}

func TestRotationServiceCreatesPendingRun(t *testing.T) {
	repo := &rotationMemory{}
	svc := &RotationService{Repository: repo, ActiveKeyVersion: "v2", Clock: func() time.Time { return time.Unix(10, 0) }}
	run, err := svc.Request(context.Background(), "actor-1")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RotationPending || run.TargetKeyVersion != "v2" || run.RequestedBy != "actor-1" {
		t.Fatalf("unexpected run: %+v", run)
	}
}
func TestRotationWorkerCompletesAcrossBatches(t *testing.T) {
	repo := &rotationMemory{run: RotationRun{ID: "run-1", Status: RotationPending}}
	ctx, cancel := context.WithCancel(context.Background())
	worker := &RotationWorker{Repository: repo, Owner: "worker-1", BatchSize: 2, PollInterval: time.Millisecond}
	go func() {
		for {
			repo.mu.Lock()
			done := repo.run.Status == RotationCompleted
			repo.mu.Unlock()
			if done {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.run.ProcessedCount != 4 || repo.batches != 2 {
		t.Fatalf("unexpected completion: %+v batches=%d", repo.run, repo.batches)
	}
}
func TestRotationWorkerPersistsFailure(t *testing.T) {
	repo := &rotationMemory{run: RotationRun{ID: "run-1", Status: RotationPending}, fail: true}
	ctx, cancel := context.WithCancel(context.Background())
	worker := &RotationWorker{Repository: repo, Owner: "worker-1", PollInterval: time.Millisecond, FailureBackoff: time.Millisecond, OnError: func(RotationRun, error) { cancel() }}
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.run.Status != RotationFailed || repo.run.FailedCount != 1 {
		t.Fatalf("failure not persisted: %+v", repo.run)
	}
}
