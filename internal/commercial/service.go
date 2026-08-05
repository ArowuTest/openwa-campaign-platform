package commercial

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

var (
	ErrNotFound     = errors.New("commercial record not found")
	ErrConflict     = errors.New("commercial record version conflict")
	ErrInvalid      = errors.New("commercial record is invalid")
	ErrNotApproved  = errors.New("commercial approval is not active for campaign")
	ErrMakerChecker = errors.New("commercial record maker cannot approve the same record")
)

type Status string

const (
	StatusDraft    Status = "DRAFT"
	StatusPending  Status = "PENDING_APPROVAL"
	StatusApproved Status = "APPROVED"
	StatusRejected Status = "REJECTED"
	StatusRevoked  Status = "REVOKED"
)

type Record struct {
	ID                 string     `json:"id"`
	CampaignID         string     `json:"campaignId"`
	OrganisationID     string     `json:"organisationId"`
	QuotationReference string     `json:"quotationReference"`
	InvoiceReference   string     `json:"invoiceReference"`
	Currency           string     `json:"currency"`
	ApprovedRecipients int64      `json:"approvedRecipients"`
	UnitPriceMinor     int64      `json:"unitPriceMinor"`
	ManagementFeeMinor int64      `json:"managementFeeMinor"`
	TotalAmountMinor   int64      `json:"totalAmountMinor"`
	PaymentReference   string     `json:"paymentReference"`
	PaymentReceivedAt  *time.Time `json:"paymentReceivedAt,omitempty"`
	Status             Status     `json:"status"`
	Version            int64      `json:"version"`
	CreatedBy          string     `json:"createdBy"`
	SubmittedBy        string     `json:"submittedBy,omitempty"`
	ApprovedBy         string     `json:"approvedBy,omitempty"`
	Reason             string     `json:"reason"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

type Store interface {
	Create(context.Context, Record) (Record, error)
	Get(context.Context, string) (Record, error)
	GetByCampaign(context.Context, string) (Record, error)
	List(context.Context, string) ([]Record, error)
	CompareAndSwap(context.Context, Record, int64) (Record, error)
}

type Service struct {
	Store Store
	Clock func() time.Time
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func validate(r Record) error {
	if strings.TrimSpace(r.CampaignID) == "" || strings.TrimSpace(r.OrganisationID) == "" || strings.TrimSpace(r.QuotationReference) == "" || strings.TrimSpace(r.InvoiceReference) == "" {
		return ErrInvalid
	}
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	if len(r.Currency) != 3 || r.ApprovedRecipients <= 0 || r.UnitPriceMinor < 0 || r.ManagementFeeMinor < 0 {
		return ErrInvalid
	}
	expected := r.ApprovedRecipients*r.UnitPriceMinor + r.ManagementFeeMinor
	if r.TotalAmountMinor != expected {
		return ErrInvalid
	}
	return nil
}
func (s *Service) CreateDraft(ctx context.Context, r Record, actor, reason string) (Record, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 5 {
		return Record{}, ErrInvalid
	}
	if err := validate(r); err != nil {
		return Record{}, err
	}
	key, err := id.New()
	if err != nil {
		return Record{}, err
	}
	now := s.now()
	r.ID = key
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	r.Status = StatusDraft
	r.Version = 1
	r.CreatedBy = strings.TrimSpace(actor)
	r.Reason = strings.TrimSpace(reason)
	r.CreatedAt = now
	r.UpdatedAt = now
	return s.Store.Create(ctx, r)
}
func (s *Service) Submit(ctx context.Context, identifier string, expected int64, actor, reason string) (Record, error) {
	r, err := s.Store.Get(ctx, identifier)
	if err != nil {
		return Record{}, err
	}
	if r.Version != expected {
		return Record{}, ErrConflict
	}
	if r.Status != StatusDraft && r.Status != StatusRejected {
		return Record{}, ErrInvalid
	}
	if strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 5 {
		return Record{}, ErrInvalid
	}
	r.Status = StatusPending
	r.SubmittedBy = actor
	r.ApprovedBy = ""
	r.Reason = strings.TrimSpace(reason)
	r.Version++
	r.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, r, expected)
}
func (s *Service) Decide(ctx context.Context, identifier string, expected int64, approve bool, actor, reason string) (Record, error) {
	r, err := s.Store.Get(ctx, identifier)
	if err != nil {
		return Record{}, err
	}
	if r.Version != expected {
		return Record{}, ErrConflict
	}
	if r.Status != StatusPending || strings.TrimSpace(actor) == "" || actor == r.SubmittedBy || len(strings.TrimSpace(reason)) < 5 {
		return Record{}, ErrMakerChecker
	}
	if approve {
		if strings.TrimSpace(r.PaymentReference) == "" || r.PaymentReceivedAt == nil {
			return Record{}, ErrInvalid
		}
		r.Status = StatusApproved
		r.ApprovedBy = actor
	} else {
		r.Status = StatusRejected
	}
	r.Reason = strings.TrimSpace(reason)
	r.Version++
	r.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, r, expected)
}
func (s *Service) Revoke(ctx context.Context, identifier string, expected int64, actor, reason string) (Record, error) {
	r, err := s.Store.Get(ctx, identifier)
	if err != nil {
		return Record{}, err
	}
	if r.Version != expected {
		return Record{}, ErrConflict
	}
	if r.Status != StatusApproved || strings.TrimSpace(actor) == "" || len(strings.TrimSpace(reason)) < 5 {
		return Record{}, ErrInvalid
	}
	r.Status = StatusRevoked
	r.Reason = strings.TrimSpace(reason)
	r.Version++
	r.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, r, expected)
}
func (s *Service) ValidateCampaignApproval(ctx context.Context, campaignID, organisationID string, maxRecipients int64) (string, error) {
	r, err := s.Store.GetByCampaign(ctx, campaignID)
	if err != nil {
		return "", err
	}
	if r.Status != StatusApproved || r.OrganisationID != organisationID || r.ApprovedRecipients < maxRecipients {
		return "", ErrNotApproved
	}
	return r.ID, nil
}
func (s *Service) List(ctx context.Context, organisationID string) ([]Record, error) {
	return s.Store.List(ctx, organisationID)
}

type MemoryStore struct {
	mu         sync.RWMutex
	items      map[string]Record
	byCampaign map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: map[string]Record{}, byCampaign: map[string]string{}}
}
func (m *MemoryStore) Create(_ context.Context, r Record) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byCampaign[r.CampaignID]; ok {
		return Record{}, ErrConflict
	}
	m.items[r.ID] = r
	m.byCampaign[r.CampaignID] = r.ID
	return r, nil
}
func (m *MemoryStore) Get(_ context.Context, id string) (Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.items[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}
func (m *MemoryStore) GetByCampaign(_ context.Context, c string) (Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byCampaign[c]
	if !ok {
		return Record{}, ErrNotFound
	}
	return m.items[id], nil
}
func (m *MemoryStore) List(_ context.Context, o string) ([]Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []Record{}
	for _, r := range m.items {
		if o == "" || r.OrganisationID == o {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (m *MemoryStore) CompareAndSwap(_ context.Context, r Record, e int64) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.items[r.ID]
	if !ok {
		return Record{}, ErrNotFound
	}
	if cur.Version != e || r.Version != e+1 {
		return Record{}, ErrConflict
	}
	m.items[r.ID] = r
	return r, nil
}
