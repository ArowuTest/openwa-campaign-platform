package consent

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/organisation"

	"campaign-platform/internal/shared/id"
)

type ReviewStatus string

const (
	StatusDraft     ReviewStatus = "DRAFT"
	StatusPending   ReviewStatus = "PENDING_REVIEW"
	StatusApproved  ReviewStatus = "APPROVED"
	StatusRejected  ReviewStatus = "REJECTED"
	StatusExpired   ReviewStatus = "EXPIRED"
	StatusSuspended ReviewStatus = "SUSPENDED"
)

type Review struct {
	ID                       string       `json:"id"`
	OrganisationID           string       `json:"organisationId"`
	Name                     string       `json:"name"`
	PurposeDescription       string       `json:"purposeDescription"`
	Channel                  string       `json:"channel"`
	ConsentSource            string       `json:"consentSource"`
	WordingVersion           string       `json:"wordingVersion"`
	EvidenceObjectKeys       []string     `json:"evidenceObjectKeys"`
	PrivacyNoticeReviewed    bool         `json:"privacyNoticeReviewed"`
	OptOutProcessReviewed    bool         `json:"optOutProcessReviewed"`
	SampleRecordsReviewed    bool         `json:"sampleRecordsReviewed"`
	PermittedCountries       []string     `json:"permittedCountries"`
	PermittedMessageCategory string       `json:"permittedMessageCategory"`
	Status                   ReviewStatus `json:"status"`
	Restrictions             string       `json:"restrictions,omitempty"`
	ReviewedBy               string       `json:"reviewedBy,omitempty"`
	ReviewedAt               *time.Time   `json:"reviewedAt,omitempty"`
	ExpiresAt                *time.Time   `json:"expiresAt,omitempty"`
	CreatedAt                time.Time    `json:"createdAt"`
	UpdatedAt                time.Time    `json:"updatedAt"`
	Version                  int64        `json:"version"`
}

type CreateInput struct {
	OrganisationID           string   `json:"organisationId"`
	Name                     string   `json:"name"`
	PurposeDescription       string   `json:"purposeDescription"`
	Channel                  string   `json:"channel"`
	ConsentSource            string   `json:"consentSource"`
	WordingVersion           string   `json:"wordingVersion"`
	EvidenceObjectKeys       []string `json:"evidenceObjectKeys"`
	PrivacyNoticeReviewed    bool     `json:"privacyNoticeReviewed"`
	OptOutProcessReviewed    bool     `json:"optOutProcessReviewed"`
	SampleRecordsReviewed    bool     `json:"sampleRecordsReviewed"`
	PermittedCountries       []string `json:"permittedCountries"`
	PermittedMessageCategory string   `json:"permittedMessageCategory"`
	Restrictions             string   `json:"restrictions"`
}

type DecisionInput struct {
	ReviewerID      string       `json:"reviewerId"`
	Decision        ReviewStatus `json:"decision"`
	ExpiresAt       *time.Time   `json:"expiresAt"`
	Reason          string       `json:"reason"`
	ExpectedVersion int64        `json:"expectedVersion"`
}

func NewReview(input CreateInput, now time.Time) (Review, error) {
	if strings.TrimSpace(input.OrganisationID) == "" {
		return Review{}, errors.New("organisation ID is required")
	}
	if strings.TrimSpace(input.Name) == "" {
		return Review{}, errors.New("review name is required")
	}
	if strings.ToUpper(strings.TrimSpace(input.Channel)) != "WHATSAPP" {
		return Review{}, errors.New("initial release supports WHATSAPP consent reviews only")
	}
	identifier, err := id.New()
	if err != nil {
		return Review{}, err
	}
	return Review{
		ID:                       identifier,
		OrganisationID:           strings.TrimSpace(input.OrganisationID),
		Name:                     strings.TrimSpace(input.Name),
		PurposeDescription:       strings.TrimSpace(input.PurposeDescription),
		Channel:                  "WHATSAPP",
		ConsentSource:            strings.TrimSpace(input.ConsentSource),
		WordingVersion:           strings.TrimSpace(input.WordingVersion),
		EvidenceObjectKeys:       append([]string(nil), input.EvidenceObjectKeys...),
		PrivacyNoticeReviewed:    input.PrivacyNoticeReviewed,
		OptOutProcessReviewed:    input.OptOutProcessReviewed,
		SampleRecordsReviewed:    input.SampleRecordsReviewed,
		PermittedCountries:       upperValues(input.PermittedCountries),
		PermittedMessageCategory: strings.TrimSpace(input.PermittedMessageCategory),
		Status:                   StatusPending,
		Restrictions:             strings.TrimSpace(input.Restrictions),
		CreatedAt:                now.UTC(),
		UpdatedAt:                now.UTC(),
		Version:                  1,
	}, nil
}

func (r Review) Decide(input DecisionInput, now time.Time) (Review, error) {
	if r.Status != StatusPending {
		return Review{}, errors.New("only pending reviews can be decided")
	}
	if input.Decision != StatusApproved && input.Decision != StatusRejected {
		return Review{}, errors.New("decision must be APPROVED or REJECTED")
	}
	if strings.TrimSpace(input.ReviewerID) == "" {
		return Review{}, errors.New("reviewer ID is required")
	}
	if input.ExpectedVersion <= 0 || input.ExpectedVersion != r.Version {
		return Review{}, ErrConflict
	}
	if input.Decision == StatusApproved {
		if !r.PrivacyNoticeReviewed || !r.OptOutProcessReviewed || !r.SampleRecordsReviewed {
			return Review{}, errors.New("all mandatory review checks must be completed before approval")
		}
		if input.ExpiresAt == nil || !input.ExpiresAt.After(now) {
			return Review{}, errors.New("approved review requires a future expiry date")
		}
	}
	decided := now.UTC()
	r.Status = input.Decision
	r.ReviewedBy = strings.TrimSpace(input.ReviewerID)
	r.ReviewedAt = &decided
	r.ExpiresAt = input.ExpiresAt
	r.Version++
	r.UpdatedAt = decided
	if strings.TrimSpace(input.Reason) != "" {
		if r.Restrictions != "" {
			r.Restrictions += "\n"
		}
		r.Restrictions += strings.TrimSpace(input.Reason)
	}
	return r, nil
}

func upperValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

var (
	ErrNotFound  = errors.New("consent review not found")
	ErrConflict  = errors.New("consent review version conflict")
	ErrDuplicate = errors.New("consent review already exists")
)

type Repository interface {
	Create(context.Context, Review) error
	CompareAndSwap(context.Context, Review, int64) error
	List(context.Context, string) ([]Review, error)
	Get(context.Context, string) (Review, error)
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

func (s *Service) Create(ctx context.Context, input CreateInput) (Review, error) {
	if s.organisations != nil {
		org, err := s.organisations.Get(ctx, input.OrganisationID)
		if err != nil {
			return Review{}, err
		}
		if org.Status != organisation.StatusActive {
			return Review{}, organisation.ErrNotActive
		}
	}
	entity, err := NewReview(input, s.clock())
	if err != nil {
		return Review{}, err
	}
	if err := s.repository.Create(ctx, entity); err != nil {
		return Review{}, err
	}
	return entity, nil
}

func (s *Service) Decide(ctx context.Context, identifier string, input DecisionInput) (Review, error) {
	entity, err := s.repository.Get(ctx, identifier)
	if err != nil {
		return Review{}, err
	}
	if input.ExpectedVersion != entity.Version {
		return Review{}, ErrConflict
	}
	if input.Decision == StatusApproved && s.organisations != nil {
		org, orgErr := s.organisations.Get(ctx, entity.OrganisationID)
		if orgErr != nil {
			return Review{}, orgErr
		}
		if org.Status != organisation.StatusActive {
			return Review{}, organisation.ErrNotActive
		}
	}
	originalVersion := entity.Version
	entity, err = entity.Decide(input, s.clock())
	if err != nil {
		return Review{}, err
	}
	if err := s.repository.CompareAndSwap(ctx, entity, originalVersion); err != nil {
		return Review{}, err
	}
	return entity, nil
}

func (s *Service) Get(ctx context.Context, identifier string) (Review, error) {
	return s.repository.Get(ctx, identifier)
}

func (s *Service) List(ctx context.Context, organisationID string) ([]Review, error) {
	return s.repository.List(ctx, organisationID)
}

type MemoryRepository struct {
	mu    sync.RWMutex
	items map[string]Review
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: make(map[string]Review)}
}

func (r *MemoryRepository) Create(_ context.Context, entity Review) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[entity.ID]; exists {
		return ErrDuplicate
	}
	r.items[entity.ID] = entity
	return nil
}

func (r *MemoryRepository) CompareAndSwap(_ context.Context, entity Review, expectedVersion int64) error {
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

func (r *MemoryRepository) List(_ context.Context, organisationID string) ([]Review, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Review, 0)
	for _, entity := range r.items {
		if organisationID == "" || entity.OrganisationID == organisationID {
			items = append(items, entity)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (r *MemoryRepository) Get(_ context.Context, identifier string) (Review, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entity, ok := r.items[identifier]
	if !ok {
		return Review{}, ErrNotFound
	}
	return entity, nil
}
