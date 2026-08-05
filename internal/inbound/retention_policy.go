package inbound

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrRetentionPolicyNotFound = errors.New("inbound retention policy not found")
	ErrRetentionPolicyConflict = errors.New("inbound retention policy version conflict")
	ErrRetentionPolicyInvalid  = errors.New("inbound retention policy is invalid")
)

type RetentionPolicyStatus string

const (
	RetentionPolicyDraft    RetentionPolicyStatus = "DRAFT"
	RetentionPolicyPending  RetentionPolicyStatus = "PENDING_APPROVAL"
	RetentionPolicyActive   RetentionPolicyStatus = "ACTIVE"
	RetentionPolicyRejected RetentionPolicyStatus = "REJECTED"
	RetentionPolicyRetired  RetentionPolicyStatus = "RETIRED"
)

type RetentionPolicy struct {
	ID            string                `json:"id"`
	RetentionDays int                   `json:"retentionDays"`
	Status        RetentionPolicyStatus `json:"status"`
	EffectiveFrom time.Time             `json:"effectiveFrom"`
	EffectiveTo   *time.Time            `json:"effectiveTo,omitempty"`
	Version       int64                 `json:"version"`
	CreatedBy     string                `json:"createdBy"`
	SubmittedBy   string                `json:"submittedBy,omitempty"`
	ApprovedBy    string                `json:"approvedBy,omitempty"`
	Reason        string                `json:"reason"`
	CreatedAt     time.Time             `json:"createdAt"`
	UpdatedAt     time.Time             `json:"updatedAt"`
}

type RetentionPolicyStore interface {
	List(context.Context) ([]RetentionPolicy, error)
	Get(context.Context, string) (RetentionPolicy, error)
	Active(context.Context, time.Time) (RetentionPolicy, error)
	Create(context.Context, RetentionPolicy) (RetentionPolicy, error)
	CompareAndSwap(context.Context, RetentionPolicy, int64) (RetentionPolicy, error)
}

type RetentionPolicyAdministration struct {
	Store RetentionPolicyStore
	Clock func() time.Time
}

func (a *RetentionPolicyAdministration) now() time.Time {
	if a != nil && a.Clock != nil {
		return a.Clock().UTC()
	}
	return time.Now().UTC()
}
func validateRetention(days int, actor, reason string) error {
	if days < 1 || days > 3650 || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 5 {
		return ErrRetentionPolicyInvalid
	}
	return nil
}
func (a *RetentionPolicyAdministration) CreateDraft(ctx context.Context, days int, effective time.Time, actor, reason string) (RetentionPolicy, error) {
	if a == nil || a.Store == nil || validateRetention(days, actor, reason) != nil {
		return RetentionPolicy{}, ErrRetentionPolicyInvalid
	}
	now := a.now()
	if effective.IsZero() {
		effective = now
	}
	p := RetentionPolicy{ID: "irp-" + now.Format("20060102150405.000000000"), RetentionDays: days, Status: RetentionPolicyDraft, EffectiveFrom: effective.UTC(), Version: 1, CreatedBy: strings.TrimSpace(actor), Reason: strings.TrimSpace(reason), CreatedAt: now, UpdatedAt: now}
	return a.Store.Create(ctx, p)
}
func (a *RetentionPolicyAdministration) Submit(ctx context.Context, id string, expected int64, actor, reason string) (RetentionPolicy, error) {
	p, err := a.Store.Get(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Version != expected {
		return p, ErrRetentionPolicyConflict
	}
	if p.Status != RetentionPolicyDraft && p.Status != RetentionPolicyRejected {
		return p, ErrRetentionPolicyInvalid
	}
	if validateRetention(p.RetentionDays, actor, reason) != nil {
		return p, ErrRetentionPolicyInvalid
	}
	p.Status = RetentionPolicyPending
	p.SubmittedBy = strings.TrimSpace(actor)
	p.ApprovedBy = ""
	p.Reason = strings.TrimSpace(reason)
	p.Version++
	p.UpdatedAt = a.now()
	return a.Store.CompareAndSwap(ctx, p, expected)
}
func (a *RetentionPolicyAdministration) Decide(ctx context.Context, id string, expected int64, approve bool, actor, reason string) (RetentionPolicy, error) {
	p, err := a.Store.Get(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Version != expected {
		return p, ErrRetentionPolicyConflict
	}
	if p.Status != RetentionPolicyPending || strings.TrimSpace(actor) == "" || actor == p.SubmittedBy || len(strings.TrimSpace(reason)) < 5 {
		return p, ErrRetentionPolicyInvalid
	}
	if approve {
		p.Status = RetentionPolicyActive
		p.ApprovedBy = strings.TrimSpace(actor)
	} else {
		p.Status = RetentionPolicyRejected
	}
	p.Reason = strings.TrimSpace(reason)
	p.Version++
	p.UpdatedAt = a.now()
	return a.Store.CompareAndSwap(ctx, p, expected)
}
func (a *RetentionPolicyAdministration) ActiveDuration(ctx context.Context) (time.Duration, error) {
	p, err := a.Store.Active(ctx, a.now())
	if err != nil {
		return 0, err
	}
	return time.Duration(p.RetentionDays) * 24 * time.Hour, nil
}

type MemoryRetentionPolicyStore struct {
	mu    sync.RWMutex
	items map[string]RetentionPolicy
}

func NewMemoryRetentionPolicyStore(seed RetentionPolicy) *MemoryRetentionPolicyStore {
	m := &MemoryRetentionPolicyStore{items: map[string]RetentionPolicy{}}
	if seed.ID != "" {
		m.items[seed.ID] = seed
	}
	return m
}
func (m *MemoryRetentionPolicyStore) List(context.Context) ([]RetentionPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]RetentionPolicy, 0, len(m.items))
	for _, p := range m.items {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (m *MemoryRetentionPolicyStore) Get(_ context.Context, id string) (RetentionPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.items[id]
	if !ok {
		return p, ErrRetentionPolicyNotFound
	}
	return p, nil
}
func (m *MemoryRetentionPolicyStore) Active(_ context.Context, at time.Time) (RetentionPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var best RetentionPolicy
	for _, p := range m.items {
		if p.Status == RetentionPolicyActive && !p.EffectiveFrom.After(at) && (p.EffectiveTo == nil || p.EffectiveTo.After(at)) && (best.ID == "" || p.EffectiveFrom.After(best.EffectiveFrom)) {
			best = p
		}
	}
	if best.ID == "" {
		return best, ErrRetentionPolicyNotFound
	}
	return best, nil
}
func (m *MemoryRetentionPolicyStore) Create(_ context.Context, p RetentionPolicy) (RetentionPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[p.ID]; ok {
		return p, ErrRetentionPolicyConflict
	}
	m.items[p.ID] = p
	return p, nil
}
func (m *MemoryRetentionPolicyStore) CompareAndSwap(_ context.Context, p RetentionPolicy, expected int64) (RetentionPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.items[p.ID]
	if !ok {
		return p, ErrRetentionPolicyNotFound
	}
	if old.Version != expected {
		return p, ErrRetentionPolicyConflict
	}
	if p.Status == RetentionPolicyActive {
		for id, x := range m.items {
			if id != p.ID && x.Status == RetentionPolicyActive {
				x.Status = RetentionPolicyRetired
				to := p.EffectiveFrom
				x.EffectiveTo = &to
				x.Version++
				x.UpdatedAt = p.UpdatedAt
				m.items[id] = x
			}
		}
	}
	m.items[p.ID] = p
	return p, nil
}
