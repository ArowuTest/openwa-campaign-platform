package commercial

import (
	"context"
	"errors"
	"math"
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

type Event struct {
	ID                 string         `json:"id"`
	CommercialRecordID string         `json:"commercialRecordId"`
	EventType          string         `json:"eventType"`
	ActorID            string         `json:"actorId"`
	Reason             string         `json:"reason"`
	RecordVersion      int64          `json:"recordVersion"`
	Evidence           map[string]any `json:"evidence,omitempty"`
	OccurredAt         time.Time      `json:"occurredAt"`
}

type Store interface {
	Create(context.Context, Record) (Record, error)
	Get(context.Context, string) (Record, error)
	GetByCampaign(context.Context, string) (Record, error)
	List(context.Context, string) ([]Record, error)
	CompareAndSwap(context.Context, Record, int64) (Record, error)
}

type governedEventStore interface {
	CreateWithEvent(context.Context, Record, Event) (Record, error)
	CompareAndSwapWithEvent(context.Context, Record, int64, Event) (Record, error)
	ListEvents(context.Context, string) ([]Event, error)
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

func commercialEvidence(record Record) map[string]any {
	evidence := map[string]any{
		"campaignId":         record.CampaignID,
		"organisationId":     record.OrganisationID,
		"quotationReference": record.QuotationReference,
		"invoiceReference":   record.InvoiceReference,
		"currency":           record.Currency,
		"approvedRecipients": record.ApprovedRecipients,
		"unitPriceMinor":     record.UnitPriceMinor,
		"managementFeeMinor": record.ManagementFeeMinor,
		"totalAmountMinor":   record.TotalAmountMinor,
		"paymentReference":   record.PaymentReference,
		"status":             record.Status,
		"createdBy":          record.CreatedBy,
		"submittedBy":        record.SubmittedBy,
		"approvedBy":         record.ApprovedBy,
	}
	if record.PaymentReceivedAt != nil {
		evidence["paymentReceivedAt"] = record.PaymentReceivedAt.UTC().Format(time.RFC3339Nano)
	}
	return evidence
}

func newEvent(record Record, eventType, actor, reason string, now time.Time) (Event, error) {
	eventID, err := id.New()
	if err != nil {
		return Event{}, err
	}
	return Event{
		ID: eventID, CommercialRecordID: record.ID, EventType: eventType,
		ActorID: strings.TrimSpace(actor), Reason: strings.TrimSpace(reason),
		RecordVersion: record.Version, Evidence: commercialEvidence(record), OccurredAt: now.UTC(),
	}, nil
}

func (s *Service) persistCreate(ctx context.Context, record Record, event Event) (Record, error) {
	store, ok := s.Store.(governedEventStore)
	if !ok {
		return Record{}, errors.New("commercial event history is required")
	}
	return store.CreateWithEvent(ctx, record, event)
}

func (s *Service) persistChange(ctx context.Context, record Record, expected int64, event Event) (Record, error) {
	store, ok := s.Store.(governedEventStore)
	if !ok {
		return Record{}, errors.New("commercial event history is required")
	}
	return store.CompareAndSwapWithEvent(ctx, record, expected, event)
}

func validCurrencyCode(value string) bool {
	if len(value) != 3 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 'A' || value[index] > 'Z' {
			return false
		}
	}
	return true
}

func validReason(value string) bool {
	length := len(strings.TrimSpace(value))
	return length >= 5 && length <= 1000
}

func normalizeRecord(r Record) Record {
	r.CampaignID = strings.TrimSpace(r.CampaignID)
	r.OrganisationID = strings.TrimSpace(r.OrganisationID)
	r.QuotationReference = strings.TrimSpace(r.QuotationReference)
	r.InvoiceReference = strings.TrimSpace(r.InvoiceReference)
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	r.PaymentReference = strings.TrimSpace(r.PaymentReference)
	return r
}

func DeriveTotal(approvedRecipients, unitPriceMinor, managementFeeMinor int64) (int64, error) {
	if approvedRecipients <= 0 || unitPriceMinor < 0 || managementFeeMinor < 0 {
		return 0, ErrInvalid
	}
	if unitPriceMinor > 0 && approvedRecipients > math.MaxInt64/unitPriceMinor {
		return 0, ErrInvalid
	}
	subtotal := approvedRecipients * unitPriceMinor
	if managementFeeMinor > math.MaxInt64-subtotal {
		return 0, ErrInvalid
	}
	return subtotal + managementFeeMinor, nil
}

func validate(r Record) error {
	if strings.TrimSpace(r.CampaignID) == "" || strings.TrimSpace(r.OrganisationID) == "" || strings.TrimSpace(r.QuotationReference) == "" || strings.TrimSpace(r.InvoiceReference) == "" {
		return ErrInvalid
	}
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	if !validCurrencyCode(r.Currency) {
		return ErrInvalid
	}
	expected, err := DeriveTotal(r.ApprovedRecipients, r.UnitPriceMinor, r.ManagementFeeMinor)
	if err != nil || r.TotalAmountMinor != expected {
		return ErrInvalid
	}
	return nil
}
func (s *Service) CreateDraft(ctx context.Context, r Record, actor, reason string) (Record, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(actor) == "" || !validReason(reason) {
		return Record{}, ErrInvalid
	}
	r = normalizeRecord(r)
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
	event, err := newEvent(r, "CREATED", actor, reason, now)
	if err != nil {
		return Record{}, err
	}
	return s.persistCreate(ctx, r, event)
}

func (s *Service) UpdateDraft(ctx context.Context, identifier string, expected int64, input Record, actor, reason string) (Record, error) {
	current, err := s.Store.Get(ctx, identifier)
	if err != nil {
		return Record{}, err
	}
	if current.Version != expected {
		return Record{}, ErrConflict
	}
	if current.Status != StatusDraft && current.Status != StatusRejected && current.Status != StatusRevoked {
		return Record{}, ErrInvalid
	}
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if actor == "" || !validReason(reason) {
		return Record{}, ErrInvalid
	}
	input.ID = current.ID
	input.CampaignID = current.CampaignID
	input.OrganisationID = current.OrganisationID
	input = normalizeRecord(input)
	if err := validate(input); err != nil {
		return Record{}, err
	}
	current.QuotationReference = strings.TrimSpace(input.QuotationReference)
	current.InvoiceReference = strings.TrimSpace(input.InvoiceReference)
	current.Currency = input.Currency
	current.ApprovedRecipients = input.ApprovedRecipients
	current.UnitPriceMinor = input.UnitPriceMinor
	current.ManagementFeeMinor = input.ManagementFeeMinor
	current.TotalAmountMinor = input.TotalAmountMinor
	current.PaymentReference = strings.TrimSpace(input.PaymentReference)
	current.PaymentReceivedAt = input.PaymentReceivedAt
	current.Status = StatusDraft
	current.SubmittedBy = ""
	current.ApprovedBy = ""
	current.Reason = reason
	current.Version++
	current.UpdatedAt = s.now()
	event, err := newEvent(current, "UPDATED", actor, reason, current.UpdatedAt)
	if err != nil {
		return Record{}, err
	}
	return s.persistChange(ctx, current, expected, event)
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
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if actor == "" || !validReason(reason) {
		return Record{}, ErrInvalid
	}
	r.Status = StatusPending
	r.SubmittedBy = actor
	r.ApprovedBy = ""
	r.Reason = reason
	r.Version++
	r.UpdatedAt = s.now()
	event, err := newEvent(r, "SUBMITTED", actor, reason, r.UpdatedAt)
	if err != nil {
		return Record{}, err
	}
	return s.persistChange(ctx, r, expected, event)
}
func (s *Service) Decide(ctx context.Context, identifier string, expected int64, approve bool, actor, reason string) (Record, error) {
	r, err := s.Store.Get(ctx, identifier)
	if err != nil {
		return Record{}, err
	}
	if r.Version != expected {
		return Record{}, ErrConflict
	}
	if r.Status != StatusPending {
		return Record{}, ErrInvalid
	}
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if actor == "" || !validReason(reason) {
		return Record{}, ErrInvalid
	}
	if actor == r.CreatedBy || actor == r.SubmittedBy {
		return Record{}, ErrMakerChecker
	}
	now := s.now()
	eventType := "REJECTED"
	if approve {
		if strings.TrimSpace(r.PaymentReference) == "" || r.PaymentReceivedAt == nil || r.PaymentReceivedAt.After(now) {
			return Record{}, ErrInvalid
		}
		r.Status = StatusApproved
		r.ApprovedBy = actor
		eventType = "APPROVED"
	} else {
		r.Status = StatusRejected
	}
	r.Reason = strings.TrimSpace(reason)
	r.Version++
	r.UpdatedAt = now
	event, err := newEvent(r, eventType, actor, reason, r.UpdatedAt)
	if err != nil {
		return Record{}, err
	}
	return s.persistChange(ctx, r, expected, event)
}
func (s *Service) Revoke(ctx context.Context, identifier string, expected int64, actor, reason string) (Record, error) {
	r, err := s.Store.Get(ctx, identifier)
	if err != nil {
		return Record{}, err
	}
	if r.Version != expected {
		return Record{}, ErrConflict
	}
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if r.Status != StatusApproved || actor == "" || !validReason(reason) {
		return Record{}, ErrInvalid
	}
	r.Status = StatusRevoked
	r.Reason = reason
	r.Version++
	r.UpdatedAt = s.now()
	event, err := newEvent(r, "REVOKED", actor, reason, r.UpdatedAt)
	if err != nil {
		return Record{}, err
	}
	return s.persistChange(ctx, r, expected, event)
}
func (s *Service) GetByCampaign(ctx context.Context, campaignID string) (Record, error) {
	return s.Store.GetByCampaign(ctx, strings.TrimSpace(campaignID))
}
func (s *Service) Events(ctx context.Context, identifier string) ([]Event, error) {
	store, ok := s.Store.(governedEventStore)
	if !ok {
		return nil, errors.New("commercial event history is unavailable")
	}
	return store.ListEvents(ctx, strings.TrimSpace(identifier))
}
func (s *Service) ValidateCampaignApproval(ctx context.Context, campaignID, organisationID string, maxRecipients int64) (string, error) {
	r, err := s.Store.GetByCampaign(ctx, campaignID)
	if err != nil {
		return "", err
	}
	if r.Status != StatusApproved ||
		r.OrganisationID != organisationID ||
		r.ApprovedRecipients < maxRecipients ||
		strings.TrimSpace(r.PaymentReference) == "" ||
		r.PaymentReceivedAt == nil ||
		r.PaymentReceivedAt.After(s.now()) {
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
	events     map[string][]Event
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: map[string]Record{}, byCampaign: map[string]string{}, events: map[string][]Event{}}
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
func (m *MemoryStore) CreateWithEvent(_ context.Context, r Record, event Event) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byCampaign[r.CampaignID]; ok {
		return Record{}, ErrConflict
	}
	m.items[r.ID] = r
	m.byCampaign[r.CampaignID] = r.ID
	m.events[r.ID] = append(m.events[r.ID], event)
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
func (m *MemoryStore) ListPage(_ context.Context, org string, limit int, before *time.Time, beforeID string) ([]Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Record, 0, len(m.items))
	for _, r := range m.items {
		if org != "" && r.OrganisationID != org {
			continue
		}
		if before != nil && !(r.CreatedAt.Before(*before) || (r.CreatedAt.Equal(*before) && r.ID < beforeID)) {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
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

func (m *MemoryStore) CompareAndSwapWithEvent(_ context.Context, r Record, e int64, event Event) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.items[r.ID]
	if !ok {
		return Record{}, ErrNotFound
	}
	if cur.Version != e || r.Version != e+1 || event.CommercialRecordID != r.ID || event.RecordVersion != r.Version {
		return Record{}, ErrConflict
	}
	m.items[r.ID] = r
	m.events[r.ID] = append(m.events[r.ID], event)
	return r, nil
}

func (m *MemoryStore) ListEvents(_ context.Context, identifier string) ([]Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.items[identifier]; !ok {
		return nil, ErrNotFound
	}
	out := append([]Event(nil), m.events[identifier]...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].RecordVersion == out[j].RecordVersion {
			return out[i].ID < out[j].ID
		}
		return out[i].RecordVersion < out[j].RecordVersion
	})
	return out, nil
}

func (m *MemoryStore) EventPage(_ context.Context, identifier string, limit int, afterVersion int64, afterID string) ([]Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.items[identifier]; !ok {
		return nil, ErrNotFound
	}
	out := make([]Event, 0, len(m.events[identifier]))
	for _, event := range m.events[identifier] {
		if afterVersion > 0 && !(event.RecordVersion > afterVersion || (event.RecordVersion == afterVersion && event.ID > afterID)) {
			continue
		}
		out = append(out, event)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RecordVersion == out[j].RecordVersion {
			return out[i].ID < out[j].ID
		}
		return out[i].RecordVersion < out[j].RecordVersion
	})
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}
