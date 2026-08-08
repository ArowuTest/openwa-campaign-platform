package organisation

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound  = errors.New("organisation not found")
	ErrDuplicate = errors.New("organisation already exists")
	ErrConflict  = errors.New("organisation version conflict")
	ErrClosed    = errors.New("organisation is closed")
	ErrNotActive = errors.New("organisation is not active")
)

type Repository interface {
	Create(context.Context, Organisation) error
	List(context.Context) ([]Organisation, error)
	Get(context.Context, string) (Organisation, error)
	Update(context.Context, Organisation, int64, Event) (Organisation, error)
	ListEvents(context.Context, string) ([]Event, error)
}

type Service struct {
	repository Repository
	clock      func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, clock: time.Now}
}
func (s *Service) Create(ctx context.Context, input CreateInput) (Organisation, error) {
	entity, err := New(input, s.clock())
	if err != nil {
		return Organisation{}, err
	}
	if err = s.repository.Create(ctx, entity); err != nil {
		return Organisation{}, err
	}
	return entity, nil
}
func (s *Service) List(ctx context.Context) ([]Organisation, error) { return s.repository.List(ctx) }
func (s *Service) Get(ctx context.Context, id string) (Organisation, error) {
	return s.repository.Get(ctx, id)
}
func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (Organisation, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return Organisation{}, err
	}
	updated, err := ApplyUpdate(current, input, s.clock())
	if err != nil {
		return Organisation{}, err
	}
	ev, err := NewEvent(id, updated.Version, "UPDATED", input.ActorID, input.Reason, current.Status, updated.Status, s.clock())
	if err != nil {
		return Organisation{}, err
	}
	return s.repository.Update(ctx, updated, input.ExpectedVersion, ev)
}
func (s *Service) SetStatus(ctx context.Context, id string, input StatusInput) (Organisation, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return Organisation{}, err
	}
	updated, err := ApplyStatus(current, input, s.clock())
	if err != nil {
		return Organisation{}, err
	}
	ev, err := NewEvent(id, updated.Version, "STATUS_CHANGED", input.ActorID, input.Reason, current.Status, updated.Status, s.clock())
	if err != nil {
		return Organisation{}, err
	}
	return s.repository.Update(ctx, updated, input.ExpectedVersion, ev)
}
func (s *Service) ListEvents(ctx context.Context, id string) ([]Event, error) {
	return s.repository.ListEvents(ctx, id)
}

type MemoryRepository struct {
	mu     sync.RWMutex
	items  map[string]Organisation
	events map[string][]Event
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: map[string]Organisation{}, events: map[string][]Event{}}
}
func (r *MemoryRepository) Create(_ context.Context, e Organisation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	fp := normalisedFingerprint(e)
	for _, x := range r.items {
		if x.Status != StatusClosed && normalisedFingerprint(x) == fp {
			return ErrDuplicate
		}
	}
	r.items[e.ID] = e
	return nil
}
func (r *MemoryRepository) List(_ context.Context) ([]Organisation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Organisation, 0, len(r.items))
	for _, e := range r.items {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}
func (r *MemoryRepository) ListPage(_ context.Context, limit int, before *time.Time, beforeID string) ([]Organisation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Organisation, 0, len(r.items))
	for _, e := range r.items {
		if before != nil && (e.CreatedAt.After(*before) || e.CreatedAt.Equal(*before) && e.ID >= beforeID) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r *MemoryRepository) Get(_ context.Context, id string) (Organisation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.items[id]
	if !ok {
		return Organisation{}, ErrNotFound
	}
	return e, nil
}
func (r *MemoryRepository) Update(_ context.Context, e Organisation, expected int64, ev Event) (Organisation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.items[e.ID]
	if !ok {
		return Organisation{}, ErrNotFound
	}
	if cur.Version != expected {
		return Organisation{}, ErrConflict
	}
	fp := normalisedFingerprint(e)
	for id, x := range r.items {
		if id != e.ID && x.Status != StatusClosed && normalisedFingerprint(x) == fp {
			return Organisation{}, ErrDuplicate
		}
	}
	r.items[e.ID] = e
	r.events[e.ID] = append(r.events[e.ID], ev)
	return e, nil
}
func (r *MemoryRepository) ListEvents(_ context.Context, id string) ([]Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.items[id]; !ok {
		return nil, ErrNotFound
	}
	return append([]Event(nil), r.events[id]...), nil
}
func (r *MemoryRepository) ListEventPage(_ context.Context, id string, limit int, afterVersion int64) ([]Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.items[id]; !ok {
		return nil, ErrNotFound
	}
	out := make([]Event, 0, limit)
	for _, event := range r.events[id] {
		if event.Version <= afterVersion {
			continue
		}
		out = append(out, event)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func normalisedFingerprint(e Organisation) string {
	return strings.ToLower(strings.Join(strings.Fields(e.LegalName), " ")) + "\x1f" + strings.ToUpper(strings.TrimSpace(e.CountryISO2))
}
