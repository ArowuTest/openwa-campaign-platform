package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

var (
	ErrReconciliationNotReady = errors.New("audience import is not ready for reconciliation closure")
	ErrReconciliationConflict = errors.New("audience import reconciliation already exists with different evidence")
)

type ReconciliationRecord struct {
	ID           string         `json:"id"`
	ImportID     string         `json:"importId"`
	Evidence     Reconciliation `json:"evidence"`
	EvidenceHash string         `json:"evidenceHash"`
	Reason       string         `json:"reason"`
	PerformedBy  string         `json:"performedBy"`
	CreatedAt    time.Time      `json:"createdAt"`
}

type ReconciliationRepository interface {
	Ensure(context.Context, ReconciliationRecord) (ReconciliationRecord, bool, error)
	GetByImport(context.Context, string) (ReconciliationRecord, error)
}

type ReconciliationService struct {
	Imports    *ImportService
	Conflicts  *ConflictService
	Repository ReconciliationRepository
	Clock      func() time.Time
}

func (s *ReconciliationService) Preview(ctx context.Context, importID string) (Reconciliation, error) {
	if s == nil || s.Imports == nil || s.Conflicts == nil {
		return Reconciliation{}, errors.New("audience import reconciliation service is not configured")
	}
	batch, err := s.Imports.Get(ctx, strings.TrimSpace(importID))
	if err != nil {
		return Reconciliation{}, err
	}
	conflicts, err := s.Conflicts.Summary(ctx, batch.ID)
	if err != nil {
		return Reconciliation{}, err
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	return BuildReconciliation(batch, conflicts, now), nil
}

func (s *ReconciliationService) Close(ctx context.Context, importID, actor, reason string) (ReconciliationRecord, bool, error) {
	if s == nil || s.Repository == nil {
		return ReconciliationRecord{}, false, errors.New("audience import reconciliation repository is not configured")
	}
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if actor == "" || len(reason) < 8 {
		return ReconciliationRecord{}, false, errors.New("actor and a reason of at least 8 characters are required")
	}
	evidence, err := s.Preview(ctx, importID)
	if err != nil {
		return ReconciliationRecord{}, false, err
	}
	if !evidence.ReadyForClosure {
		return ReconciliationRecord{}, false, ErrReconciliationNotReady
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return ReconciliationRecord{}, false, err
	}
	digest := sha256.Sum256(append([]byte("audience-import-reconciliation-v1\x00"), payload...))
	identifier, err := id.New()
	if err != nil {
		return ReconciliationRecord{}, false, err
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	record := ReconciliationRecord{ID: identifier, ImportID: evidence.ImportID, Evidence: evidence, EvidenceHash: hex.EncodeToString(digest[:]), Reason: reason, PerformedBy: actor, CreatedAt: now}
	return s.Repository.Ensure(ctx, record)
}

func (s *ReconciliationService) Get(ctx context.Context, importID string) (ReconciliationRecord, error) {
	if s == nil || s.Repository == nil {
		return ReconciliationRecord{}, errors.New("audience import reconciliation repository is not configured")
	}
	return s.Repository.GetByImport(ctx, strings.TrimSpace(importID))
}

type MemoryReconciliationRepository struct {
	mu       sync.Mutex
	byImport map[string]ReconciliationRecord
}

func NewMemoryReconciliationRepository() *MemoryReconciliationRepository {
	return &MemoryReconciliationRepository{byImport: map[string]ReconciliationRecord{}}
}

func (r *MemoryReconciliationRepository) Ensure(_ context.Context, record ReconciliationRecord) (ReconciliationRecord, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byImport[record.ImportID]; ok {
		if existing.EvidenceHash != record.EvidenceHash {
			return ReconciliationRecord{}, false, ErrReconciliationConflict
		}
		return existing, false, nil
	}
	r.byImport[record.ImportID] = record
	return record, true, nil
}
func (r *MemoryReconciliationRepository) GetByImport(_ context.Context, importID string) (ReconciliationRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.byImport[importID]
	if !ok {
		return ReconciliationRecord{}, ErrImportNotFound
	}
	return value, nil
}
