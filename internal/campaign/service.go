package campaign

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"campaign-platform/internal/organisation"
)

var (
	ErrNotFound  = errors.New("campaign not found")
	ErrConflict  = errors.New("campaign version conflict")
	ErrDuplicate = errors.New("campaign already exists")
)

type Repository interface {
	Create(context.Context, Campaign) error
	CompareAndSwap(context.Context, Campaign, int64) error
	Get(context.Context, string) (Campaign, error)
	List(context.Context) ([]Campaign, error)
}

type Service struct {
	repository    Repository
	organisations interface {
		Get(context.Context, string) (organisation.Organisation, error)
	}
	clock func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, clock: time.Now}
}

func (s *Service) WithOrganisationReader(reader interface {
	Get(context.Context, string) (organisation.Organisation, error)
}) *Service {
	s.organisations = reader
	return s
}

func (s *Service) requireActiveOrganisation(ctx context.Context, organisationID string) error {
	if s.organisations == nil {
		return nil
	}
	org, err := s.organisations.Get(ctx, organisationID)
	if err != nil {
		return err
	}
	if org.Status != organisation.StatusActive {
		return organisation.ErrNotActive
	}
	return nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Campaign, error) {
	if err := s.requireActiveOrganisation(ctx, input.OrganisationID); err != nil {
		return Campaign{}, err
	}
	entity, err := New(input, s.clock())
	if err != nil {
		return Campaign{}, err
	}
	if err := s.repository.Create(ctx, entity); err != nil {
		return Campaign{}, err
	}
	return entity, nil
}

func (s *Service) Transition(ctx context.Context, identifier string, input TransitionInput) (Campaign, error) {
	entity, err := s.repository.Get(ctx, identifier)
	if err != nil {
		return Campaign{}, err
	}
	if input.ExpectedVersion != entity.Version {
		return Campaign{}, ErrConflict
	}
	if input.Action != ActionPause && input.Action != ActionCancel && input.Action != ActionComplete {
		if err := s.requireActiveOrganisation(ctx, entity.OrganisationID); err != nil {
			return Campaign{}, err
		}
	}
	originalVersion := entity.Version
	entity, err = entity.Transition(input, s.clock())
	if err != nil {
		return Campaign{}, err
	}
	if err := s.repository.CompareAndSwap(ctx, entity, originalVersion); err != nil {
		return Campaign{}, err
	}
	return entity, nil
}

func (s *Service) Get(ctx context.Context, identifier string) (Campaign, error) {
	return s.repository.Get(ctx, identifier)
}

func (s *Service) List(ctx context.Context) ([]Campaign, error) {
	return s.repository.List(ctx)
}

type MemoryRepository struct {
	mu    sync.RWMutex
	items map[string]Campaign
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: make(map[string]Campaign)}
}

func (r *MemoryRepository) Create(_ context.Context, entity Campaign) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[entity.ID]; exists {
		return ErrDuplicate
	}
	r.items[entity.ID] = entity
	return nil
}

func (r *MemoryRepository) CompareAndSwap(_ context.Context, entity Campaign, expectedVersion int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.items[entity.ID]
	if !ok {
		return ErrNotFound
	}
	if current.Version != expectedVersion || entity.Version != expectedVersion+1 {
		return ErrConflict
	}
	r.items[entity.ID] = entity
	return nil
}

func (r *MemoryRepository) Get(_ context.Context, identifier string) (Campaign, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entity, ok := r.items[identifier]
	if !ok {
		return Campaign{}, ErrNotFound
	}
	return entity, nil
}

func (r *MemoryRepository) List(_ context.Context) ([]Campaign, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Campaign, 0, len(r.items))
	for _, item := range r.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}
