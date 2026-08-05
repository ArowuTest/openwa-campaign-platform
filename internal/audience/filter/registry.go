package filter

import (
	"fmt"
	"sort"
	"sync"
)

type Registry struct {
	mu          sync.RWMutex
	definitions map[string]Definition
}

func NewRegistry(definitions ...Definition) (*Registry, error) {
	registry := &Registry{definitions: make(map[string]Definition)}
	for _, definition := range definitions {
		if err := registry.Register(definition); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(definition Definition) error {
	definition = normaliseDefinition(definition)
	if err := definition.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.definitions[definition.Code]; exists {
		return fmt.Errorf("filter definition %s already exists", definition.Code)
	}
	r.definitions[definition.Code] = CloneDefinition(definition)
	return nil
}

func (r *Registry) ReplaceAll(definitions []Definition) error {
	next := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		definition = normaliseDefinition(definition)
		if err := definition.Validate(); err != nil {
			return err
		}
		if _, exists := next[definition.Code]; exists {
			return fmt.Errorf("filter definition %s is duplicated", definition.Code)
		}
		next[definition.Code] = CloneDefinition(definition)
	}
	r.mu.Lock()
	r.definitions = next
	r.mu.Unlock()
	return nil
}

func (r *Registry) Get(code string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definition, exists := r.definitions[code]
	return CloneDefinition(definition), exists
}

func (r *Registry) ListActive() []Definition { return r.list(false) }
func (r *Registry) ListAll() []Definition    { return r.list(true) }
func (r *Registry) list(includeInactive bool) []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definitions := make([]Definition, 0, len(r.definitions))
	for _, definition := range r.definitions {
		if includeInactive || (definition.Active && definition.Filterable) {
			definitions = append(definitions, CloneDefinition(definition))
		}
	}
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].DisplayOrder == definitions[j].DisplayOrder {
			return definitions[i].Code < definitions[j].Code
		}
		return definitions[i].DisplayOrder < definitions[j].DisplayOrder
	})
	return definitions
}

func (r *Registry) ListForPermissions(hasPermission func(string) bool) []Definition {
	items := r.ListActive()
	out := make([]Definition, 0, len(items))
	for _, item := range items {
		if item.RequiresPermission == "" || (hasPermission != nil && hasPermission(item.RequiresPermission)) {
			out = append(out, item)
		}
	}
	return out
}
