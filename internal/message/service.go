package message

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound            = errors.New("message version not found")
	ErrConflict            = errors.New("message version conflict")
	ErrDuplicateContent    = errors.New("identical message content already exists for campaign")
	ErrCampaignMismatch    = errors.New("message version does not belong to campaign")
	ErrApprovalStale       = errors.New("message content changed; reload before approval")
	ErrIdempotencyConflict = errors.New("message idempotency key reused with different content")
)

// Repository owns the atomic version-allocation and approval operations. Implementations
// must ensure that two concurrent draft requests cannot receive the same campaign version.
type Repository interface {
	CreateDraft(context.Context, Input, time.Time) (Version, error)
	Get(context.Context, string) (Version, error)
	ListByCampaign(context.Context, string) ([]Version, error)
	Approve(context.Context, string, string, string, time.Time) (Version, error)
}

type Service struct {
	repository Repository
	clock      func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, clock: time.Now}
}

func (s *Service) CreateDraft(ctx context.Context, input Input) (Version, error) {
	if s == nil || s.repository == nil {
		return Version{}, errors.New("message repository is required")
	}
	return s.repository.CreateDraft(ctx, input, s.clock())
}

func (s *Service) Get(ctx context.Context, identifier string) (Version, error) {
	if strings.TrimSpace(identifier) == "" {
		return Version{}, ErrNotFound
	}
	return s.repository.Get(ctx, identifier)
}

func (s *Service) ListByCampaign(ctx context.Context, campaignID string) ([]Version, error) {
	if strings.TrimSpace(campaignID) == "" {
		return nil, errors.New("campaign ID is required")
	}
	return s.repository.ListByCampaign(ctx, campaignID)
}

func (s *Service) Preview(ctx context.Context, identifier string, input RenderInput) (RenderResult, error) {
	version, err := s.Get(ctx, identifier)
	if err != nil {
		return RenderResult{}, err
	}
	input.Mode = RenderPreview
	return Render(version, input)
}

func (s *Service) RenderForDispatch(ctx context.Context, identifier string, values map[string]string) (RenderResult, error) {
	version, err := s.Get(ctx, identifier)
	if err != nil {
		return RenderResult{}, err
	}
	if version.Status != StatusApproved {
		return RenderResult{}, errors.New("only approved message versions may be rendered for dispatch")
	}
	return Render(version, RenderInput{Values: values, Mode: RenderDispatch})
}

func (s *Service) Approve(ctx context.Context, identifier, actorID, expectedContentHash string) (Version, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(expectedContentHash) == "" {
		return Version{}, errors.New("approver and expected content hash are required")
	}
	return s.repository.Approve(ctx, identifier, actorID, expectedContentHash, s.clock())
}

// MemoryRepository is a concurrency-safe behavioural implementation used by unit and
// local API tests. PostgreSQL is the production source of truth.
type MemoryRepository struct {
	mu         sync.RWMutex
	items      map[string]Version
	byCampaign map[string][]string
	byRequest  map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: make(map[string]Version), byCampaign: make(map[string][]string), byRequest: make(map[string]string)}
}

func (r *MemoryRepository) CreateDraft(_ context.Context, input Input, now time.Time) (Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	campaignID := strings.TrimSpace(input.CampaignID)
	if campaignID == "" {
		return Version{}, errors.New("campaign ID is required")
	}
	requestKey := campaignID + "\x1f" + strings.TrimSpace(input.IdempotencyKey)
	if identifier, exists := r.byRequest[requestKey]; exists {
		existing := r.items[identifier]
		input.Version = existing.Version
		candidate, err := NewDraft(input, existing.CreatedAt)
		if err != nil {
			return Version{}, err
		}
		if existing.ContentHash != candidate.ContentHash || existing.CreatedBy != strings.TrimSpace(input.CreatedBy) {
			return Version{}, ErrIdempotencyConflict
		}
		return cloneVersion(existing), nil
	}
	input.Version = len(r.byCampaign[campaignID]) + 1
	candidate, err := NewDraft(input, now)
	if err != nil {
		return Version{}, err
	}
	for _, identifier := range r.byCampaign[campaignID] {
		if r.items[identifier].ContentHash == candidate.ContentHash {
			return Version{}, ErrDuplicateContent
		}
	}
	r.items[candidate.ID] = cloneVersion(candidate)
	r.byCampaign[campaignID] = append(r.byCampaign[campaignID], candidate.ID)
	r.byRequest[requestKey] = candidate.ID
	return cloneVersion(candidate), nil
}

func (r *MemoryRepository) Get(_ context.Context, identifier string) (Version, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.items[identifier]
	if !ok {
		return Version{}, ErrNotFound
	}
	return cloneVersion(value), nil
}

func (r *MemoryRepository) ListByCampaign(_ context.Context, campaignID string) ([]Version, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	identifiers := append([]string(nil), r.byCampaign[campaignID]...)
	items := make([]Version, 0, len(identifiers))
	for _, identifier := range identifiers {
		items = append(items, cloneVersion(r.items[identifier]))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Version > items[j].Version })
	return items, nil
}

func (r *MemoryRepository) Approve(_ context.Context, identifier, actorID, expectedContentHash string, now time.Time) (Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.items[identifier]
	if !ok {
		return Version{}, ErrNotFound
	}
	if current.ContentHash != expectedContentHash {
		return Version{}, ErrApprovalStale
	}
	if current.Status == StatusApproved {
		if current.ApprovedBy == actorID {
			return cloneVersion(current), nil
		}
		return Version{}, ErrConflict
	}
	approved, err := current.Approve(actorID, now)
	if err != nil {
		return Version{}, err
	}
	r.items[identifier] = cloneVersion(approved)
	return cloneVersion(approved), nil
}

func cloneVersion(value Version) Version {
	value.Media = cloneMedia(value.Media)
	value.Links = append([]Link(nil), value.Links...)
	value.Variables = append([]Variable(nil), value.Variables...)
	return value
}
