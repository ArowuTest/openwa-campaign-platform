package segment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	"campaign-platform/internal/shared/id"
)

type Status string

const (
	StatusDraft    Status = "DRAFT"
	StatusActive   Status = "ACTIVE"
	StatusArchived Status = "ARCHIVED"
)

var (
	ErrDefinitionNotFound = errors.New("segment definition not found")
	ErrDefinitionConflict = errors.New("segment definition version conflict")
	ErrDefinitionState    = errors.New("segment definition state does not allow this operation")
)

type Definition struct {
	ID             string               `json:"id"`
	OrganisationID string               `json:"organisationId"`
	Name           string               `json:"name"`
	Description    string               `json:"description,omitempty"`
	Definition     audiencefilter.Group `json:"definition"`
	Status         Status               `json:"status"`
	Version        int64                `json:"version"`
	CreatedBy      string               `json:"createdBy"`
	UpdatedBy      string               `json:"updatedBy"`
	CreatedAt      time.Time            `json:"createdAt"`
	UpdatedAt      time.Time            `json:"updatedAt"`
}

type DefinitionVersion struct {
	SegmentID   string               `json:"segmentId"`
	Version     int64                `json:"version"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Definition  audiencefilter.Group `json:"definition"`
	Status      Status               `json:"status"`
	ChangedBy   string               `json:"changedBy"`
	Reason      string               `json:"reason"`
	CreatedAt   time.Time            `json:"createdAt"`
}

type CreateDefinitionInput struct {
	OrganisationID string               `json:"organisationId"`
	Name           string               `json:"name"`
	Description    string               `json:"description,omitempty"`
	Definition     audiencefilter.Group `json:"definition"`
	ActorID        string               `json:"-"`
}

type UpdateDefinitionInput struct {
	Name            string               `json:"name"`
	Description     string               `json:"description,omitempty"`
	Definition      audiencefilter.Group `json:"definition"`
	ExpectedVersion int64                `json:"expectedVersion"`
	Reason          string               `json:"reason"`
	ActorID         string               `json:"-"`
}

type DefinitionRepository interface {
	Create(context.Context, Definition, string) (Definition, error)
	Get(context.Context, string) (Definition, error)
	List(context.Context, string, int) ([]Definition, error)
	Update(context.Context, Definition, int64, string) (Definition, error)
	Versions(context.Context, string, int) ([]DefinitionVersion, error)
}

type DefinitionService struct {
	Repository DefinitionRepository
	Registry   *audiencefilter.Registry
	Clock      func() time.Time
}

func (s *DefinitionService) Create(ctx context.Context, input CreateDefinitionInput, hasPermission func(string) bool) (Definition, error) {
	if s == nil || s.Repository == nil || s.Registry == nil {
		return Definition{}, errors.New("segment definition service is not configured")
	}
	input.OrganisationID = strings.TrimSpace(input.OrganisationID)
	input.Name = strings.TrimSpace(input.Name)
	input.ActorID = strings.TrimSpace(input.ActorID)
	if input.OrganisationID == "" || input.Name == "" || input.ActorID == "" {
		return Definition{}, errors.New("organisation, name and actor are required")
	}
	if len(input.Name) > 160 || len(input.Description) > 2000 {
		return Definition{}, errors.New("segment name or description is too long")
	}
	if err := input.Definition.ValidateForPermissions(s.Registry, hasPermission); err != nil {
		return Definition{}, err
	}
	identifier, err := id.New()
	if err != nil {
		return Definition{}, err
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	value := Definition{ID: identifier, OrganisationID: input.OrganisationID, Name: input.Name, Description: strings.TrimSpace(input.Description), Definition: input.Definition, Status: StatusActive, Version: 1, CreatedBy: input.ActorID, UpdatedBy: input.ActorID, CreatedAt: now, UpdatedAt: now}
	return s.Repository.Create(ctx, value, "segment created")
}

func (s *DefinitionService) Update(ctx context.Context, identifier string, input UpdateDefinitionInput, hasPermission func(string) bool) (Definition, error) {
	if s == nil || s.Repository == nil || s.Registry == nil {
		return Definition{}, errors.New("segment definition service is not configured")
	}
	current, err := s.Repository.Get(ctx, strings.TrimSpace(identifier))
	if err != nil {
		return Definition{}, err
	}
	if current.Status == StatusArchived {
		return Definition{}, ErrDefinitionState
	}
	input.Name = strings.TrimSpace(input.Name)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Name == "" || input.ActorID == "" || input.Reason == "" || input.ExpectedVersion <= 0 {
		return Definition{}, errors.New("name, actor, reason and expected version are required")
	}
	if err := input.Definition.ValidateForPermissions(s.Registry, hasPermission); err != nil {
		return Definition{}, err
	}
	current.Name = input.Name
	current.Description = strings.TrimSpace(input.Description)
	current.Definition = input.Definition
	current.UpdatedBy = input.ActorID
	current.Version++
	if s.Clock != nil {
		current.UpdatedAt = s.Clock().UTC()
	} else {
		current.UpdatedAt = time.Now().UTC()
	}
	return s.Repository.Update(ctx, current, input.ExpectedVersion, input.Reason)
}

func (s *DefinitionService) Archive(ctx context.Context, identifier, actor, reason string, expectedVersion int64) (Definition, error) {
	if s == nil || s.Repository == nil {
		return Definition{}, errors.New("segment definition service is not configured")
	}
	current, err := s.Repository.Get(ctx, strings.TrimSpace(identifier))
	if err != nil {
		return Definition{}, err
	}
	if current.Status == StatusArchived {
		return current, nil
	}
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" || expectedVersion <= 0 {
		return Definition{}, errors.New("actor, reason and expected version are required")
	}
	current.Status = StatusArchived
	current.UpdatedBy = strings.TrimSpace(actor)
	current.Version++
	if s.Clock != nil {
		current.UpdatedAt = s.Clock().UTC()
	} else {
		current.UpdatedAt = time.Now().UTC()
	}
	return s.Repository.Update(ctx, current, expectedVersion, strings.TrimSpace(reason))
}
func (s *DefinitionService) Get(ctx context.Context, id string) (Definition, error) {
	return s.Repository.Get(ctx, strings.TrimSpace(id))
}
func (s *DefinitionService) List(ctx context.Context, organisationID string, limit int) ([]Definition, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.Repository.List(ctx, strings.TrimSpace(organisationID), limit)
}
func (s *DefinitionService) Versions(ctx context.Context, id string, limit int) ([]DefinitionVersion, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.Repository.Versions(ctx, strings.TrimSpace(id), limit)
}

type MemoryDefinitionRepository struct {
	mu       sync.RWMutex
	items    map[string]Definition
	versions map[string][]DefinitionVersion
}

func NewMemoryDefinitionRepository() *MemoryDefinitionRepository {
	return &MemoryDefinitionRepository{items: map[string]Definition{}, versions: map[string][]DefinitionVersion{}}
}
func (r *MemoryDefinitionRepository) Create(_ context.Context, v Definition, reason string) (Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.items {
		if x.OrganisationID == v.OrganisationID && x.Status != StatusArchived && strings.EqualFold(x.Name, v.Name) {
			return Definition{}, errors.New("active segment name already exists")
		}
	}
	r.items[v.ID] = cloneDefinition(v)
	r.append(v, reason)
	return cloneDefinition(v), nil
}
func (r *MemoryDefinitionRepository) Get(_ context.Context, id string) (Definition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.items[id]
	if !ok {
		return Definition{}, ErrDefinitionNotFound
	}
	return cloneDefinition(v), nil
}
func (r *MemoryDefinitionRepository) List(_ context.Context, org string, limit int) ([]Definition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []Definition{}
	for _, v := range r.items {
		if org == "" || v.OrganisationID == org {
			out = append(out, cloneDefinition(v))
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func (r *MemoryDefinitionRepository) Update(_ context.Context, v Definition, expected int64, reason string) (Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.items[v.ID]
	if !ok {
		return Definition{}, ErrDefinitionNotFound
	}
	if current.Version != expected {
		return Definition{}, ErrDefinitionConflict
	}
	r.items[v.ID] = cloneDefinition(v)
	r.append(v, reason)
	return cloneDefinition(v), nil
}
func (r *MemoryDefinitionRepository) Versions(_ context.Context, id string, limit int) ([]DefinitionVersion, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.items[id]; !ok {
		return nil, ErrDefinitionNotFound
	}
	items := r.versions[id]
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	out := append([]DefinitionVersion(nil), items...)
	return out, nil
}
func (r *MemoryDefinitionRepository) append(v Definition, reason string) {
	r.versions[v.ID] = append(r.versions[v.ID], DefinitionVersion{SegmentID: v.ID, Version: v.Version, Name: v.Name, Description: v.Description, Definition: v.Definition, Status: v.Status, ChangedBy: v.UpdatedBy, Reason: reason, CreatedAt: v.UpdatedAt})
}
func cloneDefinition(v Definition) Definition {
	payload, _ := json.Marshal(v.Definition)
	_ = json.Unmarshal(payload, &v.Definition)
	return v
}
