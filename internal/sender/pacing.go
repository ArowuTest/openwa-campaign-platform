package sender

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

type PacingScope string
type PacingStatus string
type JitterMode string

const (
	PacingPlatform    PacingScope = "PLATFORM"
	PacingProvider    PacingScope = "PROVIDER"
	PacingEngine      PacingScope = "ENGINE"
	PacingGatewayPool PacingScope = "GATEWAY_POOL"
	PacingSenderPool  PacingScope = "SENDER_POOL"
	PacingSession     PacingScope = "SESSION"
	PacingCampaign    PacingScope = "CAMPAIGN"

	PacingDraft    PacingStatus = "DRAFT"
	PacingPending  PacingStatus = "PENDING_APPROVAL"
	PacingActive   PacingStatus = "ACTIVE"
	PacingRejected PacingStatus = "REJECTED"
	PacingRetired  PacingStatus = "RETIRED"

	JitterNone    JitterMode = "NONE"
	JitterUniform JitterMode = "UNIFORM"
)

var (
	ErrPacingInvalid  = errors.New("sender pacing policy is invalid")
	ErrPacingNotFound = errors.New("sender pacing policy not found")
	ErrPacingConflict = errors.New("sender pacing policy version conflict")
)

type MessageTypeOverride struct {
	MessageType    string `json:"messageType"`
	MinimumDelayMS int64  `json:"minimumDelayMs"`
	MaximumDelayMS int64  `json:"maximumDelayMs"`
}

type PacingPolicy struct {
	ID                  string                `json:"id"`
	Scope               PacingScope           `json:"scope"`
	ScopeID             string                `json:"scopeId,omitempty"`
	Provider            string                `json:"provider,omitempty"`
	Engine              string                `json:"engine,omitempty"`
	MinimumDelayMS      int64                 `json:"minimumDelayMs"`
	MaximumDelayMS      int64                 `json:"maximumDelayMs"`
	JitterMode          JitterMode            `json:"jitterMode"`
	MaxInFlight         int                   `json:"maxInFlight"`
	MessagesPerMinute   int                   `json:"messagesPerMinute"`
	HourlyAllowance     int64                 `json:"hourlyAllowance"`
	DailyAllowance      int64                 `json:"dailyAllowance"`
	MaxActiveCampaigns  int                   `json:"maxActiveCampaigns"`
	BurstSize           int                   `json:"burstSize"`
	CooldownSeconds     int                   `json:"cooldownSeconds"`
	RecoveryRampMinutes int                   `json:"recoveryRampMinutes"`
	FailureThresholdBPS int                   `json:"failureThresholdBps"`
	DisconnectThreshold int                   `json:"disconnectThreshold"`
	AutoQuarantine      bool                  `json:"autoQuarantine"`
	Overrides           []MessageTypeOverride `json:"messageTypeOverrides,omitempty"`
	Status              PacingStatus          `json:"status"`
	EffectiveFrom       time.Time             `json:"effectiveFrom"`
	EffectiveTo         *time.Time            `json:"effectiveTo,omitempty"`
	Version             int64                 `json:"version"`
	CreatedBy           string                `json:"createdBy"`
	SubmittedBy         string                `json:"submittedBy,omitempty"`
	ApprovedBy          string                `json:"approvedBy,omitempty"`
	Reason              string                `json:"reason"`
	CreatedAt           time.Time             `json:"createdAt"`
	UpdatedAt           time.Time             `json:"updatedAt"`
}

type PacingScopeRef struct {
	Scope PacingScope `json:"scope"`
	ID    string      `json:"id"`
}

type ResolvedPacing struct {
	Policy          PacingPolicy `json:"policy"`
	SourcePolicyIDs []string     `json:"sourcePolicyIds"`
	ResolvedAt      time.Time    `json:"resolvedAt"`
}

type PacingStore interface {
	Create(context.Context, PacingPolicy) (PacingPolicy, error)
	Get(context.Context, string) (PacingPolicy, error)
	List(context.Context) ([]PacingPolicy, error)
	CompareAndSwap(context.Context, PacingPolicy, int64) (PacingPolicy, error)
	ActiveByScope(context.Context, PacingScope, string, time.Time) ([]PacingPolicy, error)
}

type PacingAdministration struct {
	Store PacingStore
	Clock func() time.Time
}

func (s *PacingAdministration) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func validatePacing(p *PacingPolicy) error {
	p.ScopeID = strings.TrimSpace(p.ScopeID)
	p.Provider = strings.ToUpper(strings.TrimSpace(p.Provider))
	p.Engine = strings.ToUpper(strings.TrimSpace(p.Engine))
	switch p.Scope {
	case PacingPlatform:
		if p.ScopeID != "" {
			return ErrPacingInvalid
		}
	case PacingProvider, PacingEngine, PacingGatewayPool, PacingSenderPool, PacingSession, PacingCampaign:
		if p.ScopeID == "" {
			return ErrPacingInvalid
		}
	default:
		return ErrPacingInvalid
	}
	if p.MinimumDelayMS < 0 || p.MaximumDelayMS < p.MinimumDelayMS || p.MaximumDelayMS > 300000 {
		return ErrPacingInvalid
	}
	if p.JitterMode != JitterNone && p.JitterMode != JitterUniform {
		return ErrPacingInvalid
	}
	if p.MaxInFlight < 1 || p.MaxInFlight > 100 || p.MessagesPerMinute < 1 || p.MessagesPerMinute > 100000 {
		return ErrPacingInvalid
	}
	if p.HourlyAllowance < 1 || p.DailyAllowance < p.HourlyAllowance || p.DailyAllowance > 100000000 {
		return ErrPacingInvalid
	}
	if p.MaxActiveCampaigns < 1 || p.MaxActiveCampaigns > 1000 || p.BurstSize < 1 || p.BurstSize > 1000 {
		return ErrPacingInvalid
	}
	if p.CooldownSeconds < 0 || p.CooldownSeconds > 86400 || p.RecoveryRampMinutes < 0 || p.RecoveryRampMinutes > 1440 {
		return ErrPacingInvalid
	}
	if p.FailureThresholdBPS < 0 || p.FailureThresholdBPS > 10000 || p.DisconnectThreshold < 0 || p.DisconnectThreshold > 1000 {
		return ErrPacingInvalid
	}
	seen := map[string]struct{}{}
	for i := range p.Overrides {
		o := &p.Overrides[i]
		o.MessageType = strings.ToUpper(strings.TrimSpace(o.MessageType))
		if o.MessageType == "" || o.MinimumDelayMS < 0 || o.MaximumDelayMS < o.MinimumDelayMS || o.MaximumDelayMS > 300000 {
			return ErrPacingInvalid
		}
		if _, ok := seen[o.MessageType]; ok {
			return ErrPacingInvalid
		}
		seen[o.MessageType] = struct{}{}
	}
	sort.Slice(p.Overrides, func(i, j int) bool { return p.Overrides[i].MessageType < p.Overrides[j].MessageType })
	return nil
}

func (s *PacingAdministration) CreateDraft(ctx context.Context, p PacingPolicy, actor, reason string) (PacingPolicy, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 5 {
		return PacingPolicy{}, ErrPacingInvalid
	}
	if err := validatePacing(&p); err != nil {
		return PacingPolicy{}, err
	}
	now := s.now()
	if p.EffectiveFrom.IsZero() {
		p.EffectiveFrom = now
	}
	ident, err := id.New()
	if err != nil {
		return PacingPolicy{}, err
	}
	p.ID = ident
	p.Status = PacingDraft
	p.Version = 1
	p.CreatedBy = actor
	p.Reason = strings.TrimSpace(reason)
	p.CreatedAt = now
	p.UpdatedAt = now
	return s.Store.Create(ctx, p)
}
func (s *PacingAdministration) Submit(ctx context.Context, ident string, expected int64, actor, reason string) (PacingPolicy, error) {
	p, err := s.Store.Get(ctx, ident)
	if err != nil {
		return PacingPolicy{}, err
	}
	if p.Version != expected {
		return PacingPolicy{}, ErrPacingConflict
	}
	if (p.Status != PacingDraft && p.Status != PacingRejected) || actor == "" || len(strings.TrimSpace(reason)) < 5 {
		return PacingPolicy{}, ErrPacingInvalid
	}
	p.Status = PacingPending
	p.SubmittedBy = actor
	p.ApprovedBy = ""
	p.Reason = strings.TrimSpace(reason)
	p.Version++
	p.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, p, expected)
}
func (s *PacingAdministration) Decide(ctx context.Context, ident string, expected int64, approve bool, actor, reason string) (PacingPolicy, error) {
	p, err := s.Store.Get(ctx, ident)
	if err != nil {
		return PacingPolicy{}, err
	}
	if p.Version != expected {
		return PacingPolicy{}, ErrPacingConflict
	}
	if p.Status != PacingPending || actor == "" || actor == p.SubmittedBy || len(strings.TrimSpace(reason)) < 5 {
		return PacingPolicy{}, ErrPacingInvalid
	}
	if approve {
		p.Status = PacingActive
		p.ApprovedBy = actor
	} else {
		p.Status = PacingRejected
	}
	p.Reason = strings.TrimSpace(reason)
	p.Version++
	p.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, p, expected)
}
func (s *PacingAdministration) List(ctx context.Context) ([]PacingPolicy, error) {
	return s.Store.List(ctx)
}

// Resolve applies least-specific to most-specific precedence and returns one frozen effective policy.
func (s *PacingAdministration) Resolve(ctx context.Context, scopes []PacingScopeRef, at time.Time) (ResolvedPacing, error) {
	if at.IsZero() {
		at = s.now()
	}
	var effective *PacingPolicy
	ids := []string{}
	for _, scope := range scopes {
		policies, err := s.Store.ActiveByScope(ctx, scope.Scope, scope.ID, at)
		if err != nil {
			return ResolvedPacing{}, err
		}
		for _, p := range policies {
			copy := p
			effective = &copy
			ids = append(ids, p.ID)
		}
	}
	if effective == nil {
		return ResolvedPacing{}, ErrPacingNotFound
	}
	return ResolvedPacing{Policy: *effective, SourcePolicyIDs: ids, ResolvedAt: at.UTC()}, nil
}

type MemoryPacingStore struct {
	mu     sync.Mutex
	values map[string]PacingPolicy
}

func NewMemoryPacingStore() *MemoryPacingStore {
	return &MemoryPacingStore{values: map[string]PacingPolicy{}}
}
func (m *MemoryPacingStore) Create(_ context.Context, p PacingPolicy) (PacingPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[p.ID] = p
	return p, nil
}
func (m *MemoryPacingStore) Get(_ context.Context, id string) (PacingPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.values[id]
	if !ok {
		return PacingPolicy{}, ErrPacingNotFound
	}
	return p, nil
}
func (m *MemoryPacingStore) List(_ context.Context) ([]PacingPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PacingPolicy, 0, len(m.values))
	for _, p := range m.values {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (m *MemoryPacingStore) CompareAndSwap(_ context.Context, p PacingPolicy, expected int64) (PacingPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.values[p.ID]
	if !ok {
		return PacingPolicy{}, ErrPacingNotFound
	}
	if old.Version != expected {
		return PacingPolicy{}, ErrPacingConflict
	}
	if p.Status == PacingActive {
		for id, v := range m.values {
			if id != p.ID && v.Scope == p.Scope && v.ScopeID == p.ScopeID && v.Status == PacingActive {
				v.Status = PacingRetired
				v.EffectiveTo = &p.EffectiveFrom
				v.Version++
				v.UpdatedAt = p.UpdatedAt
				m.values[id] = v
			}
		}
	}
	m.values[p.ID] = p
	return p, nil
}
func (m *MemoryPacingStore) ActiveByScope(_ context.Context, scope PacingScope, id string, at time.Time) ([]PacingPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []PacingPolicy{}
	for _, p := range m.values {
		if p.Scope == scope && p.ScopeID == id && p.Status == PacingActive && !p.EffectiveFrom.After(at) && (p.EffectiveTo == nil || p.EffectiveTo.After(at)) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EffectiveFrom.Before(out[j].EffectiveFrom) })
	return out, nil
}
