package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	sharedid "campaign-platform/internal/shared/id"
)

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusCompleted  Status = "COMPLETED"
	StatusFailed     Status = "FAILED"
	StatusDeadLetter Status = "DEAD_LETTER"
	StatusCancelled  Status = "CANCELLED"
)

var (
	ErrNotFound      = errors.New("job not found")
	ErrLeaseConflict = errors.New("job lease conflict")
	ErrDuplicate     = errors.New("job deduplication key already exists")
	ErrDedupMismatch = errors.New("job deduplication key was reused with a different job")
	ErrJobInProgress = errors.New("processing job cannot be cancelled; allow the leased attempt to record an outcome")
)

type Job struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	DedupKey       string          `json:"dedupKey"`
	Payload        json.RawMessage `json:"payload"`
	Status         Status          `json:"status"`
	Priority       int             `json:"priority"`
	AttemptCount   int             `json:"attemptCount"`
	MaxAttempts    int             `json:"maxAttempts"`
	AvailableAt    time.Time       `json:"availableAt"`
	LeaseOwner     string          `json:"leaseOwner,omitempty"`
	LeaseExpiresAt *time.Time      `json:"leaseExpiresAt,omitempty"`
	LeaseVersion   int64           `json:"leaseVersion"`
	LastErrorCode  string          `json:"lastErrorCode,omitempty"`
	LastError      string          `json:"lastError,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	CompletedAt    *time.Time      `json:"completedAt,omitempty"`
}

type EnqueueInput struct {
	Type        string
	DedupKey    string
	Payload     any
	Priority    int
	MaxAttempts int
	AvailableAt time.Time
}

func NewJob(input EnqueueInput, now time.Time) (Job, error) {
	if strings.TrimSpace(input.Type) == "" || strings.TrimSpace(input.DedupKey) == "" {
		return Job{}, errors.New("job type and deduplication key are required")
	}
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return Job{}, fmt.Errorf("marshal job payload: %w", err)
	}
	if len(payload) > 1<<20 {
		return Job{}, errors.New("job payload exceeds 1 MiB")
	}
	identifier, err := sharedid.New()
	if err != nil {
		return Job{}, err
	}
	if input.MaxAttempts <= 0 {
		input.MaxAttempts = 5
	}
	available := input.AvailableAt.UTC()
	if available.IsZero() {
		available = now.UTC()
	}
	return Job{ID: identifier, Type: strings.TrimSpace(input.Type), DedupKey: strings.TrimSpace(input.DedupKey),
		Payload: payload, Status: StatusPending, Priority: input.Priority, MaxAttempts: input.MaxAttempts,
		AvailableAt: available, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}, nil
}

type Repository interface {
	Enqueue(context.Context, Job) (Job, bool, error)
	Claim(context.Context, string, time.Time, time.Duration, int, []string) ([]Job, error)
	Complete(context.Context, string, string, int64, time.Time) error
	Fail(context.Context, string, string, int64, time.Time, bool, time.Duration, string, string) error
	Renew(context.Context, string, string, int64, time.Time, time.Duration) error
	Cancel(context.Context, string, time.Time) error
	Get(context.Context, string) (Job, error)
	PendingCount(context.Context, []string, time.Time) (int, error)
}

type Service struct {
	repository Repository
	clock      func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, clock: time.Now}
}

func (s *Service) PendingCount(ctx context.Context, types []string, now time.Time) (int, error) {
	if s == nil || s.repository == nil {
		return 0, errors.New("job repository is required")
	}
	return s.repository.PendingCount(ctx, types, now.UTC())
}

func (s *Service) Enqueue(ctx context.Context, input EnqueueInput) (Job, bool, error) {
	job, err := NewJob(input, s.clock())
	if err != nil {
		return Job{}, false, err
	}
	return s.repository.Enqueue(ctx, job)
}

// MemoryRepository models the durable queue contract and is safe for concurrent
// tests. PostgreSQL remains the authoritative production implementation.
type MemoryRepository struct {
	mu          sync.Mutex
	items       map[string]Job
	byDedup     map[string]string
	adminEvents []AdministrationEvent
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: make(map[string]Job), byDedup: make(map[string]string)}
}

func (r *MemoryRepository) Enqueue(_ context.Context, job Job) (Job, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.byDedup[job.DedupKey]; ok {
		existing := r.items[existingID]
		if existing.Type != job.Type || !jsonEqual(existing.Payload, job.Payload) {
			return Job{}, false, ErrDedupMismatch
		}
		return cloneJob(existing), false, nil
	}
	r.items[job.ID] = cloneJob(job)
	r.byDedup[job.DedupKey] = job.ID
	return cloneJob(job), true, nil
}

func (r *MemoryRepository) Claim(_ context.Context, owner string, now time.Time, lease time.Duration, limit int, types []string) ([]Job, error) {
	if strings.TrimSpace(owner) == "" || lease <= 0 {
		return nil, errors.New("lease owner and positive duration are required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	allowed := make(map[string]struct{}, len(types))
	for _, value := range types {
		allowed[value] = struct{}{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	candidates := make([]Job, 0)
	for _, job := range r.items {
		if len(allowed) > 0 {
			if _, ok := allowed[job.Type]; !ok {
				continue
			}
		}
		claimable := job.Status == StatusPending || (job.Status == StatusProcessing && job.LeaseExpiresAt != nil && !job.LeaseExpiresAt.After(now))
		if !claimable || job.AvailableAt.After(now) {
			continue
		}
		candidates = append(candidates, job)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		if !candidates[i].AvailableAt.Equal(candidates[j].AvailableAt) {
			return candidates[i].AvailableAt.Before(candidates[j].AvailableAt)
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	expires := now.UTC().Add(lease)
	out := make([]Job, 0, len(candidates))
	for _, candidate := range candidates {
		job := r.items[candidate.ID]
		job.Status = StatusProcessing
		job.LeaseOwner = owner
		job.LeaseExpiresAt = &expires
		job.AttemptCount++
		job.LeaseVersion++
		job.UpdatedAt = now.UTC()
		r.items[job.ID] = job
		out = append(out, cloneJob(job))
	}
	return out, nil
}

func (r *MemoryRepository) Complete(_ context.Context, jobID, owner string, leaseVersion int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.items[jobID]
	if !ok {
		return ErrNotFound
	}
	if job.Status != StatusProcessing || job.LeaseOwner != owner || job.LeaseVersion != leaseVersion || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now) {
		return ErrLeaseConflict
	}
	completed := now.UTC()
	job.Status = StatusCompleted
	job.CompletedAt = &completed
	job.UpdatedAt = completed
	job.LeaseOwner = ""
	job.LeaseExpiresAt = nil
	r.items[jobID] = job
	return nil
}

func (r *MemoryRepository) Fail(_ context.Context, jobID, owner string, leaseVersion int64, now time.Time, retryable bool, retryAfter time.Duration, code, detail string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.items[jobID]
	if !ok {
		return ErrNotFound
	}
	if job.Status != StatusProcessing || job.LeaseOwner != owner || job.LeaseVersion != leaseVersion || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now) {
		return ErrLeaseConflict
	}
	job.LastErrorCode, job.LastError, job.UpdatedAt = clean(code), clean(detail), now.UTC()
	job.LeaseOwner, job.LeaseExpiresAt = "", nil
	if !retryable || job.AttemptCount >= job.MaxAttempts {
		job.Status = StatusDeadLetter
	} else {
		job.Status = StatusPending
		job.AvailableAt = now.UTC().Add(retryAfter)
	}
	r.items[jobID] = job
	return nil
}

func (r *MemoryRepository) Renew(_ context.Context, jobID, owner string, leaseVersion int64, now time.Time, lease time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.items[jobID]
	if !ok {
		return ErrNotFound
	}
	if job.Status != StatusProcessing || job.LeaseOwner != owner || job.LeaseVersion != leaseVersion || job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now) {
		return ErrLeaseConflict
	}
	expires := now.UTC().Add(lease)
	job.LeaseExpiresAt = &expires
	job.UpdatedAt = now.UTC()
	r.items[jobID] = job
	return nil
}

func (r *MemoryRepository) Cancel(_ context.Context, jobID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.items[jobID]
	if !ok {
		return ErrNotFound
	}
	switch job.Status {
	case StatusCompleted, StatusDeadLetter, StatusCancelled:
		return nil
	case StatusProcessing:
		return ErrJobInProgress
	case StatusPending:
	default:
		return ErrJobInProgress
	}
	job.Status = StatusCancelled
	job.LeaseOwner = ""
	job.LeaseExpiresAt = nil
	job.UpdatedAt = now.UTC()
	r.items[jobID] = job
	return nil
}

func (r *MemoryRepository) Get(_ context.Context, jobID string) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.items[jobID]
	if !ok {
		return Job{}, ErrNotFound
	}
	return cloneJob(job), nil
}

func (r *MemoryRepository) PendingCount(_ context.Context, types []string, now time.Time) (int, error) {
	allowed := map[string]struct{}{}
	for _, value := range types {
		allowed[value] = struct{}{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, job := range r.items {
		if len(allowed) > 0 {
			if _, ok := allowed[job.Type]; !ok {
				continue
			}
		}
		if job.Status == StatusPending && !job.AvailableAt.After(now) {
			count++
		}
	}
	return count, nil
}

func cloneJob(job Job) Job {
	job.Payload = append(json.RawMessage(nil), job.Payload...)
	return job
}
func jsonEqual(left, right json.RawMessage) bool {
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return string(left) == string(right)
	}
	leftCanonical, _ := json.Marshal(a)
	rightCanonical, _ := json.Marshal(b)
	return string(leftCanonical) == string(rightCanonical)
}
func clean(value string) string { return strings.TrimSpace(value) }

func (r *MemoryRepository) List(_ context.Context, q Query) ([]Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	statuses := map[Status]struct{}{}
	for _, v := range q.Statuses {
		statuses[v] = struct{}{}
	}
	types := map[string]struct{}{}
	for _, v := range q.Types {
		types[strings.TrimSpace(v)] = struct{}{}
	}
	items := make([]Job, 0)
	for _, job := range r.items {
		if len(statuses) > 0 {
			if _, ok := statuses[job.Status]; !ok {
				continue
			}
		}
		if len(types) > 0 {
			if _, ok := types[job.Type]; !ok {
				continue
			}
		}
		items = append(items, cloneJob(job))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	if len(items) > q.Limit {
		items = items[:q.Limit]
	}
	return items, nil
}
func (r *MemoryRepository) Summary(_ context.Context, now time.Time) (QueueSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := QueueSummary{AsAt: now.UTC(), CountsByStatus: map[Status]int64{}, CountsByType: map[string]int64{}}
	for _, job := range r.items {
		out.CountsByStatus[job.Status]++
		out.CountsByType[job.Type]++
		if job.Status == StatusPending && (out.OldestPendingAt == nil || job.CreatedAt.Before(*out.OldestPendingAt)) {
			v := job.CreatedAt
			out.OldestPendingAt = &v
		}
		if job.Status == StatusProcessing {
			out.ProcessingLeases++
			if job.LeaseExpiresAt != nil && !job.LeaseExpiresAt.After(now) {
				out.ExpiredLeases++
			}
		}
	}
	return out, nil
}
func (r *MemoryRepository) RetryDeadLetter(_ context.Context, id, actor, reason string, now time.Time) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.items[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	if job.Status != StatusDeadLetter {
		return Job{}, ErrAdministrativeConflict
	}
	eventID, err := sharedid.New()
	if err != nil {
		return Job{}, err
	}
	prev := job.Status
	job.Status = StatusPending
	job.AttemptCount = 0
	job.AvailableAt = now.UTC()
	job.LastErrorCode = ""
	job.LastError = ""
	job.CompletedAt = nil
	job.UpdatedAt = now.UTC()
	r.items[id] = job
	r.appendAdminEvent(AdministrationEvent{ID: eventID, JobID: id, Action: "RETRY_DEAD_LETTER", ActorID: actor, Reason: reason, Previous: prev, Current: job.Status, OccurredAt: now.UTC()})
	return cloneJob(job), nil
}
func (r *MemoryRepository) CancelPending(_ context.Context, id, actor, reason string, now time.Time) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.items[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	if job.Status != StatusPending {
		return Job{}, ErrAdministrativeConflict
	}
	eventID, err := sharedid.New()
	if err != nil {
		return Job{}, err
	}
	prev := job.Status
	job.Status = StatusCancelled
	job.UpdatedAt = now.UTC()
	r.items[id] = job
	r.appendAdminEvent(AdministrationEvent{ID: eventID, JobID: id, Action: "CANCEL_PENDING", ActorID: actor, Reason: reason, Previous: prev, Current: job.Status, OccurredAt: now.UTC()})
	return cloneJob(job), nil
}
func (r *MemoryRepository) Events(_ context.Context, id string, limit int) ([]AdministrationEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []AdministrationEvent
	for i := len(r.adminEvents) - 1; i >= 0 && len(out) < limit; i-- {
		if r.adminEvents[i].JobID == id {
			out = append(out, r.adminEvents[i])
		}
	}
	return out, nil
}
func (r *MemoryRepository) ListAdministrationEventPage(_ context.Context, jobID string, limit int, before *time.Time, beforeID string) ([]AdministrationEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]AdministrationEvent, 0)
	for _, event := range r.adminEvents {
		if event.JobID == jobID {
			items = append(items, event)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
	out := make([]AdministrationEvent, 0, limit)
	for _, event := range items {
		if before != nil && (event.OccurredAt.After(*before) || (event.OccurredAt.Equal(*before) && event.ID >= beforeID)) {
			continue
		}
		out = append(out, event)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r *MemoryRepository) appendAdminEvent(v AdministrationEvent) {
	r.adminEvents = append(r.adminEvents, v)
}

func (r *MemoryRepository) ListPage(_ context.Context, q Query, before *time.Time, beforeID string) ([]Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	statuses := map[Status]struct{}{}
	for _, value := range q.Statuses {
		statuses[value] = struct{}{}
	}
	types := map[string]struct{}{}
	for _, value := range q.Types {
		types[strings.TrimSpace(value)] = struct{}{}
	}
	items := make([]Job, 0)
	for _, job := range r.items {
		if len(statuses) > 0 {
			if _, ok := statuses[job.Status]; !ok {
				continue
			}
		}
		if len(types) > 0 {
			if _, ok := types[job.Type]; !ok {
				continue
			}
		}
		if before != nil && !(job.CreatedAt.Before(*before) || (job.CreatedAt.Equal(*before) && job.ID < beforeID)) {
			continue
		}
		items = append(items, cloneJob(job))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if q.Limit <= 0 || q.Limit > 501 {
		q.Limit = 100
	}
	if len(items) > q.Limit {
		items = items[:q.Limit]
	}
	return items, nil
}
