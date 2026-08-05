package provider

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"campaign-platform/internal/shared/id"
)

type Status string

type Channel string

type Capability string

const (
	StatusDraft    Status = "DRAFT"
	StatusPending  Status = "PENDING_APPROVAL"
	StatusActive   Status = "ACTIVE"
	StatusRejected Status = "REJECTED"
	StatusRetired  Status = "RETIRED"

	ChannelWhatsApp Channel = "WHATSAPP"
	ChannelSMS      Channel = "SMS"
	ChannelEmail    Channel = "EMAIL"

	CapabilitySendText       Capability = "SEND_TEXT"
	CapabilitySendImage      Capability = "SEND_IMAGE"
	CapabilitySendVideo      Capability = "SEND_VIDEO"
	CapabilitySendDocument   Capability = "SEND_DOCUMENT"
	CapabilityDeliveryEvents Capability = "DELIVERY_EVENTS"
	CapabilityReadEvents     Capability = "READ_EVENTS"
	CapabilityInbound        Capability = "INBOUND_MESSAGES"
	CapabilityPairingQR      Capability = "PAIRING_QR"
	CapabilityPairingCode    Capability = "PAIRING_CODE"
)

var (
	ErrNotFound = errors.New("provider capability definition not found")
	ErrConflict = errors.New("provider capability definition version conflict")
	ErrInvalid  = errors.New("provider capability definition is invalid")
)

type Definition struct {
	ID                     string       `json:"id"`
	Provider               string       `json:"provider"`
	Channel                Channel      `json:"channel"`
	Engine                 string       `json:"engine,omitempty"`
	AdapterVersion         string       `json:"adapterVersion"`
	MinimumGatewayVersion  string       `json:"minimumGatewayVersion,omitempty"`
	Capabilities           []Capability `json:"capabilities"`
	MaximumAttachmentBytes int64        `json:"maximumAttachmentBytes,omitempty"`
	Status                 Status       `json:"status"`
	EffectiveFrom          time.Time    `json:"effectiveFrom"`
	EffectiveTo            *time.Time   `json:"effectiveTo,omitempty"`
	Version                int64        `json:"version"`
	CreatedBy              string       `json:"createdBy"`
	SubmittedBy            string       `json:"submittedBy,omitempty"`
	ApprovedBy             string       `json:"approvedBy,omitempty"`
	Reason                 string       `json:"reason"`
	CreatedAt              time.Time    `json:"createdAt"`
	UpdatedAt              time.Time    `json:"updatedAt"`
}

type Store interface {
	List(context.Context) ([]Definition, error)
	Get(context.Context, string) (Definition, error)
	Active(context.Context, string, Channel, string, time.Time) (Definition, error)
	Create(context.Context, Definition) (Definition, error)
	CompareAndSwap(context.Context, Definition, int64) (Definition, error)
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

func (s *Service) CreateDraft(ctx context.Context, d Definition, actor, reason string) (Definition, error) {
	if s == nil || s.Store == nil {
		return Definition{}, errors.New("provider capability store is required")
	}
	now := s.now()
	d.ID = ""
	d.Status = StatusDraft
	d.Version = 1
	d.CreatedBy = strings.TrimSpace(actor)
	d.Reason = strings.TrimSpace(reason)
	d.CreatedAt, d.UpdatedAt = now, now
	if d.EffectiveFrom.IsZero() {
		d.EffectiveFrom = now
	}
	if err := validate(&d); err != nil {
		return Definition{}, err
	}
	generated, err := id.New()
	if err != nil {
		return Definition{}, err
	}
	d.ID = generated
	return s.Store.Create(ctx, d)
}

func (s *Service) Submit(ctx context.Context, definitionID string, expected int64, actor, reason string) (Definition, error) {
	d, err := s.Store.Get(ctx, strings.TrimSpace(definitionID))
	if err != nil {
		return Definition{}, err
	}
	if d.Version != expected {
		return Definition{}, ErrConflict
	}
	if d.Status != StatusDraft && d.Status != StatusRejected {
		return Definition{}, ErrInvalid
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if actor == "" || len(reason) < 5 {
		return Definition{}, ErrInvalid
	}
	d.Status, d.SubmittedBy, d.Reason = StatusPending, actor, reason
	d.Version++
	d.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, d, expected)
}

func (s *Service) Decide(ctx context.Context, definitionID string, expected int64, approve bool, actor, reason string) (Definition, error) {
	d, err := s.Store.Get(ctx, strings.TrimSpace(definitionID))
	if err != nil {
		return Definition{}, err
	}
	if d.Version != expected {
		return Definition{}, ErrConflict
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if d.Status != StatusPending || actor == "" || actor == d.SubmittedBy || len(reason) < 5 {
		return Definition{}, ErrInvalid
	}
	if approve {
		d.Status, d.ApprovedBy = StatusActive, actor
	} else {
		d.Status = StatusRejected
	}
	d.Reason = reason
	d.Version++
	d.UpdatedAt = s.now()
	return s.Store.CompareAndSwap(ctx, d, expected)
}

func (s *Service) Require(ctx context.Context, providerName string, channel Channel, engine string, at time.Time, required []Capability) (Definition, error) {
	if s == nil || s.Store == nil {
		return Definition{}, errors.New("provider capability store is required")
	}
	d, err := s.Store.Active(ctx, strings.ToUpper(strings.TrimSpace(providerName)), channel, strings.ToUpper(strings.TrimSpace(engine)), at.UTC())
	if err != nil {
		return Definition{}, err
	}
	available := make(map[Capability]struct{}, len(d.Capabilities))
	for _, c := range d.Capabilities {
		available[c] = struct{}{}
	}
	for _, c := range required {
		if _, ok := available[c]; !ok {
			return Definition{}, errors.New("provider route lacks required capability: " + string(c))
		}
	}
	return d, nil
}

func validate(d *Definition) error {
	d.Provider = strings.ToUpper(strings.TrimSpace(d.Provider))
	d.Engine = strings.ToUpper(strings.TrimSpace(d.Engine))
	d.AdapterVersion = strings.TrimSpace(d.AdapterVersion)
	d.MinimumGatewayVersion = strings.TrimSpace(d.MinimumGatewayVersion)
	if d.Provider == "" || d.AdapterVersion == "" || d.CreatedBy == "" || len(d.Reason) < 5 {
		return ErrInvalid
	}
	switch d.Channel {
	case ChannelWhatsApp, ChannelSMS, ChannelEmail:
	default:
		return ErrInvalid
	}
	if d.Channel == ChannelWhatsApp && d.Engine == "" {
		return ErrInvalid
	}
	if d.MaximumAttachmentBytes < 0 {
		return ErrInvalid
	}
	if d.EffectiveTo != nil && !d.EffectiveTo.After(d.EffectiveFrom) {
		return ErrInvalid
	}
	allowed := map[Capability]bool{
		CapabilitySendText: true, CapabilitySendImage: true, CapabilitySendVideo: true, CapabilitySendDocument: true,
		CapabilityDeliveryEvents: true, CapabilityReadEvents: true, CapabilityInbound: true, CapabilityPairingQR: true, CapabilityPairingCode: true,
	}
	uniq := map[Capability]struct{}{}
	normalized := make([]Capability, 0, len(d.Capabilities))
	for _, c := range d.Capabilities {
		c = Capability(strings.ToUpper(strings.TrimSpace(string(c))))
		if !allowed[c] {
			return ErrInvalid
		}
		if _, ok := uniq[c]; ok {
			continue
		}
		uniq[c] = struct{}{}
		normalized = append(normalized, c)
	}
	if len(normalized) == 0 {
		return ErrInvalid
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	d.Capabilities = normalized
	return nil
}

type MemoryStore struct {
	mu    sync.Mutex
	items map[string]Definition
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{items: map[string]Definition{}} }
func (m *MemoryStore) List(context.Context) ([]Definition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Definition, 0, len(m.items))
	for _, d := range m.items {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (m *MemoryStore) Get(_ context.Context, id string) (Definition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.items[id]
	if !ok {
		return Definition{}, ErrNotFound
	}
	return d, nil
}
func (m *MemoryStore) Active(_ context.Context, p string, c Channel, e string, at time.Time) (Definition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best Definition
	for _, d := range m.items {
		if d.Status == StatusActive && d.Provider == p && d.Channel == c && d.Engine == e && !d.EffectiveFrom.After(at) && (d.EffectiveTo == nil || d.EffectiveTo.After(at)) && (best.ID == "" || d.EffectiveFrom.After(best.EffectiveFrom)) {
			best = d
		}
	}
	if best.ID == "" {
		return Definition{}, ErrNotFound
	}
	return best, nil
}
func (m *MemoryStore) Create(_ context.Context, d Definition) (Definition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[d.ID]; ok {
		return Definition{}, ErrConflict
	}
	m.items[d.ID] = d
	return d, nil
}
func (m *MemoryStore) CompareAndSwap(_ context.Context, d Definition, expected int64) (Definition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.items[d.ID]
	if !ok {
		return Definition{}, ErrNotFound
	}
	if current.Version != expected {
		return Definition{}, ErrConflict
	}
	if d.Status == StatusActive {
		for id, x := range m.items {
			if id != d.ID && x.Status == StatusActive && x.Provider == d.Provider && x.Channel == d.Channel && x.Engine == d.Engine && overlap(x.EffectiveFrom, x.EffectiveTo, d.EffectiveFrom, d.EffectiveTo) {
				x.Status = StatusRetired
				x.Version++
				x.UpdatedAt = d.UpdatedAt
				m.items[id] = x
			}
		}
	}
	m.items[d.ID] = d
	return d, nil
}
func overlap(a time.Time, ae *time.Time, b time.Time, be *time.Time) bool {
	aEnd := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	bEnd := aEnd
	if ae != nil {
		aEnd = *ae
	}
	if be != nil {
		bEnd = *be
	}
	return a.Before(bEnd) && b.Before(aEnd)
}
