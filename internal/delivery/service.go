package delivery

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrRecipientNotFound       = errors.New("campaign recipient not found")
	ErrEventDedupMismatch      = errors.New("delivery event deduplication key was reused with different content")
	ErrCampaignNotDispatchable = errors.New("campaign is not dispatchable at submission boundary")
)

type Repository interface {
	Create(context.Context, Recipient) error
	Get(context.Context, string) (Recipient, error)
	GetByProviderMessageID(context.Context, string) (Recipient, error)
	ApplyEvent(context.Context, string, Event) (Recipient, bool, error)
	ResolveReconciliation(context.Context, string, Status, Status, string, string, string, string, time.Time) (Recipient, error)
}
type TransactionExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}
type ReconciliationEvidenceWriter func(context.Context, TransactionExecer) error
type ReconciliationAtomicEvidenceRepository interface {
	ResolveReconciliationWithEvidence(context.Context, string, Status, Status, string, string, string, string, time.Time, ReconciliationEvidenceWriter) (Recipient, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) *Service { return &Service{repository: repository} }
func (s *Service) Create(ctx context.Context, recipient Recipient) error {
	return s.repository.Create(ctx, recipient)
}
func (s *Service) Get(ctx context.Context, id string) (Recipient, error) {
	return s.repository.Get(ctx, id)
}
func (s *Service) GetByProviderMessageID(ctx context.Context, providerMessageID string) (Recipient, error) {
	return s.repository.GetByProviderMessageID(ctx, providerMessageID)
}
func (s *Service) ApplyEvent(ctx context.Context, id string, event Event) (Recipient, bool, error) {
	return s.repository.ApplyEvent(ctx, id, event)
}
func (s *Service) ResolveReconciliation(ctx context.Context, id string, expected, resolved Status, actor, action, evidence, reason string, now time.Time) (Recipient, error) {
	return s.repository.ResolveReconciliation(ctx, id, expected, resolved, actor, action, evidence, reason, now)
}

func validateReconciliationResolution(current Recipient, expected, resolved Status, action string) error {
	if current.Status != expected {
		return errors.New("recipient status changed during reconciliation")
	}
	if !current.ReconciliationRequired {
		return errors.New("recipient is not awaiting reconciliation")
	}
	action = strings.TrimSpace(action)
	switch action {
	case "CONFIRM_SENT":
		if acknowledgementRank(resolved) < acknowledgementRank(StatusSent) || acknowledgementRank(resolved) < acknowledgementRank(expected) {
			return errors.New("confirmed sent resolution cannot downgrade delivery evidence")
		}
	case "CONFIRM_DELIVERED":
		if acknowledgementRank(resolved) < acknowledgementRank(StatusDelivered) || acknowledgementRank(resolved) < acknowledgementRank(expected) {
			return errors.New("confirmed delivered resolution cannot downgrade delivery evidence")
		}
	case "CONFIRM_READ":
		if resolved != StatusRead {
			return errors.New("confirmed read resolution must resolve to READ")
		}
	case "MARK_FAILED_PERMANENT":
		if resolved != StatusFailedPermanent || acknowledgementRank(expected) >= 0 {
			return errors.New("permanent failure resolution cannot overwrite provider acknowledgement")
		}
	case "CONFIRM_NOT_SUBMITTED":
		if expected != StatusUnknown || resolved != StatusFailedRetryable {
			return errors.New("safe retry resolution requires UNKNOWN to FAILED_RETRYABLE transition")
		}
	default:
		return errors.New("unsupported reconciliation resolution action")
	}
	return nil
}
func (s *Service) ResolveReconciliationWithEvidence(ctx context.Context, id string, expected, resolved Status, actor, action, evidence, reason string, now time.Time, writer ReconciliationEvidenceWriter) (Recipient, bool, error) {
	if repository, ok := s.repository.(ReconciliationAtomicEvidenceRepository); ok {
		value, err := repository.ResolveReconciliationWithEvidence(ctx, id, expected, resolved, actor, action, evidence, reason, now, writer)
		return value, true, err
	}
	value, err := s.repository.ResolveReconciliation(ctx, id, expected, resolved, actor, action, evidence, reason, now)
	return value, false, err
}

type eventIdentity struct {
	RecipientID string
	Fingerprint string
}

type MemoryRepository struct {
	mu         sync.Mutex
	recipients map[string]Recipient
	eventKeys  map[string]eventIdentity
}

func NewMemoryRepository(recipients ...Recipient) *MemoryRepository {
	r := &MemoryRepository{recipients: make(map[string]Recipient), eventKeys: make(map[string]eventIdentity)}
	for _, v := range recipients {
		r.recipients[v.ID] = cloneRecipient(v)
	}
	return r
}
func (r *MemoryRepository) Create(_ context.Context, recipient Recipient) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.recipients[recipient.ID]; ok {
		return errors.New("recipient already exists")
	}
	r.recipients[recipient.ID] = cloneRecipient(recipient)
	return nil
}
func (r *MemoryRepository) Get(_ context.Context, id string) (Recipient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.recipients[id]
	if !ok {
		return Recipient{}, ErrRecipientNotFound
	}
	return cloneRecipient(v), nil
}
func (r *MemoryRepository) GetByProviderMessageID(_ context.Context, providerMessageID string) (Recipient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.recipients {
		if v.ProviderMessageID == providerMessageID && providerMessageID != "" {
			return cloneRecipient(v), nil
		}
	}
	return Recipient{}, ErrRecipientNotFound
}
func (r *MemoryRepository) ApplyEvent(_ context.Context, id string, event Event) (Recipient, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	recipient, ok := r.recipients[id]
	if !ok {
		return Recipient{}, false, ErrRecipientNotFound
	}
	if existing, exists := r.eventKeys[event.DeduplicationKey]; exists {
		if existing.RecipientID != id {
			return Recipient{}, false, errors.New("delivery event key belongs to another recipient")
		}
		if existing.Fingerprint != Fingerprint(event) {
			return Recipient{}, false, ErrEventDedupMismatch
		}
		return cloneRecipient(recipient), false, nil
	}
	next, changed, err := Apply(recipient, event)
	if err != nil {
		return Recipient{}, false, err
	}
	r.eventKeys[event.DeduplicationKey] = eventIdentity{RecipientID: id, Fingerprint: Fingerprint(event)}
	if changed {
		r.recipients[id] = cloneRecipient(next)
	}
	return cloneRecipient(next), changed, nil
}
func cloneRecipient(input Recipient) Recipient {
	output := input
	if input.AppliedEventKeys != nil {
		output.AppliedEventKeys = make(map[string]struct{}, len(input.AppliedEventKeys))
		for k := range input.AppliedEventKeys {
			output.AppliedEventKeys[k] = struct{}{}
		}
	}
	return output
}
func (r *MemoryRepository) ResolveReconciliation(_ context.Context, id string, expected, resolved Status, actor, action, evidence, reason string, now time.Time) (Recipient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.recipients[id]
	if !ok {
		return Recipient{}, ErrRecipientNotFound
	}
	if err := validateReconciliationResolution(v, expected, resolved, action); err != nil {
		return Recipient{}, err
	}
	if actor == "" || evidence == "" || reason == "" || now.IsZero() {
		return Recipient{}, errors.New("complete reconciliation evidence is required")
	}
	v.Status = resolved
	v.ReconciliationRequired = false
	v.LastErrorDetail = ""
	switch resolved {
	case StatusFailedRetryable, StatusFailedPermanent:
		v.LastErrorCode = "OPERATOR_RECONCILIATION"
	default:
		v.LastErrorCode = ""
	}
	v.UpdatedAt = now.UTC()
	r.recipients[id] = cloneRecipient(v)
	return cloneRecipient(v), nil
}
