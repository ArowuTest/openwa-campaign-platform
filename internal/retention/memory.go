package retention

import (
	"context"
	"sort"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

type MemoryStore struct {
	mu       sync.Mutex
	policies map[string]Policy
	events   map[string][]Event
	jobs     map[string]Job
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{policies: map[string]Policy{}, events: map[string][]Event{}, jobs: map[string]Job{}}
}
func (m *MemoryStore) addEvent(ev Event) error {
	if ev.ID == "" {
		var err error
		ev.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	m.events[ev.PolicyID] = append(m.events[ev.PolicyID], ev)
	return nil
}
func (m *MemoryStore) ListPolicies(_ context.Context, status Status, limit int) ([]Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Policy{}
	for _, v := range m.policies {
		if status == "" || v.Status == status {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *MemoryStore) ListPolicyPage(_ context.Context, status Status, limit int, before *time.Time, beforeID string) ([]Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Policy, 0, len(m.policies))
	for _, v := range m.policies {
		if status != "" && v.Status != status {
			continue
		}
		if before != nil && !(v.CreatedAt.Before(*before) || (v.CreatedAt.Equal(*before) && v.ID < beforeID)) {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}

func (m *MemoryStore) GetPolicy(_ context.Context, key string) (Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.policies[key]
	if !ok {
		return Policy{}, ErrNotFound
	}
	return v, nil
}
func (m *MemoryStore) CreatePolicy(_ context.Context, v Policy, ev Event) (Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.policies[v.ID]; ok {
		return Policy{}, ErrConflict
	}
	if err := m.addEvent(ev); err != nil {
		return Policy{}, err
	}
	m.policies[v.ID] = v
	return v, nil
}
func (m *MemoryStore) UpdatePolicy(_ context.Context, v Policy, expected int64, ev Event) (Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.policies[v.ID]
	if !ok {
		return Policy{}, ErrNotFound
	}
	if cur.Version != expected {
		return Policy{}, ErrConflict
	}
	if err := m.addEvent(ev); err != nil {
		return Policy{}, err
	}
	m.policies[v.ID] = v
	return v, nil
}
func (m *MemoryStore) ActivatePolicy(_ context.Context, v Policy, expected int64, ev Event) (Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.policies[v.ID]
	if !ok {
		return Policy{}, ErrNotFound
	}
	if cur.Version != expected {
		return Policy{}, ErrConflict
	}
	for key, p := range m.policies {
		if key == v.ID || p.Status != StatusActive || p.ObjectType != v.ObjectType || p.ScopeType != v.ScopeType || p.ScopeID != v.ScopeID ||
			!periodsOverlap(p.EffectiveFrom, p.EffectiveTo, v.EffectiveFrom, v.EffectiveTo) {
			continue
		}
		if !v.EffectiveFrom.After(p.EffectiveFrom) {
			return Policy{}, ErrConflict
		}
		end := v.EffectiveFrom
		p.EffectiveTo = &end
		// Keep the prior policy ACTIVE until the scheduled replacement boundary.
		p.Version++
		p.UpdatedAt = v.UpdatedAt
		m.policies[key] = p
		superseded := Event{PolicyID: p.ID, EventType: "SUPERSEDED", Version: p.Version, ActorID: ev.ActorID, Reason: ev.Reason, Evidence: map[string]any{"supersededById": v.ID, "effectiveTo": end}, OccurredAt: ev.OccurredAt}
		if err := m.addEvent(superseded); err != nil {
			return Policy{}, err
		}
	}
	if err := m.addEvent(ev); err != nil {
		return Policy{}, err
	}
	m.policies[v.ID] = v
	return v, nil
}
func (m *MemoryStore) ListEvents(_ context.Context, key string, limit int) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := append([]Event(nil), m.events[key]...)
	sort.Slice(v, func(i, j int) bool { return v[i].OccurredAt.After(v[j].OccurredAt) })
	if len(v) > limit {
		v = v[:limit]
	}
	return v, nil
}
func (m *MemoryStore) ScheduleDue(context.Context, time.Time, int) (int, error) { return 0, nil }
func (m *MemoryStore) ClaimJobs(_ context.Context, worker string, now time.Time, lease time.Duration, limit int) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Job{}
	for key, v := range m.jobs {
		if len(out) >= limit {
			break
		}
		available := v.AvailableAt.IsZero() || !v.AvailableAt.After(now)
		if available && (v.Status == JobPending || v.Status == JobFailed || (v.Status == JobClaimed && v.LeaseExpiresAt != nil && !v.LeaseExpiresAt.After(now))) {
			v.Status = JobClaimed
			v.LeaseOwner = worker
			v.LeaseVersion++
			v.AttemptCount++
			x := now.Add(lease)
			v.LeaseExpiresAt = &x
			v.UpdatedAt = now
			m.jobs[key] = v
			out = append(out, v)
		}
	}
	return out, nil
}
func (m *MemoryStore) CompleteJob(_ context.Context, v Job, e map[string]any, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.jobs[v.ID]
	if !ok {
		return ErrNotFound
	}
	if cur.Status != JobClaimed || cur.LeaseVersion != v.LeaseVersion || cur.LeaseOwner != v.LeaseOwner || cur.LeaseExpiresAt == nil || !cur.LeaseExpiresAt.After(now) {
		return ErrConflict
	}
	cur.Status = JobCompleted
	cur.Evidence = e
	cur.LeaseOwner = ""
	cur.LeaseExpiresAt = nil
	cur.UpdatedAt = now
	cur.CompletedAt = &now
	m.jobs[v.ID] = cur
	return nil
}
func (m *MemoryStore) FailJob(_ context.Context, v Job, code, ref string, now time.Time, retry time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.jobs[v.ID]
	if !ok {
		return ErrNotFound
	}
	if cur.Status != JobClaimed || cur.LeaseVersion != v.LeaseVersion || cur.LeaseOwner != v.LeaseOwner || cur.LeaseExpiresAt == nil || !cur.LeaseExpiresAt.After(now) {
		return ErrConflict
	}
	cur.Status = JobFailed
	cur.LastErrorCode = code
	cur.LastErrorReference = ref
	cur.LeaseOwner = ""
	cur.LeaseExpiresAt = nil
	cur.AvailableAt = now.Add(retry)
	cur.UpdatedAt = now
	m.jobs[v.ID] = cur
	return nil
}
func (m *MemoryStore) HoldJob(_ context.Context, v Job, reason string, e map[string]any, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.jobs[v.ID]
	if !ok {
		return ErrNotFound
	}
	if cur.Status != JobClaimed || cur.LeaseVersion != v.LeaseVersion || cur.LeaseOwner != v.LeaseOwner || cur.LeaseExpiresAt == nil || !cur.LeaseExpiresAt.After(now) {
		return ErrConflict
	}
	cur.Status = JobHeldReview
	cur.LastErrorReference = reason
	cur.Evidence = e
	cur.LeaseOwner = ""
	cur.LeaseExpiresAt = nil
	cur.UpdatedAt = now
	m.jobs[v.ID] = cur
	return nil
}
func (m *MemoryStore) ListJobs(_ context.Context, status JobStatus, limit int) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Job{}
	for _, v := range m.jobs {
		if status == "" || v.Status == status {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *MemoryStore) AddJob(v Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v.ID == "" {
		var err error
		v.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	if v.Status == "" {
		v.Status = JobPending
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
		v.UpdatedAt = v.CreatedAt
	}
	if v.AvailableAt.IsZero() {
		v.AvailableAt = v.CreatedAt
	}
	m.jobs[v.ID] = v
	return nil
}

func (m *MemoryStore) ListJobPage(_ context.Context, status JobStatus, limit int, before *time.Time, beforeID string) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		if status != "" && job.Status != status {
			continue
		}
		if before != nil && !(job.CreatedAt.Before(*before) || (job.CreatedAt.Equal(*before) && job.ID < beforeID)) {
			continue
		}
		items = append(items, job)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (m *MemoryStore) ListEventPage(_ context.Context, key string, limit int, before *time.Time, beforeID string) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.policies[key]; !ok {
		return nil, ErrNotFound
	}
	items := append([]Event(nil), m.events[key]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
	out := make([]Event, 0, limit)
	for _, item := range items {
		if before != nil && (item.OccurredAt.After(*before) || item.OccurredAt.Equal(*before) && item.ID >= beforeID) {
			continue
		}
		out = append(out, item)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
