package importer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

type SourceTrustPolicy struct {
	OrganisationID string    `json:"organisationId"`
	SourceSystem   string    `json:"sourceSystem"`
	TrustLevel     int       `json:"trustLevel"`
	Reason         string    `json:"reason"`
	UpdatedBy      string    `json:"updatedBy"`
	Version        int64     `json:"version"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

var (
	ErrSourceTrustNotFound = errors.New("source trust policy not found")
	ErrSourceTrustVersion  = errors.New("source trust policy version conflict")
)

type SourceTrustRepository interface {
	List(context.Context, string) ([]SourceTrustPolicy, error)
	Upsert(context.Context, SourceTrustPolicy, int64) (SourceTrustPolicy, error)
}

type SourceTrustService struct {
	Repository SourceTrustRepository
	Clock      func() time.Time
}

func (s *SourceTrustService) List(ctx context.Context, organisationID string) ([]SourceTrustPolicy, error) {
	if s == nil || s.Repository == nil {
		return nil, errors.New("source trust repository is required")
	}
	if strings.TrimSpace(organisationID) == "" {
		return nil, errors.New("organisation ID is required")
	}
	return s.Repository.List(ctx, strings.TrimSpace(organisationID))
}

func (s *SourceTrustService) Upsert(ctx context.Context, organisationID, sourceSystem string, trustLevel int, reason, actor string, expectedVersion int64) (SourceTrustPolicy, error) {
	if s == nil || s.Repository == nil {
		return SourceTrustPolicy{}, errors.New("source trust repository is required")
	}
	organisationID = strings.TrimSpace(organisationID)
	sourceSystem = strings.ToUpper(strings.TrimSpace(sourceSystem))
	reason = strings.TrimSpace(reason)
	actor = strings.TrimSpace(actor)
	if organisationID == "" || sourceSystem == "" || actor == "" {
		return SourceTrustPolicy{}, errors.New("organisation, source system and actor are required")
	}
	if trustLevel < 0 || trustLevel > 100 {
		return SourceTrustPolicy{}, errors.New("trust level must be between 0 and 100")
	}
	if len(reason) < 8 {
		return SourceTrustPolicy{}, errors.New("reason must contain at least 8 characters")
	}
	if expectedVersion < 0 {
		return SourceTrustPolicy{}, errors.New("expected version cannot be negative")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	return s.Repository.Upsert(ctx, SourceTrustPolicy{OrganisationID: organisationID, SourceSystem: sourceSystem, TrustLevel: trustLevel, Reason: reason, UpdatedBy: actor, UpdatedAt: now}, expectedVersion)
}

type MemorySourceTrustRepository struct {
	mu    sync.Mutex
	items map[string]SourceTrustPolicy
}

func NewMemorySourceTrustRepository() *MemorySourceTrustRepository {
	return &MemorySourceTrustRepository{items: map[string]SourceTrustPolicy{}}
}
func sourceTrustKey(org, source string) string {
	return strings.TrimSpace(org) + "\x1f" + strings.ToUpper(strings.TrimSpace(source))
}
func (r *MemorySourceTrustRepository) List(_ context.Context, organisationID string) ([]SourceTrustPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []SourceTrustPolicy{}
	for _, v := range r.items {
		if v.OrganisationID == organisationID {
			out = append(out, v)
		}
	}
	sortSourceTrust(out)
	return out, nil
}
func (r *MemorySourceTrustRepository) Upsert(_ context.Context, input SourceTrustPolicy, expectedVersion int64) (SourceTrustPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := sourceTrustKey(input.OrganisationID, input.SourceSystem)
	current, exists := r.items[key]
	if !exists {
		if expectedVersion != 0 {
			return SourceTrustPolicy{}, ErrSourceTrustVersion
		}
		input.Version = 1
		r.items[key] = input
		return input, nil
	}
	if current.Version != expectedVersion {
		return SourceTrustPolicy{}, ErrSourceTrustVersion
	}
	input.Version = current.Version + 1
	r.items[key] = input
	return input, nil
}
func sortSourceTrust(values []SourceTrustPolicy) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j].SourceSystem < values[j-1].SourceSystem; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
