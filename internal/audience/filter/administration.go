package filter

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrDefinitionNotFound  = errors.New("filter definition was not found")
	ErrDefinitionConflict  = errors.New("filter definition changed; reload before updating")
	ErrDefinitionDuplicate = errors.New("filter definition code already exists")
	ErrDefinitionInvalid   = errors.New("filter definition is invalid")
)

var mandatoryDefinitionCodes = map[string]struct{}{"COUNTRY": {}, "STATE": {}, "LGA": {}, "REPORTED_AGE": {}, "GENDER": {}}

type AdministrationStore interface {
	List(context.Context) ([]Definition, error)
	Get(context.Context, string) (Definition, error)
	Generation(context.Context) (int64, error)
	Create(context.Context, Definition, string, string) (Definition, error)
	CompareAndSwap(context.Context, Definition, int64, string, string) (Definition, error)
}

type AdministrationService struct {
	store      AdministrationStore
	registry   *Registry
	clock      func() time.Time
	refreshMu  sync.Mutex
	generation int64
}

func NewAdministrationService(store AdministrationStore, registry *Registry) *AdministrationService {
	return &AdministrationService{store: store, registry: registry, clock: time.Now}
}

type CreateDefinitionInput struct {
	Code               string     `json:"code"`
	DisplayName        string     `json:"displayName"`
	Description        string     `json:"description"`
	DataType           DataType   `json:"dataType"`
	Operators          []Operator `json:"operators,omitempty"`
	Filterable         *bool      `json:"filterable,omitempty"`
	Reportable         bool       `json:"reportable"`
	Sensitive          bool       `json:"sensitive"`
	RequiresPermission string     `json:"requiresPermission,omitempty"`
	AllowedValues      []string   `json:"allowedValues,omitempty"`
	DisplayOrder       int        `json:"displayOrder"`
	Active             *bool      `json:"active,omitempty"`
	Reason             string     `json:"reason"`
	ActorID            string     `json:"-"`
}
type UpdateDefinitionInput struct {
	DisplayName        string     `json:"displayName"`
	Description        string     `json:"description"`
	Operators          []Operator `json:"operators"`
	Filterable         bool       `json:"filterable"`
	Reportable         bool       `json:"reportable"`
	Sensitive          bool       `json:"sensitive"`
	RequiresPermission string     `json:"requiresPermission,omitempty"`
	AllowedValues      []string   `json:"allowedValues,omitempty"`
	DisplayOrder       int        `json:"displayOrder"`
	Active             bool       `json:"active"`
	ExpectedVersion    int64      `json:"expectedVersion"`
	Reason             string     `json:"reason"`
	ActorID            string     `json:"-"`
}

func (s *AdministrationService) Refresh(ctx context.Context) error {
	if s == nil || s.store == nil || s.registry == nil {
		return errors.New("filter administration is not configured")
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	generation, err := s.store.Generation(ctx)
	if err != nil {
		return err
	}
	if generation == s.generation && generation != 0 {
		return nil
	}
	items, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	if err := ensureMandatoryDefinitions(items); err != nil {
		return err
	}
	if err := s.registry.ReplaceAll(items); err != nil {
		return err
	}
	s.generation = generation
	return nil
}
func (s *AdministrationService) List(ctx context.Context) ([]Definition, error) {
	if err := s.Refresh(ctx); err != nil {
		return nil, err
	}
	return s.registry.ListAll(), nil
}
func (s *AdministrationService) ListForPermissions(ctx context.Context, hasPermission func(string) bool) ([]Definition, error) {
	if err := s.Refresh(ctx); err != nil {
		return nil, err
	}
	return s.registry.ListForPermissions(hasPermission), nil
}
func (s *AdministrationService) Create(ctx context.Context, input CreateDefinitionInput) (Definition, error) {
	if err := validateAdministrationEvidence(input.ActorID, input.Reason); err != nil {
		return Definition{}, err
	}
	filterable, active := true, true
	if input.Filterable != nil {
		filterable = *input.Filterable
	}
	if input.Active != nil {
		active = *input.Active
	}
	operators := append([]Operator(nil), input.Operators...)
	if len(operators) == 0 {
		operators = DefaultOperators(input.DataType)
	}
	now := s.clock().UTC()
	definition := normaliseDefinition(Definition{Code: input.Code, DisplayName: input.DisplayName, Description: input.Description, DataType: input.DataType, Operators: operators, Core: false, Filterable: filterable, Reportable: input.Reportable, Sensitive: input.Sensitive, RequiresPermission: input.RequiresPermission, Storage: StorageContactAttribute, AttributeValueColumn: AttributeValueColumnForDataType(input.DataType), IndexStrategy: "btree", AllowedValues: input.AllowedValues, DisplayOrder: input.DisplayOrder, Active: active, Version: 1, CreatedAt: now, UpdatedAt: now})
	if err := definition.Validate(); err != nil {
		return Definition{}, fmt.Errorf("%w: %v", ErrDefinitionInvalid, err)
	}
	created, err := s.store.Create(ctx, definition, strings.TrimSpace(input.ActorID), strings.TrimSpace(input.Reason))
	if err != nil {
		return Definition{}, err
	}
	if err := s.forceRefresh(ctx); err != nil {
		return Definition{}, err
	}
	return CloneDefinition(created), nil
}
func (s *AdministrationService) Update(ctx context.Context, code string, input UpdateDefinitionInput) (Definition, error) {
	if input.ExpectedVersion <= 0 {
		return Definition{}, fmt.Errorf("%w: expected version is required", ErrDefinitionInvalid)
	}
	if err := validateAdministrationEvidence(input.ActorID, input.Reason); err != nil {
		return Definition{}, err
	}
	current, err := s.store.Get(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return Definition{}, err
	}
	if current.Version != input.ExpectedVersion {
		return Definition{}, ErrDefinitionConflict
	}
	updated := CloneDefinition(current)
	updated.DisplayName = strings.TrimSpace(input.DisplayName)
	updated.Description = strings.TrimSpace(input.Description)
	updated.Operators = append([]Operator(nil), input.Operators...)
	updated.Filterable = input.Filterable
	updated.Reportable = input.Reportable
	updated.Sensitive = input.Sensitive
	updated.RequiresPermission = strings.TrimSpace(input.RequiresPermission)
	updated.AllowedValues = append([]string(nil), input.AllowedValues...)
	updated.DisplayOrder = input.DisplayOrder
	updated.Active = input.Active
	updated.Version = input.ExpectedVersion + 1
	updated.UpdatedAt = s.clock().UTC()
	if _, mandatory := mandatoryDefinitionCodes[updated.Code]; mandatory && (!updated.Active || !updated.Filterable) {
		return Definition{}, fmt.Errorf("%w: mandatory filter %s cannot be disabled", ErrDefinitionInvalid, updated.Code)
	}
	updated = normaliseDefinition(updated)
	if err := updated.Validate(); err != nil {
		return Definition{}, fmt.Errorf("%w: %v", ErrDefinitionInvalid, err)
	}
	stored, err := s.store.CompareAndSwap(ctx, updated, input.ExpectedVersion, strings.TrimSpace(input.ActorID), strings.TrimSpace(input.Reason))
	if err != nil {
		return Definition{}, err
	}
	if err := s.forceRefresh(ctx); err != nil {
		return Definition{}, err
	}
	return CloneDefinition(stored), nil
}
func (s *AdministrationService) forceRefresh(ctx context.Context) error {
	s.refreshMu.Lock()
	s.generation = 0
	s.refreshMu.Unlock()
	return s.Refresh(ctx)
}
func validateAdministrationEvidence(actorID, reason string) error {
	if strings.TrimSpace(actorID) == "" || len(strings.TrimSpace(reason)) < 5 || len(reason) > 1000 {
		return fmt.Errorf("%w: actor and a meaningful reason are required", ErrDefinitionInvalid)
	}
	return nil
}
func ensureMandatoryDefinitions(items []Definition) error {
	found := map[string]Definition{}
	for _, item := range items {
		found[item.Code] = item
	}
	for code := range mandatoryDefinitionCodes {
		item, ok := found[code]
		if !ok || !item.Core || !item.Active || !item.Filterable {
			return fmt.Errorf("mandatory filter definition %s is missing or disabled", code)
		}
	}
	return nil
}

type MemoryAdministrationStore struct {
	mu         sync.RWMutex
	items      map[string]Definition
	generation int64
	history    []Definition
}

func NewMemoryAdministrationStore(definitions []Definition) (*MemoryAdministrationStore, error) {
	store := &MemoryAdministrationStore{items: map[string]Definition{}, generation: 1}
	now := time.Now().UTC()
	for _, item := range definitions {
		item = normaliseDefinition(item)
		if item.Version == 0 {
			item.Version = 1
		}
		if item.CreatedAt.IsZero() {
			item.CreatedAt = now
		}
		if item.UpdatedAt.IsZero() {
			item.UpdatedAt = item.CreatedAt
		}
		if err := item.Validate(); err != nil {
			return nil, err
		}
		if _, exists := store.items[item.Code]; exists {
			return nil, ErrDefinitionDuplicate
		}
		store.items[item.Code] = CloneDefinition(item)
	}
	if err := ensureMandatoryDefinitions(definitions); err != nil {
		return nil, err
	}
	return store, nil
}
func (s *MemoryAdministrationStore) List(context.Context) ([]Definition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Definition, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, CloneDefinition(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].DisplayOrder == items[j].DisplayOrder {
			return items[i].Code < items[j].Code
		}
		return items[i].DisplayOrder < items[j].DisplayOrder
	})
	return items, nil
}
func (s *MemoryAdministrationStore) Get(_ context.Context, code string) (Definition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[strings.ToUpper(strings.TrimSpace(code))]
	if !ok {
		return Definition{}, ErrDefinitionNotFound
	}
	return CloneDefinition(item), nil
}
func (s *MemoryAdministrationStore) Generation(context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation, nil
}
func (s *MemoryAdministrationStore) Create(_ context.Context, definition Definition, _, _ string) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.items[definition.Code]; exists {
		return Definition{}, ErrDefinitionDuplicate
	}
	s.items[definition.Code] = CloneDefinition(definition)
	s.history = append(s.history, CloneDefinition(definition))
	s.generation++
	return CloneDefinition(definition), nil
}
func (s *MemoryAdministrationStore) CompareAndSwap(_ context.Context, definition Definition, expected int64, _, _ string) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.items[definition.Code]
	if !exists {
		return Definition{}, ErrDefinitionNotFound
	}
	if current.Version != expected {
		return Definition{}, ErrDefinitionConflict
	}
	s.items[definition.Code] = CloneDefinition(definition)
	s.history = append(s.history, CloneDefinition(definition))
	s.generation++
	return CloneDefinition(definition), nil
}
