package campaign

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/organisation"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/sender"
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
	ListPage(context.Context, int, *time.Time, string) ([]Campaign, error)
	AmendMaterial(context.Context, Campaign, MaterialChangeEvent, int64) error
	ListMaterialChanges(context.Context, string) ([]MaterialChangeEvent, error)
}

type Page struct {
	Items      []Campaign `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

type pageCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
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
		ValidateCampaignReview(context.Context, string, string, string, string, time.Time) error
	}
	providerCapabilities interface {
		Require(context.Context, string, provider.Channel, string, time.Time, []provider.Capability) (provider.Definition, error)
		Get(context.Context, string) (provider.Definition, error)
	}
	gatewayPools interface {
		RequireCapabilities(context.Context, string, sender.GatewayProvider, sender.GatewayEngine, []sender.Capability) (sender.GatewayPool, error)
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

func (s *Service) WithProviderCapabilities(registry interface {
	Require(context.Context, string, provider.Channel, string, time.Time, []provider.Capability) (provider.Definition, error)
	Get(context.Context, string) (provider.Definition, error)
}) *Service {
	s.providerCapabilities = registry
	return s
}

func (s *Service) WithGatewayPools(pools interface {
	RequireCapabilities(context.Context, string, sender.GatewayProvider, sender.GatewayEngine, []sender.Capability) (sender.GatewayPool, error)
}) *Service {
	s.gatewayPools = pools
	return s
}

func (s *Service) resolveProviderCapabilities(ctx context.Context, transport TransportSelection, effectiveAt time.Time) (TransportSelection, error) {
	transport.Channel = strings.ToUpper(strings.TrimSpace(transport.Channel))
	transport.Provider = Provider(strings.ToUpper(strings.TrimSpace(string(transport.Provider))))
	transport.Engine = Engine(strings.ToUpper(strings.TrimSpace(string(transport.Engine))))
	if s.providerCapabilities == nil {
		return transport, nil
	}
	required := make([]provider.Capability, 0, len(transport.RequiredCapabilities))
	gatewayRequired := make([]sender.Capability, 0, len(transport.RequiredCapabilities))
	for _, capability := range transport.RequiredCapabilities {
		required = append(required, provider.Capability(capability))
		gatewayRequired = append(gatewayRequired, sender.Capability(capability))
	}
	if effectiveAt.IsZero() {
		effectiveAt = s.clock().UTC()
	}
	definition, err := s.providerCapabilities.Require(ctx, string(transport.Provider), provider.Channel(strings.ToUpper(strings.TrimSpace(transport.Channel))), string(transport.Engine), effectiveAt.UTC(), required)
	if err != nil {
		return transport, err
	}
	if definition.AdapterVersion != strings.TrimSpace(transport.AdapterVersion) {
		return transport, errors.New("campaign adapter version does not match the active provider capability definition")
	}
	if transport.ProviderDefinitionID != "" && (transport.ProviderDefinitionID != definition.ID || transport.ProviderDefinitionVersion != definition.Version) {
		return transport, errors.New("campaign provider capability binding no longer matches the active governed definition; material reapproval is required")
	}
	transport.ProviderDefinitionID = definition.ID
	transport.ProviderDefinitionVersion = definition.Version
	if s.gatewayPools != nil && transport.Provider == ProviderOpenWA {
		pool, poolErr := s.gatewayPools.RequireCapabilities(ctx, transport.GatewayPoolID, sender.GatewayProvider(transport.Provider), sender.GatewayEngine(transport.Engine), gatewayRequired)
		if poolErr != nil {
			return transport, poolErr
		}
		if strings.TrimSpace(pool.AdapterVersion) != strings.TrimSpace(transport.AdapterVersion) {
			return transport, errors.New("gateway pool adapter version does not match the campaign provider adapter version")
		}
		if transport.GatewayPoolVersion > 0 && transport.GatewayPoolVersion != pool.Version {
			return transport, errors.New("campaign gateway pool binding no longer matches the governed pool version; material reapproval is required")
		}
		transport.GatewayPoolVersion = pool.Version
		if definition.MinimumGatewayVersion != "" {
			ok, versionErr := provider.VersionAtLeast(pool.AdapterVersion, definition.MinimumGatewayVersion)
			if versionErr != nil {
				return transport, versionErr
			}
			if !ok {
				return transport, errors.New("gateway pool version is below the governed provider minimum")
			}
		}
	}
	return transport, nil
}

func (s *Service) providerEffectiveAt(requestedStart *time.Time) time.Time {
	now := s.clock().UTC()
	if requestedStart != nil && requestedStart.After(now) {
		return requestedStart.UTC()
	}
	return now
}

func (s *Service) WithConsentReviews(reader interface {
	ValidateCampaignReview(context.Context, string, string, string, string, time.Time) error
}) *Service {
	s.consentReviews = reader
	return s
}

func (s *Service) validateConsentReview(ctx context.Context, entity Campaign) error {
	if s.consentReviews == nil {
		return nil
	}
	return s.consentReviews.ValidateCampaignReview(ctx, entity.ConsentReviewID, entity.ID, entity.OrganisationID, entity.Transport.Channel, s.clock().UTC())
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
	// Provider-definition identity is authoritative server evidence and cannot be
	// selected by the caller. It is resolved from the active governed registry.
	input.Transport.ProviderDefinitionID = ""
	input.Transport.ProviderDefinitionVersion = 0
	input.Transport.GatewayPoolVersion = 0
	transport, err := s.resolveProviderCapabilities(ctx, input.Transport, s.providerEffectiveAt(input.RequestedStartAt))
	if err != nil {
		return Campaign{}, err
	}
	input.Transport = transport
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
	if s.policies != nil {
		if err := s.policies.ValidatePurpose(ctx, clone.OrganisationID, clone.PurposeID); err != nil {
			return Campaign{}, err
		}
	}
	clone.Transport.ProviderDefinitionID = ""
	clone.Transport.ProviderDefinitionVersion = 0
	clone.Transport.GatewayPoolVersion = 0
	transport, err := s.resolveProviderCapabilities(ctx, clone.Transport, s.providerEffectiveAt(clone.RequestedStartAt))
	if err != nil {
		return Campaign{}, err
	}
	clone.Transport = transport
	if err := s.repository.Create(ctx, clone); err != nil {
		return Campaign{}, err
	}
	return clone, nil
}

func (s *Service) PrepareTransition(ctx context.Context, identifier string, input TransitionInput) (Campaign, error) {
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
	switch input.Action {
	case ActionRequestFinalApproval, ActionApproveFinal, ActionStartDispatch, ActionResume:
		transport, validationErr := s.resolveProviderCapabilities(ctx, entity.Transport, s.providerEffectiveAt(entity.RequestedStartAt))
		if validationErr != nil {
			return Campaign{}, validationErr
		}
		entity.Transport = transport
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
	entity, err = entity.Transition(input, s.clock())
	if err != nil {
		return Campaign{}, err
	}
	return entity, nil
}

func (s *Service) Transition(ctx context.Context, identifier string, input TransitionInput) (Campaign, error) {
	entity, err := s.PrepareTransition(ctx, identifier, input)
	if err != nil {
		return Campaign{}, err
	}
	if err := s.repository.CompareAndSwap(ctx, entity, input.ExpectedVersion); err != nil {
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
	if input.ChangeTransport {
		amended.Transport.ProviderDefinitionID = ""
		amended.Transport.ProviderDefinitionVersion = 0
		amended.Transport.GatewayPoolVersion = 0
		transport, validationErr := s.resolveProviderCapabilities(ctx, amended.Transport, s.providerEffectiveAt(amended.RequestedStartAt))
		if validationErr != nil {
			return Campaign{}, validationErr
		}
		amended.Transport = transport
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

func (s *Service) List(ctx context.Context, limit int, cursor string) (Page, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	before, beforeID, err := decodeCampaignPageCursor(cursor)
	if err != nil {
		return Page{}, err
	}
	items, err := s.repository.ListPage(ctx, limit+1, before, beforeID)
	if err != nil {
		return Page{}, err
	}
	return campaignPageFromItems(items, limit)
}

func decodeCampaignPageCursor(cursor string) (*time.Time, string, error) {
	if strings.TrimSpace(cursor) == "" {
		return nil, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, "", errors.New("invalid campaign page cursor")
	}
	var decoded pageCursor
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.CreatedAt.IsZero() || strings.TrimSpace(decoded.ID) == "" {
		return nil, "", errors.New("invalid campaign page cursor")
	}
	value := decoded.CreatedAt.UTC()
	return &value, strings.TrimSpace(decoded.ID), nil
}

func campaignPageFromItems(items []Campaign, limit int) (Page, error) {
	page := Page{Items: items}
	if len(items) <= limit {
		return page, nil
	}
	page.Items = items[:limit]
	last := page.Items[len(page.Items)-1]
	raw, err := json.Marshal(pageCursor{CreatedAt: last.CreatedAt.UTC(), ID: last.ID})
	if err != nil {
		return Page{}, err
	}
	page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	return page, nil
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

func (r *MemoryRepository) ListPage(_ context.Context, limit int, before *time.Time, beforeID string) ([]Campaign, error) {
	return r.listFilteredPage(ListFilter{}, limit, before, beforeID), nil
}

func (r *MemoryRepository) ListFilteredPage(_ context.Context, filter ListFilter, limit int, before *time.Time, beforeID string) ([]Campaign, error) {
	return r.listFilteredPage(filter, limit, before, beforeID), nil
}

func (r *MemoryRepository) listFilteredPage(filter ListFilter, limit int, before *time.Time, beforeID string) []Campaign {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > 501 {
		limit = 101
	}
	items := make([]Campaign, 0, len(r.items))
	for _, item := range r.items {
		if filter.OrganisationID != "" && item.OrganisationID != filter.OrganisationID {
			continue
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if before != nil && (item.CreatedAt.After(*before) || (item.CreatedAt.Equal(*before) && item.ID >= beforeID)) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
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

func (r *MemoryRepository) ListMaterialChangePage(_ context.Context, campaignID string, limit int, afterSequence int64) ([]MaterialChangeEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.items[campaignID]; !ok {
		return nil, ErrNotFound
	}
	out := make([]MaterialChangeEvent, 0, limit)
	for _, event := range r.events[campaignID] {
		if event.Sequence <= afterSequence {
			continue
		}
		out = append(out, event)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
