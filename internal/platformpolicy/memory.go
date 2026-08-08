package platformpolicy

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

type MemoryStore struct {
	mu                  sync.Mutex
	configurations      map[string]Configuration
	configurationEvents map[string][]Event
	maintenance         map[string]MaintenanceWindow
	maintenanceEvents   map[string][]Event
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		configurations:      map[string]Configuration{},
		configurationEvents: map[string][]Event{},
		maintenance:         map[string]MaintenanceWindow{},
		maintenanceEvents:   map[string][]Event{},
	}
}

func cloneRaw(v []byte) []byte                         { return append([]byte(nil), v...) }
func cloneConfiguration(v Configuration) Configuration { v.Value = cloneRaw(v.Value); return v }
func cloneEvent(v Event) Event {
	if v.Evidence != nil {
		original := v.Evidence
		v.Evidence = make(map[string]any, len(original))
		for k, x := range original {
			v.Evidence[k] = x
		}
	}
	return v
}

func addEventID(event *Event) error {
	if event.ID != "" {
		return nil
	}
	identifier, err := id.New()
	if err != nil {
		return err
	}
	event.ID = identifier
	return nil
}

func (m *MemoryStore) ListConfigurations(_ context.Context, query ConfigurationQuery) ([]Configuration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Configuration, 0, len(m.configurations))
	for _, v := range m.configurations {
		if query.Key != "" && !strings.EqualFold(v.Key, query.Key) {
			continue
		}
		if query.ScopeType != "" && v.ScopeType != query.ScopeType {
			continue
		}
		if query.ScopeID != "" && v.ScopeID != query.ScopeID {
			continue
		}
		if query.Status != "" && v.Status != query.Status {
			continue
		}
		out = append(out, cloneConfiguration(v))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	limit := query.Limit
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}

func (m *MemoryStore) ListConfigurationPage(_ context.Context, query ConfigurationQuery, before *time.Time, beforeID string) ([]Configuration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Configuration, 0, len(m.configurations))
	for _, v := range m.configurations {
		if query.Key != "" && !strings.EqualFold(v.Key, query.Key) {
			continue
		}
		if query.ScopeType != "" && v.ScopeType != query.ScopeType {
			continue
		}
		if query.ScopeID != "" && v.ScopeID != query.ScopeID {
			continue
		}
		if query.Status != "" && v.Status != query.Status {
			continue
		}
		if before != nil && !(v.UpdatedAt.Before(*before) || (v.UpdatedAt.Equal(*before) && v.ID < beforeID)) {
			continue
		}
		out = append(out, cloneConfiguration(v))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	limit := query.Limit
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}

func (m *MemoryStore) GetConfiguration(_ context.Context, id string) (Configuration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.configurations[id]
	if !ok {
		return Configuration{}, ErrNotFound
	}
	return cloneConfiguration(v), nil
}

func (m *MemoryStore) CreateConfiguration(_ context.Context, value Configuration, event Event) (Configuration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.configurations[value.ID]; exists {
		return Configuration{}, ErrConflict
	}
	if err := addEventID(&event); err != nil {
		return Configuration{}, err
	}
	m.configurations[value.ID] = cloneConfiguration(value)
	m.configurationEvents[value.ID] = append(m.configurationEvents[value.ID], cloneEvent(event))
	return cloneConfiguration(value), nil
}

func (m *MemoryStore) UpdateConfiguration(_ context.Context, value Configuration, expected int64, event Event) (Configuration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.configurations[value.ID]
	if !ok {
		return Configuration{}, ErrNotFound
	}
	if current.Version != expected || value.Version != expected+1 {
		return Configuration{}, ErrConflict
	}
	if err := addEventID(&event); err != nil {
		return Configuration{}, err
	}
	m.configurations[value.ID] = cloneConfiguration(value)
	m.configurationEvents[value.ID] = append(m.configurationEvents[value.ID], cloneEvent(event))
	return cloneConfiguration(value), nil
}

func periodsOverlap(aStart time.Time, aEnd *time.Time, bStart time.Time, bEnd *time.Time) bool {
	if aEnd != nil && !aEnd.After(bStart) {
		return false
	}
	if bEnd != nil && !bEnd.After(aStart) {
		return false
	}
	return true
}

func (m *MemoryStore) ActivateConfiguration(_ context.Context, value Configuration, expected int64, event Event) (Configuration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.configurations[value.ID]
	if !ok {
		return Configuration{}, ErrNotFound
	}
	if current.Version != expected || value.Version != expected+1 || value.Status != StatusActive {
		return Configuration{}, ErrConflict
	}
	for idValue, existing := range m.configurations {
		if idValue == value.ID || existing.Status != StatusActive && existing.Status != StatusSuperseded || existing.Key != value.Key || existing.ScopeType != value.ScopeType || existing.ScopeID != value.ScopeID {
			continue
		}
		if !periodsOverlap(existing.EffectiveFrom, existing.EffectiveTo, value.EffectiveFrom, value.EffectiveTo) {
			continue
		}
		// Future replacements keep the current configuration effective until
		// the exact boundary. Backdated/equal starts fail closed.
		if !value.EffectiveFrom.After(existing.EffectiveFrom) {
			return Configuration{}, ErrConflict
		}
		end := value.EffectiveFrom
		existing.EffectiveTo = &end
		existing.Status = StatusSuperseded
		existing.Version++
		existing.UpdatedAt = event.OccurredAt
		m.configurations[idValue] = existing
		if value.SupersedesID == "" {
			value.SupersedesID = existing.ID
		}
		supersededEvent := Event{
			ObjectID: existing.ID, EventType: "SUPERSEDED", Version: existing.Version,
			ActorID: event.ActorID, Reason: event.Reason,
			Evidence:   map[string]any{"supersededById": value.ID, "effectiveTo": end},
			OccurredAt: event.OccurredAt,
		}
		if err := addEventID(&supersededEvent); err != nil {
			return Configuration{}, err
		}
		m.configurationEvents[existing.ID] = append(m.configurationEvents[existing.ID], cloneEvent(supersededEvent))
	}
	if err := addEventID(&event); err != nil {
		return Configuration{}, err
	}
	m.configurations[value.ID] = cloneConfiguration(value)
	m.configurationEvents[value.ID] = append(m.configurationEvents[value.ID], cloneEvent(event))
	return cloneConfiguration(value), nil
}

func (m *MemoryStore) ResolveConfiguration(_ context.Context, key string, scope ScopeType, scopeID string, at time.Time) (Configuration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var candidates []Configuration
	for _, v := range m.configurations {
		if v.Key != key || v.ScopeType != scope || v.ScopeID != scopeID || (v.Status != StatusActive && v.Status != StatusSuperseded) || v.EffectiveFrom.After(at) || (v.EffectiveTo != nil && !v.EffectiveTo.After(at)) {
			continue
		}
		candidates = append(candidates, cloneConfiguration(v))
	}
	if len(candidates) == 0 {
		return Configuration{}, ErrNotFound
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].EffectiveFrom.Equal(candidates[j].EffectiveFrom) {
			return candidates[i].Version > candidates[j].Version
		}
		return candidates[i].EffectiveFrom.After(candidates[j].EffectiveFrom)
	})
	return candidates[0], nil
}

func (m *MemoryStore) ListConfigurationEvents(_ context.Context, objectID string, limit int) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.configurations[objectID]; !ok {
		return nil, ErrNotFound
	}
	values := m.configurationEvents[objectID]
	if limit <= 0 || limit > len(values) {
		limit = len(values)
	}
	out := make([]Event, 0, limit)
	for i := len(values) - 1; i >= len(values)-limit; i-- {
		out = append(out, cloneEvent(values[i]))
	}
	return out, nil
}

func (m *MemoryStore) ListMaintenance(_ context.Context, status MaintenanceStatus, limit int) ([]MaintenanceWindow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MaintenanceWindow, 0, len(m.maintenance))
	for _, v := range m.maintenance {
		if status != "" && v.Status != status {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}

func (m *MemoryStore) ListMaintenancePage(_ context.Context, status MaintenanceStatus, limit int, before *time.Time, beforeID string) ([]MaintenanceWindow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MaintenanceWindow, 0, len(m.maintenance))
	for _, v := range m.maintenance {
		if status != "" && v.Status != status {
			continue
		}
		if before != nil && !(v.UpdatedAt.Before(*before) || (v.UpdatedAt.Equal(*before) && v.ID < beforeID)) {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}

func (m *MemoryStore) GetMaintenance(_ context.Context, id string) (MaintenanceWindow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.maintenance[id]
	if !ok {
		return MaintenanceWindow{}, ErrNotFound
	}
	return v, nil
}

func (m *MemoryStore) CreateMaintenance(_ context.Context, value MaintenanceWindow, event Event) (MaintenanceWindow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.maintenance[value.ID]; ok {
		return MaintenanceWindow{}, ErrConflict
	}
	if err := addEventID(&event); err != nil {
		return MaintenanceWindow{}, err
	}
	m.maintenance[value.ID] = value
	m.maintenanceEvents[value.ID] = append(m.maintenanceEvents[value.ID], cloneEvent(event))
	return value, nil
}

func (m *MemoryStore) UpdateMaintenance(_ context.Context, value MaintenanceWindow, expected int64, event Event) (MaintenanceWindow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.maintenance[value.ID]
	if !ok {
		return MaintenanceWindow{}, ErrNotFound
	}
	if current.Version != expected || value.Version != expected+1 {
		return MaintenanceWindow{}, ErrConflict
	}
	if value.Status == MaintenanceActive {
		for idValue, existing := range m.maintenance {
			if idValue == value.ID || existing.Status != MaintenanceActive || existing.ScopeType != value.ScopeType || existing.ScopeID != value.ScopeID {
				continue
			}
			if periodsOverlap(existing.StartsAt, existing.EndsAt, value.StartsAt, value.EndsAt) {
				return MaintenanceWindow{}, errors.New("overlapping active maintenance window")
			}
		}
	}
	if err := addEventID(&event); err != nil {
		return MaintenanceWindow{}, err
	}
	m.maintenance[value.ID] = value
	m.maintenanceEvents[value.ID] = append(m.maintenanceEvents[value.ID], cloneEvent(event))
	return value, nil
}

func (m *MemoryStore) ListActiveMaintenance(_ context.Context, at time.Time) ([]MaintenanceWindow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []MaintenanceWindow{}
	for _, v := range m.maintenance {
		if v.Status != MaintenanceActive || v.StartsAt.After(at) || (v.EndsAt != nil && !v.EndsAt.After(at)) {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

func (m *MemoryStore) ListMaintenanceEvents(_ context.Context, objectID string, limit int) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.maintenance[objectID]; !ok {
		return nil, ErrNotFound
	}
	values := m.maintenanceEvents[objectID]
	if limit <= 0 || limit > len(values) {
		limit = len(values)
	}
	out := make([]Event, 0, limit)
	for i := len(values) - 1; i >= len(values)-limit; i-- {
		out = append(out, cloneEvent(values[i]))
	}
	return out, nil
}

func paginateMemoryEvents(values []Event, limit int, before *time.Time, beforeID string) []Event {
	items := append([]Event(nil), values...)
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
		out = append(out, cloneEvent(item))
		if len(out) == limit {
			break
		}
	}
	return out
}

func (m *MemoryStore) ListConfigurationEventPage(_ context.Context, objectID string, limit int, before *time.Time, beforeID string) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.configurations[objectID]; !ok {
		return nil, ErrNotFound
	}
	return paginateMemoryEvents(m.configurationEvents[objectID], limit, before, beforeID), nil
}

func (m *MemoryStore) ListMaintenanceEventPage(_ context.Context, objectID string, limit int, before *time.Time, beforeID string) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.maintenance[objectID]; !ok {
		return nil, ErrNotFound
	}
	return paginateMemoryEvents(m.maintenanceEvents[objectID], limit, before, beforeID), nil
}
