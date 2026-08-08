package consent

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrOptOutPolicyNotFound = errors.New("opt-out policy not found")
	ErrOptOutPolicyConflict = errors.New("opt-out policy version conflict")
	ErrOptOutPolicyInvalid  = errors.New("opt-out policy is invalid")
)

type OptOutPolicyStatus string

const (
	OptOutPolicyDraft    OptOutPolicyStatus = "DRAFT"
	OptOutPolicyPending  OptOutPolicyStatus = "PENDING_APPROVAL"
	OptOutPolicyActive   OptOutPolicyStatus = "ACTIVE"
	OptOutPolicyRejected OptOutPolicyStatus = "REJECTED"
	OptOutPolicyRetired  OptOutPolicyStatus = "RETIRED"
)

type GovernedOptOutPolicy struct {
	ID            string             `json:"id"`
	Keywords      []string           `json:"keywords"`
	Status        OptOutPolicyStatus `json:"status"`
	EffectiveFrom time.Time          `json:"effectiveFrom"`
	EffectiveTo   *time.Time         `json:"effectiveTo,omitempty"`
	Version       int64              `json:"version"`
	CreatedBy     string             `json:"createdBy"`
	SubmittedBy   string             `json:"submittedBy,omitempty"`
	ApprovedBy    string             `json:"approvedBy,omitempty"`
	Reason        string             `json:"reason"`
	CreatedAt     time.Time          `json:"createdAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
}

type OptOutPolicyStore interface {
	List(context.Context) ([]GovernedOptOutPolicy, error)
	Get(context.Context, string) (GovernedOptOutPolicy, error)
	Active(context.Context, time.Time) (GovernedOptOutPolicy, error)
	Create(context.Context, GovernedOptOutPolicy) (GovernedOptOutPolicy, error)
	CompareAndSwap(context.Context, GovernedOptOutPolicy, int64) (GovernedOptOutPolicy, error)
}

type OptOutPolicyAdministration struct {
	Store OptOutPolicyStore
	Clock func() time.Time
}

func (s *OptOutPolicyAdministration) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
func canonicalKeywords(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		k := normaliseOptOutText(v)
		if k == "" || len(k) > 64 {
			return nil, ErrOptOutPolicyInvalid
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	if len(out) == 0 || len(out) > 100 {
		return nil, ErrOptOutPolicyInvalid
	}
	sort.Strings(out)
	return out, nil
}
func (s *OptOutPolicyAdministration) CreateDraft(ctx context.Context, keywords []string, effectiveFrom time.Time, actor, reason string) (GovernedOptOutPolicy, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 5 {
		return GovernedOptOutPolicy{}, ErrOptOutPolicyInvalid
	}
	ks, err := canonicalKeywords(keywords)
	if err != nil {
		return GovernedOptOutPolicy{}, err
	}
	now := s.now()
	if effectiveFrom.IsZero() {
		effectiveFrom = now
	}
	p := GovernedOptOutPolicy{ID: newPolicyID(now), Keywords: ks, Status: OptOutPolicyDraft, EffectiveFrom: effectiveFrom.UTC(), Version: 1, CreatedBy: actor, Reason: strings.TrimSpace(reason), CreatedAt: now, UpdatedAt: now}
	return s.Store.Create(ctx, p)
}
func (s *OptOutPolicyAdministration) Submit(ctx context.Context, id string, expected int64, actor, reason string) (GovernedOptOutPolicy, error) {
	p, err := s.Store.Get(ctx, id)
	if err != nil {
		return GovernedOptOutPolicy{}, err
	}
	if p.Version != expected {
		return GovernedOptOutPolicy{}, ErrOptOutPolicyConflict
	}
	if p.Status != OptOutPolicyDraft && p.Status != OptOutPolicyRejected {
		return GovernedOptOutPolicy{}, ErrOptOutPolicyInvalid
	}
	if strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 5 {
		return GovernedOptOutPolicy{}, ErrOptOutPolicyInvalid
	}
	p.Status = OptOutPolicyPending
	p.SubmittedBy = actor
	p.ApprovedBy = ""
	p.Reason = strings.TrimSpace(reason)
	p.Version++
	p.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, p, expected)
}
func (s *OptOutPolicyAdministration) Decide(ctx context.Context, id string, expected int64, approve bool, actor, reason string) (GovernedOptOutPolicy, error) {
	p, err := s.Store.Get(ctx, id)
	if err != nil {
		return GovernedOptOutPolicy{}, err
	}
	if p.Version != expected {
		return GovernedOptOutPolicy{}, ErrOptOutPolicyConflict
	}
	if p.Status != OptOutPolicyPending || strings.TrimSpace(actor) == "" || actor == p.SubmittedBy || len(strings.TrimSpace(reason)) < 5 {
		return GovernedOptOutPolicy{}, ErrOptOutPolicyInvalid
	}
	if approve {
		p.Status = OptOutPolicyActive
		p.ApprovedBy = actor
	} else {
		p.Status = OptOutPolicyRejected
	}
	p.Reason = strings.TrimSpace(reason)
	p.Version++
	p.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, p, expected)
}
func (s *OptOutPolicyAdministration) Active(ctx context.Context) (OptOutPolicy, error) {
	p, err := s.Store.Active(ctx, s.now())
	if err != nil {
		return OptOutPolicy{}, err
	}
	return OptOutPolicy{Keywords: append([]string(nil), p.Keywords...)}, nil
}
func newPolicyID(t time.Time) string { return "op-" + t.Format("20060102150405.000000000") }

type MemoryOptOutPolicyStore struct {
	mu    sync.RWMutex
	items map[string]GovernedOptOutPolicy
}

func NewMemoryOptOutPolicyStore(seed GovernedOptOutPolicy) *MemoryOptOutPolicyStore {
	m := &MemoryOptOutPolicyStore{items: map[string]GovernedOptOutPolicy{}}
	if seed.ID != "" {
		m.items[seed.ID] = seed
	}
	return m
}
func (m *MemoryOptOutPolicyStore) List(context.Context) ([]GovernedOptOutPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]GovernedOptOutPolicy, 0, len(m.items))
	for _, p := range m.items {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (m *MemoryOptOutPolicyStore) ListOptOutPolicyPage(_ context.Context, limit int, before *time.Time, beforeID string) ([]GovernedOptOutPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]GovernedOptOutPolicy, 0, len(m.items))
	for _, p := range m.items {
		if before != nil && !(p.CreatedAt.Before(*before) || (p.CreatedAt.Equal(*before) && p.ID < beforeID)) {
			continue
		}
		out = append(out, p)
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

func (m *MemoryOptOutPolicyStore) Get(_ context.Context, id string) (GovernedOptOutPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.items[id]
	if !ok {
		return GovernedOptOutPolicy{}, ErrOptOutPolicyNotFound
	}
	return p, nil
}
func (m *MemoryOptOutPolicyStore) Active(_ context.Context, at time.Time) (GovernedOptOutPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var best GovernedOptOutPolicy
	for _, p := range m.items {
		if p.Status == OptOutPolicyActive && !p.EffectiveFrom.After(at) && (p.EffectiveTo == nil || p.EffectiveTo.After(at)) && (best.ID == "" || p.EffectiveFrom.After(best.EffectiveFrom)) {
			best = p
		}
	}
	if best.ID == "" {
		return GovernedOptOutPolicy{}, ErrOptOutPolicyNotFound
	}
	return best, nil
}
func (m *MemoryOptOutPolicyStore) Create(_ context.Context, p GovernedOptOutPolicy) (GovernedOptOutPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[p.ID]; ok {
		return GovernedOptOutPolicy{}, ErrOptOutPolicyConflict
	}
	m.items[p.ID] = p
	return p, nil
}
func (m *MemoryOptOutPolicyStore) CompareAndSwap(_ context.Context, p GovernedOptOutPolicy, expected int64) (GovernedOptOutPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.items[p.ID]
	if !ok {
		return GovernedOptOutPolicy{}, ErrOptOutPolicyNotFound
	}
	if old.Version != expected {
		return GovernedOptOutPolicy{}, ErrOptOutPolicyConflict
	}
	if p.Status == OptOutPolicyActive {
		for id, x := range m.items {
			if id != p.ID && x.Status == OptOutPolicyActive {
				x.Status = OptOutPolicyRetired
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
