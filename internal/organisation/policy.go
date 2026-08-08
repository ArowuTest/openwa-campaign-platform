package organisation

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

var (
	ErrPolicyNotFound      = errors.New("organisation policy not found")
	ErrPolicyConflict      = errors.New("organisation policy version conflict")
	ErrPolicyInvalid       = errors.New("organisation policy is invalid")
	ErrPurposeProhibited   = errors.New("campaign purpose is prohibited by organisation policy")
	ErrPurposeNotPermitted = errors.New("campaign purpose is not permitted by organisation policy")
)

type PolicyStatus string

const (
	PolicyDraft    PolicyStatus = "DRAFT"
	PolicyPending  PolicyStatus = "PENDING_APPROVAL"
	PolicyActive   PolicyStatus = "ACTIVE"
	PolicyRejected PolicyStatus = "REJECTED"
	PolicyRetired  PolicyStatus = "RETIRED"
)

type FrequencyCap struct {
	PurposeID   string `json:"purposeId,omitempty"`
	Channel     string `json:"channel"`
	MaxMessages int    `json:"maxMessages"`
	WindowHours int    `json:"windowHours"`
}

type Policy struct {
	ID                    string         `json:"id"`
	OrganisationID        string         `json:"organisationId"`
	AllowedPurposeIDs     []string       `json:"allowedPurposeIds"`
	ProhibitedPurposeIDs  []string       `json:"prohibitedPurposeIds"`
	FrequencyCaps         []FrequencyCap `json:"frequencyCaps,omitempty"`
	ContactRetentionDays  int            `json:"contactRetentionDays"`
	CampaignRetentionDays int            `json:"campaignRetentionDays"`
	ReportBrandName       string         `json:"reportBrandName,omitempty"`
	ReportFooter          string         `json:"reportFooter,omitempty"`
	Status                PolicyStatus   `json:"status"`
	EffectiveFrom         time.Time      `json:"effectiveFrom"`
	EffectiveTo           *time.Time     `json:"effectiveTo,omitempty"`
	Version               int64          `json:"version"`
	CreatedBy             string         `json:"createdBy"`
	SubmittedBy           string         `json:"submittedBy,omitempty"`
	ApprovedBy            string         `json:"approvedBy,omitempty"`
	Reason                string         `json:"reason"`
	CreatedAt             time.Time      `json:"createdAt"`
	UpdatedAt             time.Time      `json:"updatedAt"`
}

type PolicyStore interface {
	List(context.Context, string) ([]Policy, error)
	Get(context.Context, string) (Policy, error)
	Active(context.Context, string, time.Time) (Policy, error)
	Create(context.Context, Policy) (Policy, error)
	CompareAndSwap(context.Context, Policy, int64) (Policy, error)
}

type PolicyAdministration struct {
	Store         PolicyStore
	Organisations interface {
		Get(context.Context, string) (Organisation, error)
	}
	Clock func() time.Time
}

func (s *PolicyAdministration) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func canonicalPurposeIDs(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" || len(v) > 128 {
			return nil, ErrPolicyInvalid
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out, nil
}

func canonicalFrequencyCaps(values []FrequencyCap) ([]FrequencyCap, error) {
	seen := map[string]struct{}{}
	out := make([]FrequencyCap, 0, len(values))
	for _, raw := range values {
		cap := raw
		cap.PurposeID = strings.TrimSpace(cap.PurposeID)
		cap.Channel = strings.ToUpper(strings.TrimSpace(cap.Channel))
		if cap.Channel == "" || len(cap.Channel) > 32 || cap.MaxMessages < 1 || cap.MaxMessages > 10000 || cap.WindowHours < 1 || cap.WindowHours > 8760 {
			return nil, ErrPolicyInvalid
		}
		key := cap.PurposeID + "\x1f" + cap.Channel
		if _, ok := seen[key]; ok {
			return nil, ErrPolicyInvalid
		}
		seen[key] = struct{}{}
		out = append(out, cap)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PurposeID == out[j].PurposeID {
			return out[i].Channel < out[j].Channel
		}
		return out[i].PurposeID < out[j].PurposeID
	})
	return out, nil
}

func validatePolicy(p Policy) error {
	allowed, err := canonicalPurposeIDs(p.AllowedPurposeIDs)
	if err != nil {
		return err
	}
	prohibited, err := canonicalPurposeIDs(p.ProhibitedPurposeIDs)
	if err != nil {
		return err
	}
	overlap := map[string]struct{}{}
	for _, v := range allowed {
		overlap[v] = struct{}{}
	}
	for _, v := range prohibited {
		if _, ok := overlap[v]; ok {
			return ErrPolicyInvalid
		}
	}
	if p.ContactRetentionDays < 1 || p.ContactRetentionDays > 3650 || p.CampaignRetentionDays < 30 || p.CampaignRetentionDays > 3650 {
		return ErrPolicyInvalid
	}
	if len(strings.TrimSpace(p.ReportBrandName)) > 160 || len(strings.TrimSpace(p.ReportFooter)) > 500 {
		return ErrPolicyInvalid
	}
	if _, err := canonicalFrequencyCaps(p.FrequencyCaps); err != nil {
		return err
	}
	return nil
}
func (s *PolicyAdministration) CreateDraft(ctx context.Context, p Policy, actor, reason string) (Policy, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(p.OrganisationID) == "" || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 5 {
		return Policy{}, ErrPolicyInvalid
	}
	if s.Organisations != nil {
		org, err := s.Organisations.Get(ctx, p.OrganisationID)
		if err != nil {
			return Policy{}, err
		}
		if org.Status == StatusClosed {
			return Policy{}, ErrClosed
		}
	}
	p.AllowedPurposeIDs, _ = canonicalPurposeIDs(p.AllowedPurposeIDs)
	p.ProhibitedPurposeIDs, _ = canonicalPurposeIDs(p.ProhibitedPurposeIDs)
	var capErr error
	p.FrequencyCaps, capErr = canonicalFrequencyCaps(p.FrequencyCaps)
	if capErr != nil {
		return Policy{}, capErr
	}
	if err := validatePolicy(p); err != nil {
		return Policy{}, err
	}
	now := s.now()
	if p.EffectiveFrom.IsZero() {
		p.EffectiveFrom = now
	}
	identifier, err := id.New()
	if err != nil {
		return Policy{}, err
	}
	p.ID = identifier
	p.Status = PolicyDraft
	p.Version = 1
	p.CreatedBy = strings.TrimSpace(actor)
	p.Reason = strings.TrimSpace(reason)
	p.CreatedAt = now
	p.UpdatedAt = now
	return s.Store.Create(ctx, p)
}
func (s *PolicyAdministration) Submit(ctx context.Context, identifier string, expected int64, actor, reason string) (Policy, error) {
	p, err := s.Store.Get(ctx, identifier)
	if err != nil {
		return Policy{}, err
	}
	if p.Version != expected {
		return Policy{}, ErrPolicyConflict
	}
	if p.Status != PolicyDraft && p.Status != PolicyRejected {
		return Policy{}, ErrPolicyInvalid
	}
	if strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 5 {
		return Policy{}, ErrPolicyInvalid
	}
	p.Status = PolicyPending
	p.SubmittedBy = actor
	p.ApprovedBy = ""
	p.Reason = strings.TrimSpace(reason)
	p.Version++
	p.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, p, expected)
}
func (s *PolicyAdministration) Decide(ctx context.Context, identifier string, expected int64, approve bool, actor, reason string) (Policy, error) {
	p, err := s.Store.Get(ctx, identifier)
	if err != nil {
		return Policy{}, err
	}
	if p.Version != expected {
		return Policy{}, ErrPolicyConflict
	}
	if p.Status != PolicyPending || strings.TrimSpace(actor) == "" || actor == p.SubmittedBy || len(strings.TrimSpace(reason)) < 5 {
		return Policy{}, ErrPolicyInvalid
	}
	if approve {
		p.Status = PolicyActive
		p.ApprovedBy = actor
	} else {
		p.Status = PolicyRejected
	}
	p.Reason = strings.TrimSpace(reason)
	p.Version++
	p.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, p, expected)
}
func (s *PolicyAdministration) List(ctx context.Context, organisationID string) ([]Policy, error) {
	return s.Store.List(ctx, organisationID)
}
func (s *PolicyAdministration) Active(ctx context.Context, organisationID string) (Policy, error) {
	return s.Store.Active(ctx, organisationID, s.now())
}
func (s *PolicyAdministration) ValidatePurpose(ctx context.Context, organisationID, purposeID string) error {
	p, err := s.Active(ctx, organisationID)
	if err != nil {
		return err
	}
	for _, v := range p.ProhibitedPurposeIDs {
		if v == purposeID {
			return ErrPurposeProhibited
		}
	}
	if len(p.AllowedPurposeIDs) > 0 {
		for _, v := range p.AllowedPurposeIDs {
			if v == purposeID {
				return nil
			}
		}
		return ErrPurposeNotPermitted
	}
	return nil
}

type MemoryPolicyStore struct {
	mu    sync.RWMutex
	items map[string]Policy
}

func NewMemoryPolicyStore() *MemoryPolicyStore { return &MemoryPolicyStore{items: map[string]Policy{}} }
func (m *MemoryPolicyStore) List(_ context.Context, org string) ([]Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Policy{}
	for _, p := range m.items {
		if p.OrganisationID == org {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (m *MemoryPolicyStore) ListPolicyPage(_ context.Context, org string, limit int, before *time.Time, beforeID string) ([]Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Policy, 0, len(m.items))
	for _, p := range m.items {
		if org != "" && p.OrganisationID != org {
			continue
		}
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

func (m *MemoryPolicyStore) Get(_ context.Context, id string) (Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.items[id]
	if !ok {
		return Policy{}, ErrPolicyNotFound
	}
	return p, nil
}
func (m *MemoryPolicyStore) Active(_ context.Context, org string, at time.Time) (Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var best Policy
	for _, p := range m.items {
		if p.OrganisationID == org && p.Status == PolicyActive && !p.EffectiveFrom.After(at) && (p.EffectiveTo == nil || p.EffectiveTo.After(at)) && (best.ID == "" || p.EffectiveFrom.After(best.EffectiveFrom)) {
			best = p
		}
	}
	if best.ID == "" {
		return Policy{}, ErrPolicyNotFound
	}
	return best, nil
}
func (m *MemoryPolicyStore) Create(_ context.Context, p Policy) (Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[p.ID]; ok {
		return Policy{}, ErrPolicyConflict
	}
	m.items[p.ID] = p
	return p, nil
}
func (m *MemoryPolicyStore) CompareAndSwap(_ context.Context, p Policy, expected int64) (Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.items[p.ID]
	if !ok {
		return Policy{}, ErrPolicyNotFound
	}
	if old.Version != expected {
		return Policy{}, ErrPolicyConflict
	}
	if p.Status == PolicyActive {
		for id, x := range m.items {
			if id != p.ID && x.OrganisationID == p.OrganisationID && x.Status == PolicyActive {
				x.Status = PolicyRetired
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
