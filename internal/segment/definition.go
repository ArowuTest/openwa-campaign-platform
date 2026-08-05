package segment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

type CloneDefinitionInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Reason      string `json:"reason"`
	ActorID     string `json:"-"`
}

type RuleChange struct {
	Path   string               `json:"path"`
	Before *audiencefilter.Rule `json:"before,omitempty"`
	After  *audiencefilter.Rule `json:"after,omitempty"`
}

type VersionComparison struct {
	SegmentID          string       `json:"segmentId"`
	FromVersion        int64        `json:"fromVersion"`
	ToVersion          int64        `json:"toVersion"`
	NameChanged        bool         `json:"nameChanged"`
	DescriptionChanged bool         `json:"descriptionChanged"`
	StatusChanged      bool         `json:"statusChanged"`
	JoinChanged        bool         `json:"joinChanged"`
	AddedRules         []RuleChange `json:"addedRules"`
	RemovedRules       []RuleChange `json:"removedRules"`
	ChangedRules       []RuleChange `json:"changedRules"`
	Equivalent         bool         `json:"equivalent"`
}

func (s *DefinitionService) Clone(ctx context.Context, identifier string, input CloneDefinitionInput, hasPermission func(string) bool) (Definition, error) {
	if s == nil || s.Repository == nil || s.Registry == nil {
		return Definition{}, errors.New("segment definition service is not configured")
	}
	source, err := s.Repository.Get(ctx, strings.TrimSpace(identifier))
	if err != nil {
		return Definition{}, err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Name == "" || input.ActorID == "" || len(input.Reason) < 8 {
		return Definition{}, errors.New("name, actor and a reason of at least 8 characters are required")
	}
	if input.Description == "" {
		input.Description = source.Description
	}
	if err := source.Definition.ValidateForPermissions(s.Registry, hasPermission); err != nil {
		return Definition{}, err
	}
	identifierNew, err := id.New()
	if err != nil {
		return Definition{}, err
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	clone := Definition{
		ID: identifierNew, OrganisationID: source.OrganisationID, Name: input.Name,
		Description: input.Description, Definition: cloneGroup(source.Definition), Status: StatusActive,
		Version: 1, CreatedBy: input.ActorID, UpdatedBy: input.ActorID, CreatedAt: now, UpdatedAt: now,
	}
	return s.Repository.Create(ctx, clone, "cloned from "+source.ID+": "+input.Reason)
}

func (s *DefinitionService) CompareVersions(ctx context.Context, identifier string, fromVersion, toVersion int64) (VersionComparison, error) {
	if s == nil || s.Repository == nil {
		return VersionComparison{}, errors.New("segment definition service is not configured")
	}
	if fromVersion <= 0 || toVersion <= 0 || fromVersion == toVersion {
		return VersionComparison{}, errors.New("two different positive versions are required")
	}
	versions, err := s.Repository.Versions(ctx, strings.TrimSpace(identifier), 500)
	if err != nil {
		return VersionComparison{}, err
	}
	var from, to *DefinitionVersion
	for i := range versions {
		v := versions[i]
		if v.Version == fromVersion {
			copyValue := v
			from = &copyValue
		}
		if v.Version == toVersion {
			copyValue := v
			to = &copyValue
		}
	}
	if from == nil || to == nil {
		return VersionComparison{}, errors.New("requested segment version was not found")
	}
	comparison := VersionComparison{
		SegmentID: identifier, FromVersion: fromVersion, ToVersion: toVersion,
		NameChanged: from.Name != to.Name, DescriptionChanged: from.Description != to.Description,
		StatusChanged: from.Status != to.Status, JoinChanged: from.Definition.Join != to.Definition.Join,
		AddedRules: []RuleChange{}, RemovedRules: []RuleChange{}, ChangedRules: []RuleChange{},
	}
	left := flattenRules(from.Definition)
	right := flattenRules(to.Definition)
	paths := map[string]struct{}{}
	for path := range left {
		paths[path] = struct{}{}
	}
	for path := range right {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		before, hasBefore := left[path]
		after, hasAfter := right[path]
		switch {
		case !hasBefore && hasAfter:
			value := after
			comparison.AddedRules = append(comparison.AddedRules, RuleChange{Path: path, After: &value})
		case hasBefore && !hasAfter:
			value := before
			comparison.RemovedRules = append(comparison.RemovedRules, RuleChange{Path: path, Before: &value})
		case !rulesEqual(before, after):
			leftValue, rightValue := before, after
			comparison.ChangedRules = append(comparison.ChangedRules, RuleChange{Path: path, Before: &leftValue, After: &rightValue})
		}
	}
	comparison.Equivalent = !comparison.NameChanged && !comparison.DescriptionChanged && !comparison.StatusChanged && !comparison.JoinChanged && len(comparison.AddedRules) == 0 && len(comparison.RemovedRules) == 0 && len(comparison.ChangedRules) == 0
	return comparison, nil
}

func cloneGroup(group audiencefilter.Group) audiencefilter.Group {
	result := audiencefilter.Group{Join: group.Join, Rules: make([]audiencefilter.Rule, len(group.Rules)), Children: make([]audiencefilter.Group, len(group.Children))}
	for i, rule := range group.Rules {
		result.Rules[i] = audiencefilter.Rule{DefinitionCode: rule.DefinitionCode, Operator: rule.Operator, Values: append([]any(nil), rule.Values...)}
	}
	for i, child := range group.Children {
		result.Children[i] = cloneGroup(child)
	}
	return result
}

func flattenRules(group audiencefilter.Group) map[string]audiencefilter.Rule {
	out := map[string]audiencefilter.Rule{}
	var walk func(audiencefilter.Group, string)
	walk = func(current audiencefilter.Group, path string) {
		for index, rule := range current.Rules {
			out[fmt.Sprintf("%s/rules/%d", path, index)] = audiencefilter.Rule{DefinitionCode: rule.DefinitionCode, Operator: rule.Operator, Values: append([]any(nil), rule.Values...)}
		}
		for index, child := range current.Children {
			walk(child, fmt.Sprintf("%s/children/%d", path, index))
		}
	}
	walk(group, "")
	return out
}

func rulesEqual(left, right audiencefilter.Rule) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}
