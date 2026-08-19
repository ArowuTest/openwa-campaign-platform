package metacloud

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

type Status string
type HealthStatus string

const (
	StatusDraft    Status = "DRAFT"
	StatusPending  Status = "PENDING_APPROVAL"
	StatusActive   Status = "ACTIVE"
	StatusRejected Status = "REJECTED"
	StatusRetired  Status = "RETIRED"

	HealthUnknown     HealthStatus = "HEALTH_UNKNOWN"
	HealthHealthy     HealthStatus = "HEALTHY"
	HealthDegraded    HealthStatus = "DEGRADED"
	HealthUnavailable HealthStatus = "UNAVAILABLE"
)

var (
	ErrNotFound = errors.New("Meta Cloud sender not found")
	ErrConflict = errors.New("Meta Cloud sender version conflict")
	ErrInvalid  = errors.New("Meta Cloud sender is invalid")
)

type Sender struct {
	ID                   string       `json:"id"`
	OrganisationID       string       `json:"organisationId"`
	SenderPoolID         string       `json:"senderPoolId"`
	WABAID               string       `json:"wabaId"`
	PhoneNumberID        string       `json:"phoneNumberId"`
	DisplayName          string       `json:"displayName"`
	BusinessPhoneDisplay string       `json:"businessPhoneDisplay"`
	CredentialKey        string       `json:"credentialKey"`
	GraphAPIVersion      string       `json:"graphApiVersion"`
	Status               Status       `json:"status"`
	Health               HealthStatus `json:"health"`
	HealthObservedAt     *time.Time   `json:"healthObservedAt,omitempty"`
	EffectiveFrom        *time.Time   `json:"effectiveFrom,omitempty"`
	EffectiveTo          *time.Time   `json:"effectiveTo,omitempty"`
	Version              int64        `json:"version"`
	CreatedBy            string       `json:"createdBy"`
	SubmittedBy          string       `json:"submittedBy,omitempty"`
	ApprovedBy           string       `json:"approvedBy,omitempty"`
	Reason               string       `json:"reason"`
	CreatedAt            time.Time    `json:"createdAt"`
	UpdatedAt            time.Time    `json:"updatedAt"`
}

type Event struct {
	ID            int64          `json:"id"`
	SenderID      string         `json:"senderId"`
	Action        string         `json:"action"`
	ActorID       string         `json:"actorId"`
	Reason        string         `json:"reason"`
	SenderVersion int64          `json:"senderVersion"`
	Evidence      map[string]any `json:"evidence,omitempty"`
	OccurredAt    time.Time      `json:"occurredAt"`
}

type Store interface {
	List(context.Context) ([]Sender, error)
	Get(context.Context, string) (Sender, error)
	Create(context.Context, Sender) (Sender, error)
	CompareAndSwap(context.Context, Sender, int64, string, string, map[string]any) (Sender, error)
	ListEvents(context.Context, string) ([]Event, error)
}

type Service struct {
	Store Store
	Clock func() time.Time
}

func (s *Service) now() time.Time {
	if s != nil && s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) CreateDraft(ctx context.Context, value Sender, actor, reason string) (Sender, error) {
	if s == nil || s.Store == nil {
		return Sender{}, errors.New("Meta Cloud sender store is required")
	}
	now := s.now()
	value.ID = ""
	value.Status = StatusDraft
	value.Health = HealthUnknown
	value.HealthObservedAt, value.EffectiveFrom, value.EffectiveTo = nil, nil, nil
	value.Version = 1
	value.CreatedBy = strings.TrimSpace(actor)
	value.SubmittedBy, value.ApprovedBy = "", ""
	value.Reason = strings.TrimSpace(reason)
	value.CreatedAt, value.UpdatedAt = now, now
	if err := validateSender(&value); err != nil {
		return Sender{}, err
	}
	generated, err := id.New()
	if err != nil {
		return Sender{}, err
	}
	value.ID = generated
	return s.Store.Create(ctx, value)
}

func (s *Service) Submit(ctx context.Context, senderID string, expected int64, actor, reason string) (Sender, error) {
	if s == nil || s.Store == nil {
		return Sender{}, errors.New("Meta Cloud sender store is required")
	}
	current, err := s.Store.Get(ctx, strings.TrimSpace(senderID))
	if err != nil {
		return Sender{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if current.Version != expected {
		return Sender{}, ErrConflict
	}
	if (current.Status != StatusDraft && current.Status != StatusRejected) || actor == "" || len(reason) < 5 {
		return Sender{}, ErrInvalid
	}
	current.Status = StatusPending
	current.SubmittedBy = actor
	current.Reason = reason
	current.Version++
	current.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, current, expected, actor, string(StatusPending), nil)
}

func (s *Service) Decide(ctx context.Context, senderID string, expected int64, approve bool, actor, reason string, effectiveFrom time.Time) (Sender, error) {
	if s == nil || s.Store == nil {
		return Sender{}, errors.New("Meta Cloud sender store is required")
	}
	current, err := s.Store.Get(ctx, strings.TrimSpace(senderID))
	if err != nil {
		return Sender{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if current.Version != expected {
		return Sender{}, ErrConflict
	}
	if current.Status != StatusPending || actor == "" || actor == current.CreatedBy || actor == current.SubmittedBy || len(reason) < 5 {
		return Sender{}, ErrInvalid
	}
	current.ApprovedBy = actor
	current.Reason = reason
	if approve {
		if effectiveFrom.IsZero() {
			effectiveFrom = s.now()
		}
		when := effectiveFrom.UTC()
		current.Status = StatusActive
		current.EffectiveFrom = &when
		current.EffectiveTo = nil
	} else {
		current.Status = StatusRejected
		current.EffectiveFrom = nil
		current.EffectiveTo = nil
	}
	current.Version++
	current.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, current, expected, actor, string(current.Status), nil)
}

func (s *Service) ObserveHealth(ctx context.Context, senderID string, expected int64, health HealthStatus, observedAt time.Time, actor, reason string) (Sender, error) {
	if s == nil || s.Store == nil {
		return Sender{}, errors.New("Meta Cloud sender store is required")
	}
	current, err := s.Store.Get(ctx, strings.TrimSpace(senderID))
	if err != nil {
		return Sender{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if current.Version != expected {
		return Sender{}, ErrConflict
	}
	if current.Status != StatusActive || actor == "" || len(reason) < 5 || observedAt.IsZero() {
		return Sender{}, ErrInvalid
	}
	switch health {
	case HealthHealthy, HealthDegraded, HealthUnavailable:
	default:
		return Sender{}, ErrInvalid
	}
	observedAt = observedAt.UTC()
	now := s.now()
	if observedAt.After(now.Add(time.Minute)) {
		return Sender{}, ErrInvalid
	}
	if current.HealthObservedAt != nil && !observedAt.After(current.HealthObservedAt.UTC()) {
		return Sender{}, ErrInvalid
	}
	current.Health = health
	current.HealthObservedAt = &observedAt
	current.Reason = reason
	current.Version++
	current.UpdatedAt = now
	evidence := map[string]any{"health": health, "observedAt": observedAt}
	return s.Store.CompareAndSwap(ctx, current, expected, actor, "HEALTH_OBSERVED", evidence)
}

func (s *Service) Retire(ctx context.Context, senderID string, expected int64, actor, reason string) (Sender, error) {
	if s == nil || s.Store == nil {
		return Sender{}, errors.New("Meta Cloud sender store is required")
	}
	current, err := s.Store.Get(ctx, strings.TrimSpace(senderID))
	if err != nil {
		return Sender{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if current.Version != expected {
		return Sender{}, ErrConflict
	}
	if current.Status != StatusActive || actor == "" || len(reason) < 5 {
		return Sender{}, ErrInvalid
	}
	now := s.now()
	current.Status = StatusRetired
	current.EffectiveTo = &now
	current.Reason = reason
	current.Version++
	current.UpdatedAt = now
	return s.Store.CompareAndSwap(ctx, current, expected, actor, string(StatusRetired), nil)
}

func (s *Service) Get(ctx context.Context, senderID string) (Sender, error) {
	if s == nil || s.Store == nil {
		return Sender{}, errors.New("Meta Cloud sender store is required")
	}
	return s.Store.Get(ctx, strings.TrimSpace(senderID))
}

func (s *Service) List(ctx context.Context) ([]Sender, error) {
	if s == nil || s.Store == nil {
		return nil, errors.New("Meta Cloud sender store is required")
	}
	return s.Store.List(ctx)
}

func (s *Service) ListEvents(ctx context.Context, senderID string) ([]Event, error) {
	if s == nil || s.Store == nil {
		return nil, errors.New("Meta Cloud sender store is required")
	}
	return s.Store.ListEvents(ctx, strings.TrimSpace(senderID))
}

var credentialKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,127}$`)
var graphVersionPattern = regexp.MustCompile(`^v[0-9]{1,3}\.[0-9]{1,2}$`)

func validateSender(value *Sender) error {
	value.OrganisationID = strings.TrimSpace(value.OrganisationID)
	value.SenderPoolID = strings.TrimSpace(value.SenderPoolID)
	value.WABAID = strings.TrimSpace(value.WABAID)
	value.PhoneNumberID = strings.TrimSpace(value.PhoneNumberID)
	value.DisplayName = strings.TrimSpace(value.DisplayName)
	value.BusinessPhoneDisplay = strings.TrimSpace(value.BusinessPhoneDisplay)
	value.CredentialKey = strings.TrimSpace(value.CredentialKey)
	value.GraphAPIVersion = strings.TrimSpace(value.GraphAPIVersion)
	if value.OrganisationID == "" || value.SenderPoolID == "" || value.WABAID == "" || value.PhoneNumberID == "" || value.DisplayName == "" || value.BusinessPhoneDisplay == "" || value.CreatedBy == "" || len(value.Reason) < 5 {
		return ErrInvalid
	}
	if !credentialKeyPattern.MatchString(value.CredentialKey) || !graphVersionPattern.MatchString(value.GraphAPIVersion) {
		return ErrInvalid
	}
	if strings.ContainsAny(value.DisplayName+value.BusinessPhoneDisplay, "\r\n\x00") {
		return ErrInvalid
	}
	return nil
}

type MemoryStore struct {
	mu     sync.Mutex
	items  map[string]Sender
	events map[string][]Event
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: map[string]Sender{}, events: map[string][]Event{}}
}

func (m *MemoryStore) List(context.Context) ([]Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Sender, 0, len(m.items))
	for _, value := range m.items {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (m *MemoryStore) ResolveMetaWebhookSender(_ context.Context, credentialKey, wabaID, phoneNumberID string) (Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	credentialKey, wabaID, phoneNumberID = strings.TrimSpace(credentialKey), strings.TrimSpace(wabaID), strings.TrimSpace(phoneNumberID)
	for _, value := range m.items {
		if value.CredentialKey == credentialKey && value.WABAID == wabaID && value.PhoneNumberID == phoneNumberID {
			return value, nil
		}
	}
	return Sender{}, ErrNotFound
}

func (m *MemoryStore) Get(_ context.Context, senderID string) (Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.items[senderID]
	if !ok {
		return Sender{}, ErrNotFound
	}
	return value, nil
}

func (m *MemoryStore) Create(_ context.Context, value Sender) (Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.items[value.ID]; exists {
		return Sender{}, ErrConflict
	}
	phoneNumberID := strings.TrimSpace(value.PhoneNumberID)
	for _, existing := range m.items {
		if strings.TrimSpace(existing.PhoneNumberID) == phoneNumberID {
			return Sender{}, ErrConflict
		}
	}
	m.items[value.ID] = value
	m.appendEvent(value, "CREATED", value.CreatedBy, nil)
	return value, nil
}

func sameSenderIdentity(a, b Sender) bool {
	return strings.TrimSpace(a.ID) == strings.TrimSpace(b.ID) &&
		strings.TrimSpace(a.OrganisationID) == strings.TrimSpace(b.OrganisationID) &&
		strings.TrimSpace(a.SenderPoolID) == strings.TrimSpace(b.SenderPoolID) &&
		strings.TrimSpace(a.WABAID) == strings.TrimSpace(b.WABAID) &&
		strings.TrimSpace(a.PhoneNumberID) == strings.TrimSpace(b.PhoneNumberID) &&
		strings.TrimSpace(a.DisplayName) == strings.TrimSpace(b.DisplayName) &&
		strings.TrimSpace(a.BusinessPhoneDisplay) == strings.TrimSpace(b.BusinessPhoneDisplay) &&
		strings.TrimSpace(a.CredentialKey) == strings.TrimSpace(b.CredentialKey) &&
		strings.TrimSpace(a.GraphAPIVersion) == strings.TrimSpace(b.GraphAPIVersion) &&
		strings.TrimSpace(a.CreatedBy) == strings.TrimSpace(b.CreatedBy) &&
		a.CreatedAt.UTC().Truncate(time.Microsecond).Equal(b.CreatedAt.UTC().Truncate(time.Microsecond))
}

func (m *MemoryStore) CompareAndSwap(_ context.Context, value Sender, expected int64, actor, action string, evidence map[string]any) (Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.items[value.ID]
	if !ok {
		return Sender{}, ErrNotFound
	}
	if current.Version != expected || !sameSenderIdentity(current, value) {
		return Sender{}, ErrConflict
	}
	m.items[value.ID] = value
	m.appendEvent(value, action, actor, evidence)
	return value, nil
}

func (m *MemoryStore) ListEvents(_ context.Context, senderID string) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[senderID]; !ok {
		return nil, ErrNotFound
	}
	return append([]Event(nil), m.events[senderID]...), nil
}

func (m *MemoryStore) appendEvent(value Sender, action, actor string, evidence map[string]any) {
	events := m.events[value.ID]
	m.events[value.ID] = append(events, Event{
		ID: int64(len(events) + 1), SenderID: value.ID, Action: action, ActorID: actor,
		Reason: value.Reason, SenderVersion: value.Version, Evidence: evidence, OccurredAt: value.UpdatedAt,
	})
}
