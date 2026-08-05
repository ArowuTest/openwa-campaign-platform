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
	AmendMaterial(context.Context, Campaign, MaterialChangeEvent, int64) error
	ListMaterialChanges(context.Context, string) ([]MaterialChangeEvent, error)
}

type Service struct {
	repository    Repository
	organisations interface {
		Get(context.Context, string) (organisation.Organisation, error)
	}
	policies interface {
		ValidatePurpose(context.Context, string, string) error
	}
	commercial interface {
		ValidateCampaignApproval(context.Context, string, string, int64) (string, error)
	}
	consentReviews interface {
		ValidateCampaignReview(context.Context, string, string, string, time.Time) error
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

func (s *Service) WithCommercialApprovals(reader interface {
	ValidateCampaignApproval(context.Context, string, string, int64) (string, error)
}) *Service {
	s.commercial = reader
	return s
}

func (s *Service) WithOrganisationPolicies(policies interface {
	ValidatePurpose(context.Context, string, string) error
}) *Service {
	s.policies = policies
	return s
}

func (s *Service) WithConsentReviews(reader interface {
	ValidateCampaignReview(context.Context, string, string, string, time.Time) error
}) *Service {
	s.consentReviews = reader
	return s
}

func (s *Service) validateConsentReview(ctx context.Context, entity Campaign) error {
	if s.consentReviews == nil {
		return nil
	}
	return s.consentReviews.ValidateCampaignReview(ctx, entity.ConsentReviewID, entity.OrganisationID, entity.Transport.Channel, s.clock().UTC())
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
	if s.policies != nil {
		if err := s.policies.ValidatePurpose(ctx, input.OrganisationID, input.PurposeID); err != nil {
			return Campaign{}, err
		}
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

func (s *Service) Clone(ctx context.Context, identifier string, input CloneInput) (Campaign, error) {
	source, err := s.repository.Get(ctx, identifier)
	if err != nil {
		return Campaign{}, err
	}
	if err := s.requireActiveOrganisation(ctx, source.OrganisationID); err != nil {
		return Campaign{}, err
	}
	if input.ActorID == "" {
		return Campaign{}, errors.New("actor identity is required")
	}
	clone, err := source.CloneAsDraft(input, s.clock())
	if err != nil {
		return Campaign{}, err
	}
	if err := s.repository.Create(ctx, clone); err != nil {
		return Campaign{}, err
	}
	return clone, nil
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
	switch input.Action {
	case ActionApproveConsent, ActionRequestFinalApproval, ActionApproveFinal, ActionStartDispatch, ActionResume:
		if err := s.validateConsentReview(ctx, entity); err != nil {
			return Campaign{}, err
		}
	}
	if input.Action == ActionApproveCommercial {
		if s.commercial == nil {
			return Campaign{}, errors.New("commercial approval service is required")
		}
		approvalID, validationErr := s.commercial.ValidateCampaignApproval(ctx, entity.ID, entity.OrganisationID, entity.MaximumUniqueRecipients)
		if validationErr != nil {
			return Campaign{}, validationErr
		}
		input.CommercialApprovalID = approvalID
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

func (s *Service) AmendMaterial(ctx context.Context, identifier string, input MaterialAmendmentInput) (Campaign, error) {
	entity, err := s.repository.Get(ctx, identifier)
	if err != nil {
		return Campaign{}, err
	}
	if input.ExpectedVersion != entity.Version {
		return Campaign{}, ErrConflict
	}
	if err := s.requireActiveOrganisation(ctx, entity.OrganisationID); err != nil {
		return Campaign{}, err
	}
	amended, event, err := entity.AmendMaterial(input, s.clock())
	if err != nil {
		return Campaign{}, err
	}
	if err := s.repository.AmendMaterial(ctx, amended, event, entity.Version); err != nil {
		return Campaign{}, err
	}
	return amended, nil
}

func (s *Service) ListMaterialChanges(ctx context.Context, identifier string) ([]MaterialChangeEvent, error) {
	if _, err := s.repository.Get(ctx, identifier); err != nil {
		return nil, err
	}
	return s.repository.ListMaterialChanges(ctx, identifier)
}

func (s *Service) Get(ctx context.Context, identifier string) (Campaign, error) {
	return s.repository.Get(ctx, identifier)
}

func (s *Service) List(ctx context.Context) ([]Campaign, error) {
	return s.repository.List(ctx)
}

type MemoryRepository struct {
	mu     sync.RWMutex
	items  map[string]Campaign
	events map[string][]MaterialChangeEvent
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: make(map[string]Campaign), events: make(map[string][]MaterialChangeEvent)}
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

func (r *MemoryRepository) AmendMaterial(_ context.Context, entity Campaign, event MaterialChangeEvent, expectedVersion int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.items[entity.ID]
	if !ok {
		return ErrNotFound
	}
	if current.Version != expectedVersion || entity.Version != expectedVersion+1 {
		return ErrConflict
	}
	event.Sequence = int64(len(r.events[entity.ID]) + 1)
	r.items[entity.ID] = entity
	r.events[entity.ID] = append(r.events[entity.ID], event)
	return nil
}

func (r *MemoryRepository) ListMaterialChanges(_ context.Context, campaignID string) ([]MaterialChangeEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.items[campaignID]; !ok {
		return nil, ErrNotFound
	}
	items := append([]MaterialChangeEvent(nil), r.events[campaignID]...)
	return items, nil
}
