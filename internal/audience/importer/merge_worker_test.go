package importer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type mergeWorkRepo struct {
	mu       sync.Mutex
	items    []MergeWork
	renewed  int
	released int
}

func (r *mergeWorkRepo) ClaimMergeReady(context.Context, string, time.Time, time.Duration, int) ([]MergeWork, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := r.items
	r.items = nil
	return items, nil
}
func (r *mergeWorkRepo) RenewMergeLease(context.Context, MergeWork, time.Time, time.Duration) error {
	r.mu.Lock()
	r.renewed++
	r.mu.Unlock()
	return nil
}
func (r *mergeWorkRepo) ReleaseMergeLease(context.Context, MergeWork, time.Time, error, time.Duration) error {
	r.mu.Lock()
	r.released++
	r.mu.Unlock()
	return nil
}

type mergeRepoFunc func(context.Context, string, time.Time) (MergeResult, error)

func (f mergeRepoFunc) Merge(ctx context.Context, id string, now time.Time) (MergeResult, error) {
	return f(ctx, id, now)
}

func TestMergeWorkerProcessesClaim(t *testing.T) {
	repo := &mergeWorkRepo{items: []MergeWork{{ImportID: "import-1", Lease: ValidationLease{Owner: "worker", Version: 1, ExpiresAt: time.Now().Add(time.Minute)}}}}
	called := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	worker := &MergeWorker{Repository: repo, Merger: &MergeService{Repository: mergeRepoFunc(func(_ context.Context, id string, _ time.Time) (MergeResult, error) {
		called <- id
		cancel()
		return MergeResult{InsertedContacts: 1}, nil
	})}, WorkerID: "worker", PollInterval: time.Millisecond}
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-called:
		if id != "import-1" {
			t.Fatalf("unexpected %s", id)
		}
	default:
		t.Fatal("merge not called")
	}
}
func TestMergeWorkerReleasesFailure(t *testing.T) {
	repo := &mergeWorkRepo{items: []MergeWork{{ImportID: "import-1", Lease: ValidationLease{Owner: "worker", Version: 1, ExpiresAt: time.Now().Add(time.Minute)}}}}
	ctx, cancel := context.WithCancel(context.Background())
	onErr := make(chan error, 1)
	worker := &MergeWorker{Repository: repo, Merger: &MergeService{Repository: mergeRepoFunc(func(context.Context, string, time.Time) (MergeResult, error) {
		cancel()
		return MergeResult{}, errors.New("merge failed")
	})}, WorkerID: "worker", PollInterval: time.Millisecond, OnError: func(_ MergeWork, err error) { onErr <- err }}
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	released := repo.released
	repo.mu.Unlock()
	if released != 1 {
		t.Fatalf("expected release, got %d", released)
	}
	select {
	case <-onErr:
	default:
		t.Fatal("expected error callback")
	}
}
