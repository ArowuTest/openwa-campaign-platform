package importer

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

type ConflictStatus string

const (
	ConflictPending  ConflictStatus = "PENDING"
	ConflictResolved ConflictStatus = "RESOLVED"
	ConflictRejected ConflictStatus = "REJECTED"
)

type ConflictResolution string

const (
	ResolutionKeepExisting ConflictResolution = "KEEP_EXISTING"
	ResolutionUseIncoming  ConflictResolution = "USE_INCOMING"
)

type ProfileConflict struct {
	ID               string             `json:"id"`
	AudienceImportID string             `json:"audienceImportId"`
	ContactID        string             `json:"contactId,omitempty"`
	MaskedMSISDN     string             `json:"maskedMsisdn"`
	Field            string             `json:"field"`
	ExistingValue    string             `json:"existingValue,omitempty"`
	IncomingValue    string             `json:"incomingValue,omitempty"`
	Status           ConflictStatus     `json:"status"`
	Resolution       ConflictResolution `json:"resolution,omitempty"`
	Reason           string             `json:"reason,omitempty"`
	ResolvedBy       string             `json:"resolvedBy,omitempty"`
	ResolvedAt       *time.Time         `json:"resolvedAt,omitempty"`
	Version          int64              `json:"version"`
	CreatedAt        time.Time          `json:"createdAt"`
}

var (
	ErrConflictNotFound = errors.New("audience profile conflict not found")
	ErrConflictVersion  = errors.New("audience profile conflict version conflict")
	ErrConflictState    = errors.New("audience profile conflict is not pending")
)

type ConflictRepository interface {
	List(context.Context, string, ConflictStatus, int) ([]ProfileConflict, error)
	Resolve(context.Context, string, ConflictResolution, string, string, int64, time.Time) (ProfileConflict, error)
}

type ConflictService struct {
	Repository ConflictRepository
	Clock      func() time.Time
}

func (s *ConflictService) List(ctx context.Context, importID string, status ConflictStatus, limit int) ([]ProfileConflict, error) {
	if s == nil || s.Repository == nil {
		return nil, errors.New("conflict repository is required")
	}
	if strings.TrimSpace(importID) == "" {
		return nil, errors.New("import ID is required")
	}
	if status == "" {
		status = ConflictPending
	}
	if status != ConflictPending && status != ConflictResolved && status != ConflictRejected {
		return nil, errors.New("invalid conflict status")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.Repository.List(ctx, importID, status, limit)
}

func (s *ConflictService) Resolve(ctx context.Context, conflictID string, resolution ConflictResolution, reason, actor string, expectedVersion int64) (ProfileConflict, error) {
	if s == nil || s.Repository == nil {
		return ProfileConflict{}, errors.New("conflict repository is required")
	}
	if strings.TrimSpace(conflictID) == "" || strings.TrimSpace(actor) == "" {
		return ProfileConflict{}, errors.New("conflict ID and actor are required")
	}
	if resolution != ResolutionKeepExisting && resolution != ResolutionUseIncoming {
		return ProfileConflict{}, errors.New("invalid conflict resolution")
	}
	if len(strings.TrimSpace(reason)) < 8 {
		return ProfileConflict{}, errors.New("resolution reason must contain at least 8 characters")
	}
	if expectedVersion <= 0 {
		return ProfileConflict{}, errors.New("expected version is required")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	return s.Repository.Resolve(ctx, conflictID, resolution, strings.TrimSpace(reason), strings.TrimSpace(actor), expectedVersion, now)
}

type MemoryConflictRepository struct {
	mu    sync.Mutex
	items map[string]ProfileConflict
}

func NewMemoryConflictRepository() *MemoryConflictRepository {
	return &MemoryConflictRepository{items: map[string]ProfileConflict{}}
}

func (r *MemoryConflictRepository) Add(input ProfileConflict) ProfileConflict {
	r.mu.Lock()
	defer r.mu.Unlock()
	if input.ID == "" {
		input.ID, _ = id.New()
	}
	if input.Version == 0 {
		input.Version = 1
	}
	if input.Status == "" {
		input.Status = ConflictPending
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = time.Now().UTC()
	}
	r.items[input.ID] = input
	return input
}

func (r *MemoryConflictRepository) List(_ context.Context, importID string, status ConflictStatus, limit int) ([]ProfileConflict, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ProfileConflict, 0)
	for _, value := range r.items {
		if value.AudienceImportID == importID && value.Status == status {
			out = append(out, value)
			if len(out) == limit {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *MemoryConflictRepository) Resolve(_ context.Context, conflictID string, resolution ConflictResolution, reason, actor string, expectedVersion int64, now time.Time) (ProfileConflict, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.items[conflictID]
	if !ok {
		return ProfileConflict{}, ErrConflictNotFound
	}
	if value.Version != expectedVersion {
		return ProfileConflict{}, ErrConflictVersion
	}
	if value.Status != ConflictPending {
		return ProfileConflict{}, ErrConflictState
	}
	value.Resolution = resolution
	value.Reason = reason
	value.ResolvedBy = actor
	value.Status = ConflictResolved
	value.ResolvedAt = &now
	value.Version++
	r.items[conflictID] = value
	return value, nil
}
