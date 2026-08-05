package inbound

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"campaign-platform/internal/shared/id"
)

var ErrRotationLeaseConflict = errors.New("inbound re-encryption lease conflict")

type RotationStatus string

const (
	RotationPending   RotationStatus = "PENDING"
	RotationRunning   RotationStatus = "RUNNING"
	RotationCompleted RotationStatus = "COMPLETED"
	RotationFailed    RotationStatus = "FAILED"
)

type RotationRun struct {
	ID               string         `json:"id"`
	RequestedBy      string         `json:"requestedBy"`
	TargetKeyVersion string         `json:"targetKeyVersion"`
	Status           RotationStatus `json:"status"`
	RequestedAt      time.Time      `json:"requestedAt"`
	StartedAt        *time.Time     `json:"startedAt,omitempty"`
	CompletedAt      *time.Time     `json:"completedAt,omitempty"`
	UpdatedAt        time.Time      `json:"updatedAt"`
	ProcessedCount   int64          `json:"processedCount"`
	FailedCount      int64          `json:"failedCount"`
	LastProcessedID  string         `json:"lastProcessedId,omitempty"`
	FailureReason    string         `json:"failureReason,omitempty"`
	LeaseOwner       string         `json:"-"`
	LeaseVersion     int64          `json:"-"`
	LeaseExpiresAt   time.Time      `json:"-"`
}

type RotationRepository interface {
	Request(context.Context, RotationRun) (RotationRun, error)
	GetRun(context.Context, string) (RotationRun, error)
	ListRuns(context.Context, int) ([]RotationRun, error)
	ClaimRun(context.Context, string, time.Time, time.Duration) (RotationRun, error)
	RenewRun(context.Context, RotationRun, time.Time, time.Duration) error
	ProcessRunBatch(context.Context, RotationRun, int, time.Time) (RotationRun, bool, error)
	FailRun(context.Context, RotationRun, time.Time, error) error
}

type RotationService struct {
	Repository       RotationRepository
	ActiveKeyVersion string
	Clock            func() time.Time
}

func (s *RotationService) Request(ctx context.Context, actor string) (RotationRun, error) {
	if s == nil || s.Repository == nil || strings.TrimSpace(s.ActiveKeyVersion) == "" {
		return RotationRun{}, errors.New("rotation repository and active key version are required")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return RotationRun{}, errors.New("actor is required")
	}
	identifier, err := id.New()
	if err != nil {
		return RotationRun{}, err
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	return s.Repository.Request(ctx, RotationRun{ID: identifier, RequestedBy: actor, TargetKeyVersion: strings.TrimSpace(s.ActiveKeyVersion), Status: RotationPending, RequestedAt: now, UpdatedAt: now})
}
func (s *RotationService) Get(ctx context.Context, id string) (RotationRun, error) {
	return s.Repository.GetRun(ctx, strings.TrimSpace(id))
}
func (s *RotationService) List(ctx context.Context, limit int) ([]RotationRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.Repository.ListRuns(ctx, limit)
}

type RotationWorker struct {
	Repository     RotationRepository
	Owner          string
	BatchSize      int
	Lease          time.Duration
	PollInterval   time.Duration
	FailureBackoff time.Duration
	active         atomic.Int64
	OnError        func(RotationRun, error)
}

func (w *RotationWorker) Active() int64 {
	if w == nil {
		return 0
	}
	return w.active.Load()
}
func (w *RotationWorker) Run(ctx context.Context) error {
	if w == nil || w.Repository == nil || strings.TrimSpace(w.Owner) == "" {
		return errors.New("rotation worker repository and owner are required")
	}
	if w.BatchSize <= 0 || w.BatchSize > 1000 {
		w.BatchSize = 100
	}
	if w.Lease <= 0 {
		w.Lease = time.Minute
	}
	if w.PollInterval <= 0 {
		w.PollInterval = 5 * time.Second
	}
	if w.FailureBackoff <= 0 {
		w.FailureBackoff = time.Minute
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		now := time.Now().UTC()
		run, err := w.Repository.ClaimRun(ctx, w.Owner, now, w.Lease)
		if err != nil {
			if !errors.Is(err, ErrNotFound) && w.OnError != nil {
				w.OnError(RotationRun{}, err)
			}
			timer.Reset(w.PollInterval)
			continue
		}
		w.active.Add(1)
		updated, done, processErr := w.Repository.ProcessRunBatch(ctx, run, w.BatchSize, time.Now().UTC())
		w.active.Add(-1)
		if processErr != nil {
			_ = w.Repository.FailRun(context.WithoutCancel(ctx), run, time.Now().UTC(), processErr)
			if w.OnError != nil {
				w.OnError(run, processErr)
			}
			timer.Reset(w.FailureBackoff)
			continue
		}
		if done {
			timer.Reset(0)
		} else {
			_ = w.Repository.RenewRun(ctx, updated, time.Now().UTC(), w.Lease)
			timer.Reset(0)
		}
	}
}

type MemoryRotationRepository struct {
	runs map[string]RotationRun
}

func NewMemoryRotationRepository() *MemoryRotationRepository {
	return &MemoryRotationRepository{runs: map[string]RotationRun{}}
}
func (m *MemoryRotationRepository) Request(_ context.Context, r RotationRun) (RotationRun, error) {
	if m.runs == nil {
		m.runs = map[string]RotationRun{}
	}
	m.runs[r.ID] = r
	return r, nil
}
func (m *MemoryRotationRepository) GetRun(_ context.Context, id string) (RotationRun, error) {
	r, ok := m.runs[id]
	if !ok {
		return RotationRun{}, ErrNotFound
	}
	return r, nil
}
func (m *MemoryRotationRepository) ListRuns(_ context.Context, limit int) ([]RotationRun, error) {
	out := make([]RotationRun, 0, len(m.runs))
	for _, r := range m.runs {
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
func (m *MemoryRotationRepository) ClaimRun(_ context.Context, owner string, now time.Time, lease time.Duration) (RotationRun, error) {
	for id, r := range m.runs {
		if r.Status == RotationPending || r.Status == RotationFailed {
			r.Status = RotationRunning
			r.LeaseOwner = owner
			r.LeaseVersion++
			r.LeaseExpiresAt = now.Add(lease)
			m.runs[id] = r
			return r, nil
		}
	}
	return RotationRun{}, ErrNotFound
}
func (m *MemoryRotationRepository) RenewRun(_ context.Context, r RotationRun, now time.Time, lease time.Duration) error {
	current, ok := m.runs[r.ID]
	if !ok || current.LeaseVersion != r.LeaseVersion {
		return ErrRotationLeaseConflict
	}
	current.LeaseExpiresAt = now.Add(lease)
	m.runs[r.ID] = current
	return nil
}
func (m *MemoryRotationRepository) ProcessRunBatch(_ context.Context, r RotationRun, _ int, now time.Time) (RotationRun, bool, error) {
	current, ok := m.runs[r.ID]
	if !ok {
		return r, false, ErrNotFound
	}
	current.Status = RotationCompleted
	current.CompletedAt = &now
	current.UpdatedAt = now
	current.LeaseOwner = ""
	m.runs[r.ID] = current
	return current, true, nil
}
func (m *MemoryRotationRepository) FailRun(_ context.Context, r RotationRun, now time.Time, cause error) error {
	current, ok := m.runs[r.ID]
	if !ok {
		return ErrNotFound
	}
	current.Status = RotationFailed
	current.FailedCount++
	current.UpdatedAt = now
	if cause != nil {
		current.FailureReason = cause.Error()
	}
	current.LeaseOwner = ""
	m.runs[r.ID] = current
	return nil
}
