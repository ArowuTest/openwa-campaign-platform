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

func (r *MemoryConflictRepository) ListConflictPage(_ context.Context, importID string, status ConflictStatus, limit int, after *time.Time, afterID string) ([]ProfileConflict, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ProfileConflict, 0)
	for _, value := range r.items {
		if value.AudienceImportID != importID || value.Status != status {
			continue
		}
		if after != nil && (value.CreatedAt.Before(*after) || (value.CreatedAt.Equal(*after) && value.ID <= afterID)) {
			continue
		}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
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

type ConflictSummary struct {
	Pending  int64 `json:"pending"`
	Resolved int64 `json:"resolved"`
	Rejected int64 `json:"rejected"`
	Total    int64 `json:"total"`
}

type Reconciliation struct {
	ImportID             string          `json:"importId"`
	Status               ImportStatus    `json:"status"`
	UploadedRows         int64           `json:"uploadedRows"`
	ValidRows            int64           `json:"validRows"`
	InvalidRows          int64           `json:"invalidRows"`
	DuplicateRows        int64           `json:"duplicateRows"`
	SuppressedRows       int64           `json:"suppressedRows"`
	InsertedContacts     int64           `json:"insertedContacts"`
	UpdatedContacts      int64           `json:"updatedContacts"`
	Conflicts            ConflictSummary `json:"conflicts"`
	ValidationAccounted  int64           `json:"validationAccounted"`
	ValidationBalanced   bool            `json:"validationBalanced"`
	MergeAccounted       int64           `json:"mergeAccounted"`
	MergeWithinValidRows bool            `json:"mergeWithinValidRows"`
	ReadyForClosure      bool            `json:"readyForClosure"`
	Warnings             []string        `json:"warnings"`
	CalculatedAt         time.Time       `json:"calculatedAt"`
}

type ConflictSummaryRepository interface {
	Summary(context.Context, string) (ConflictSummary, error)
}

func (s *ConflictService) Summary(ctx context.Context, importID string) (ConflictSummary, error) {
	if s == nil || s.Repository == nil {
		return ConflictSummary{}, errors.New("conflict repository is required")
	}
	provider, ok := s.Repository.(ConflictSummaryRepository)
	if !ok {
		return ConflictSummary{}, errors.New("conflict summary is unavailable")
	}
	if strings.TrimSpace(importID) == "" {
		return ConflictSummary{}, errors.New("import ID is required")
	}
	return provider.Summary(ctx, strings.TrimSpace(importID))
}

func (r *MemoryConflictRepository) Summary(_ context.Context, importID string) (ConflictSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result ConflictSummary
	for _, value := range r.items {
		if value.AudienceImportID != importID {
			continue
		}
		switch value.Status {
		case ConflictPending:
			result.Pending++
		case ConflictResolved:
			result.Resolved++
		case ConflictRejected:
			result.Rejected++
		}
		result.Total++
	}
	return result, nil
}

func BuildReconciliation(batch ImportBatch, conflicts ConflictSummary, now time.Time) Reconciliation {
	validationAccounted := batch.ValidRows + batch.InvalidRows + batch.DuplicateRows + batch.SuppressedRows
	mergeAccounted := batch.InsertedContacts + batch.UpdatedContacts
	result := Reconciliation{
		ImportID: batch.ID, Status: batch.Status, UploadedRows: batch.UploadedRows,
		ValidRows: batch.ValidRows, InvalidRows: batch.InvalidRows, DuplicateRows: batch.DuplicateRows,
		SuppressedRows: batch.SuppressedRows, InsertedContacts: batch.InsertedContacts,
		UpdatedContacts: batch.UpdatedContacts, Conflicts: conflicts,
		ValidationAccounted: validationAccounted,
		ValidationBalanced:  batch.UploadedRows == validationAccounted,
		MergeAccounted:      mergeAccounted, MergeWithinValidRows: mergeAccounted <= batch.ValidRows,
		Warnings: []string{}, CalculatedAt: now.UTC(),
	}
	if !result.ValidationBalanced {
		result.Warnings = append(result.Warnings, "VALIDATION_ROW_TOTAL_MISMATCH")
	}
	if !result.MergeWithinValidRows {
		result.Warnings = append(result.Warnings, "MERGE_TOTAL_EXCEEDS_VALID_ROWS")
	}
	if conflicts.Pending > 0 {
		result.Warnings = append(result.Warnings, "PENDING_PROFILE_CONFLICTS")
	}
	if batch.Status == ImportCompleted && conflicts.Pending > 0 {
		result.Warnings = append(result.Warnings, "COMPLETED_IMPORT_HAS_PENDING_CONFLICTS")
	}
	result.ReadyForClosure = result.ValidationBalanced && result.MergeWithinValidRows && conflicts.Pending == 0 && (batch.Status == ImportCompleted || batch.Status == ImportCompletedWithExceptions)
	return result
}

type ConflictDecision struct {
	ConflictID      string             `json:"conflictId"`
	Resolution      ConflictResolution `json:"resolution"`
	Reason          string             `json:"reason"`
	ExpectedVersion int64              `json:"expectedVersion"`
}

type BatchResolutionResult struct {
	Resolved []ProfileConflict `json:"resolved"`
	Count    int               `json:"count"`
}

type BatchConflictRepository interface {
	ResolveBatch(context.Context, []ConflictDecision, string, time.Time) ([]ProfileConflict, error)
}

func (s *ConflictService) ResolveBatch(ctx context.Context, decisions []ConflictDecision, actor string) (BatchResolutionResult, error) {
	if s == nil || s.Repository == nil {
		return BatchResolutionResult{}, errors.New("conflict repository is required")
	}
	provider, ok := s.Repository.(BatchConflictRepository)
	if !ok {
		return BatchResolutionResult{}, errors.New("batch conflict resolution is unavailable")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return BatchResolutionResult{}, errors.New("actor is required")
	}
	if len(decisions) == 0 || len(decisions) > 100 {
		return BatchResolutionResult{}, errors.New("batch must contain between 1 and 100 decisions")
	}
	seen := map[string]struct{}{}
	clean := make([]ConflictDecision, len(decisions))
	for i, decision := range decisions {
		decision.ConflictID = strings.TrimSpace(decision.ConflictID)
		decision.Reason = strings.TrimSpace(decision.Reason)
		if decision.ConflictID == "" || decision.ExpectedVersion <= 0 || len(decision.Reason) < 8 {
			return BatchResolutionResult{}, errors.New("each decision requires conflict ID, expected version and reason of at least 8 characters")
		}
		if decision.Resolution != ResolutionKeepExisting && decision.Resolution != ResolutionUseIncoming {
			return BatchResolutionResult{}, errors.New("invalid conflict resolution")
		}
		if _, exists := seen[decision.ConflictID]; exists {
			return BatchResolutionResult{}, errors.New("batch contains duplicate conflict IDs")
		}
		seen[decision.ConflictID] = struct{}{}
		clean[i] = decision
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	resolved, err := provider.ResolveBatch(ctx, clean, actor, now)
	if err != nil {
		return BatchResolutionResult{}, err
	}
	return BatchResolutionResult{Resolved: resolved, Count: len(resolved)}, nil
}

func (r *MemoryConflictRepository) ResolveBatch(_ context.Context, decisions []ConflictDecision, actor string, now time.Time) ([]ProfileConflict, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, decision := range decisions {
		value, ok := r.items[decision.ConflictID]
		if !ok {
			return nil, ErrConflictNotFound
		}
		if value.Version != decision.ExpectedVersion {
			return nil, ErrConflictVersion
		}
		if value.Status != ConflictPending {
			return nil, ErrConflictState
		}
	}
	out := make([]ProfileConflict, 0, len(decisions))
	for _, decision := range decisions {
		value := r.items[decision.ConflictID]
		value.Resolution, value.Reason, value.ResolvedBy = decision.Resolution, decision.Reason, actor
		value.Status, value.ResolvedAt, value.Version = ConflictResolved, &now, value.Version+1
		r.items[decision.ConflictID] = value
		out = append(out, value)
	}
	return out, nil
}
