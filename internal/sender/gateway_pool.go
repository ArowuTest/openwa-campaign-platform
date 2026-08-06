package sender

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type GatewayProvider string
type GatewayEngine string
type GatewayPoolStatus string

const (
	GatewayProviderOpenWA      GatewayProvider   = "OPENWA"
	GatewayEngineWhatsAppWebJS GatewayEngine     = "WHATSAPP_WEB_JS"
	GatewayEngineBaileys       GatewayEngine     = "BAILEYS"
	GatewayPoolDraft           GatewayPoolStatus = "DRAFT"
	GatewayPoolPendingApproval GatewayPoolStatus = "PENDING_APPROVAL"
	GatewayPoolActive          GatewayPoolStatus = "ACTIVE"
	GatewayPoolPaused          GatewayPoolStatus = "PAUSED"
	GatewayPoolRejected        GatewayPoolStatus = "REJECTED"
	GatewayPoolRetired         GatewayPoolStatus = "RETIRED"
)

type Capability string

const (
	CapabilitySendText        Capability = "SEND_TEXT"
	CapabilitySendImage       Capability = "SEND_IMAGE"
	CapabilitySendVideo       Capability = "SEND_VIDEO"
	CapabilitySendDocument    Capability = "SEND_DOCUMENT"
	CapabilityDeliveryEvents  Capability = "DELIVERY_EVENTS"
	CapabilityReadEvents      Capability = "READ_EVENTS"
	CapabilityInboundMessages Capability = "INBOUND_MESSAGES"
)

type GatewayPool struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Provider            GatewayProvider   `json:"provider"`
	Engine              GatewayEngine     `json:"engine"`
	AdapterVersion      string            `json:"adapterVersion"`
	Status              GatewayPoolStatus `json:"status"`
	Capabilities        []Capability      `json:"capabilities"`
	MinimumHealthyNodes int               `json:"minimumHealthyNodes"`
	CreatedBy           string            `json:"createdBy,omitempty"`
	SubmittedBy         string            `json:"submittedBy,omitempty"`
	ApprovedBy          string            `json:"approvedBy,omitempty"`
	EffectiveFrom       *time.Time        `json:"effectiveFrom,omitempty"`
	EffectiveTo         *time.Time        `json:"effectiveTo,omitempty"`
	ApprovalReason      string            `json:"approvalReason,omitempty"`
	Version             int64             `json:"version"`
	CreatedAt           time.Time         `json:"createdAt"`
	UpdatedAt           time.Time         `json:"updatedAt"`
}

type GatewayPoolStore interface {
	ListGatewayPools(context.Context) ([]GatewayPool, error)
	CreateGatewayPool(context.Context, GatewayPool, string, string) (GatewayPool, error)
	UpdateGatewayPool(context.Context, string, int64, GatewayPool, string, string) (GatewayPool, error)
	GetGatewayPool(context.Context, string) (GatewayPool, error)
}

type GatewayPoolService struct{ Store GatewayPoolStore }

func (s *GatewayPoolService) Get(ctx context.Context, id string) (GatewayPool, error) {
	if s == nil || s.Store == nil {
		return GatewayPool{}, errors.New("gateway pool store is required")
	}
	return s.Store.GetGatewayPool(ctx, strings.TrimSpace(id))
}

func (s *GatewayPoolService) Create(ctx context.Context, value GatewayPool, actor, reason string) (GatewayPool, error) {
	if s == nil || s.Store == nil {
		return GatewayPool{}, errors.New("gateway pool store is required")
	}
	if err := normalizeAndValidateGatewayPool(&value, actor, reason); err != nil {
		return GatewayPool{}, err
	}
	if value.CreatedBy == "" {
		value.CreatedBy = strings.TrimSpace(actor)
	}
	if value.Status == GatewayPoolActive && value.EffectiveFrom == nil {
		now := time.Now().UTC()
		value.EffectiveFrom = &now
	}
	return s.Store.CreateGatewayPool(ctx, value, actor, reason)
}

func (s *GatewayPoolService) Update(ctx context.Context, id string, expectedVersion int64, value GatewayPool, actor, reason string) (GatewayPool, error) {
	if strings.TrimSpace(id) == "" || expectedVersion <= 0 {
		return GatewayPool{}, errors.New("id and expected version are required")
	}
	if err := normalizeAndValidateGatewayPool(&value, actor, reason); err != nil {
		return GatewayPool{}, err
	}
	return s.Store.UpdateGatewayPool(ctx, id, expectedVersion, value, actor, reason)
}

func (s *GatewayPoolService) RequireCapabilities(ctx context.Context, poolID string, provider GatewayProvider, engine GatewayEngine, required []Capability) (GatewayPool, error) {
	if s == nil || s.Store == nil {
		return GatewayPool{}, errors.New("gateway pool store is required")
	}
	pool, err := s.Store.GetGatewayPool(ctx, strings.TrimSpace(poolID))
	if err != nil {
		return GatewayPool{}, err
	}
	if pool.Status != GatewayPoolActive {
		return GatewayPool{}, errors.New("gateway pool is not active")
	}
	now := time.Now().UTC()
	if pool.EffectiveFrom != nil && pool.EffectiveFrom.After(now) {
		return GatewayPool{}, errors.New("gateway pool is not yet effective")
	}
	if pool.EffectiveTo != nil && !pool.EffectiveTo.After(now) {
		return GatewayPool{}, errors.New("gateway pool is no longer effective")
	}
	if pool.Provider != provider || pool.Engine != engine {
		return GatewayPool{}, errors.New("gateway pool provider or engine is incompatible")
	}
	available := map[Capability]bool{}
	for _, capability := range pool.Capabilities {
		available[capability] = true
	}
	for _, capability := range required {
		if !available[capability] {
			return GatewayPool{}, errors.New("gateway pool lacks required capability: " + string(capability))
		}
	}
	return pool, nil
}

func normalizeAndValidateGatewayPool(value *GatewayPool, actor, reason string) error {
	value.Name = strings.TrimSpace(value.Name)
	value.AdapterVersion = strings.TrimSpace(value.AdapterVersion)
	if value.Name == "" || value.AdapterVersion == "" || strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return errors.New("name, adapter version, actor and reason are required")
	}
	if value.Provider != GatewayProviderOpenWA {
		return errors.New("initial release supports OPENWA gateway pools")
	}
	if value.Engine != GatewayEngineWhatsAppWebJS && value.Engine != GatewayEngineBaileys {
		return errors.New("unsupported gateway engine")
	}
	if value.Status == "" {
		value.Status = GatewayPoolActive
	}
	if value.Status != GatewayPoolDraft && value.Status != GatewayPoolPendingApproval && value.Status != GatewayPoolActive && value.Status != GatewayPoolPaused && value.Status != GatewayPoolRejected && value.Status != GatewayPoolRetired {
		return errors.New("invalid gateway pool status")
	}
	if value.EffectiveFrom != nil && value.EffectiveTo != nil && !value.EffectiveTo.After(*value.EffectiveFrom) {
		return errors.New("gateway pool effective end must be after its start")
	}
	if value.MinimumHealthyNodes <= 0 || value.MinimumHealthyNodes > 1000 {
		return errors.New("minimum healthy nodes must be between 1 and 1000")
	}
	allowed := map[Capability]bool{CapabilitySendText: true, CapabilitySendImage: true, CapabilitySendVideo: true, CapabilitySendDocument: true, CapabilityDeliveryEvents: true, CapabilityReadEvents: true, CapabilityInboundMessages: true}
	unique := map[Capability]bool{}
	normalized := make([]Capability, 0, len(value.Capabilities))
	for _, c := range value.Capabilities {
		c = Capability(strings.ToUpper(strings.TrimSpace(string(c))))
		if !allowed[c] {
			return errors.New("unsupported gateway capability: " + string(c))
		}
		if !unique[c] {
			unique[c] = true
			normalized = append(normalized, c)
		}
	}
	if len(normalized) == 0 {
		return errors.New("at least one gateway capability is required")
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	value.Capabilities = normalized
	return nil
}

type GatewayPoolEvent struct {
	ID         string         `json:"id"`
	PoolID     string         `json:"poolId"`
	EventType  string         `json:"eventType"`
	Version    int64          `json:"version"`
	ActorID    string         `json:"actorId,omitempty"`
	Reason     string         `json:"reason"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
}

type GatewayPoolUsage struct {
	NonRetiredNodes      int `json:"nonRetiredNodes"`
	NonRetiredSessions   int `json:"nonRetiredSessions"`
	NonTerminalCampaigns int `json:"nonTerminalCampaigns"`
	ActiveReservations   int `json:"activeReservations"`
}

func (u GatewayPoolUsage) BlocksRetirement() bool {
	return u.NonRetiredNodes > 0 || u.NonRetiredSessions > 0 || u.NonTerminalCampaigns > 0 || u.ActiveReservations > 0
}

type GatewayPoolGovernanceStore interface {
	GatewayPoolStore
	ListGatewayPoolEvents(context.Context, string, int) ([]GatewayPoolEvent, error)
	GatewayPoolUsage(context.Context, string, time.Time) (GatewayPoolUsage, error)
}

type GatewayPoolAdministration struct {
	Store GatewayPoolGovernanceStore
	Clock func() time.Time
}

func (a *GatewayPoolAdministration) now() time.Time {
	if a != nil && a.Clock != nil {
		return a.Clock().UTC()
	}
	return time.Now().UTC()
}

func (a *GatewayPoolAdministration) List(ctx context.Context) ([]GatewayPool, error) {
	if a == nil || a.Store == nil {
		return nil, errors.New("gateway pool governance store is required")
	}
	return a.Store.ListGatewayPools(ctx)
}
func (a *GatewayPoolAdministration) Get(ctx context.Context, id string) (GatewayPool, error) {
	if a == nil || a.Store == nil {
		return GatewayPool{}, errors.New("gateway pool governance store is required")
	}
	return a.Store.GetGatewayPool(ctx, strings.TrimSpace(id))
}
func (a *GatewayPoolAdministration) Create(ctx context.Context, value GatewayPool, actor, reason string) (GatewayPool, error) {
	if a == nil || a.Store == nil {
		return GatewayPool{}, errors.New("gateway pool governance store is required")
	}
	value.Status = GatewayPoolDraft
	value.CreatedBy = strings.TrimSpace(actor)
	value.SubmittedBy = ""
	value.ApprovedBy = ""
	value.ApprovalReason = strings.TrimSpace(reason)
	if err := normalizeAndValidateGatewayPool(&value, actor, reason); err != nil {
		return GatewayPool{}, err
	}
	return a.Store.CreateGatewayPool(ctx, value, actor, reason)
}
func (a *GatewayPoolAdministration) Submit(ctx context.Context, poolID string, expected int64, actor, reason string) (GatewayPool, error) {
	current, err := a.Get(ctx, poolID)
	if err != nil {
		return GatewayPool{}, err
	}
	if current.Version != expected || current.Status != GatewayPoolDraft || strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return GatewayPool{}, ErrSenderConflict
	}
	current.Status = GatewayPoolPendingApproval
	current.SubmittedBy = strings.TrimSpace(actor)
	current.ApprovalReason = strings.TrimSpace(reason)
	return a.Store.UpdateGatewayPool(ctx, current.ID, expected, current, actor, reason)
}
func (a *GatewayPoolAdministration) Decide(ctx context.Context, poolID string, expected int64, approve bool, actor, reason string, effectiveFrom time.Time) (GatewayPool, error) {
	current, err := a.Get(ctx, poolID)
	if err != nil {
		return GatewayPool{}, err
	}
	actor, reason = strings.TrimSpace(actor), strings.TrimSpace(reason)
	if current.Version != expected || current.Status != GatewayPoolPendingApproval || actor == "" || reason == "" {
		return GatewayPool{}, ErrSenderConflict
	}
	if actor == current.CreatedBy || actor == current.SubmittedBy {
		return GatewayPool{}, errors.New("gateway pool approver must be independent from maker and submitter")
	}
	current.ApprovedBy = actor
	current.ApprovalReason = reason
	if approve {
		current.Status = GatewayPoolActive
		if effectiveFrom.IsZero() {
			effectiveFrom = a.now()
		}
		effectiveFrom = effectiveFrom.UTC()
		if current.EffectiveTo != nil && !current.EffectiveTo.After(effectiveFrom) {
			return GatewayPool{}, errors.New("gateway pool effective end must be after its approved start")
		}
		current.EffectiveFrom = &effectiveFrom
	} else {
		current.Status = GatewayPoolRejected
	}
	return a.Store.UpdateGatewayPool(ctx, current.ID, expected, current, actor, reason)
}
func (a *GatewayPoolAdministration) Retire(ctx context.Context, poolID string, expected int64, actor, reason string) (GatewayPool, error) {
	current, err := a.Get(ctx, poolID)
	if err != nil {
		return GatewayPool{}, err
	}
	if current.Version != expected || (current.Status != GatewayPoolActive && current.Status != GatewayPoolPaused) || strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return GatewayPool{}, ErrSenderConflict
	}
	now := a.now()
	usage, err := a.Store.GatewayPoolUsage(ctx, current.ID, now)
	if err != nil {
		return GatewayPool{}, err
	}
	if usage.BlocksRetirement() {
		return GatewayPool{}, fmt.Errorf("gateway pool remains in use: nodes=%d sessions=%d campaigns=%d reservations=%d", usage.NonRetiredNodes, usage.NonRetiredSessions, usage.NonTerminalCampaigns, usage.ActiveReservations)
	}
	current.Status = GatewayPoolRetired
	if current.EffectiveFrom == nil || !current.EffectiveFrom.After(now) {
		current.EffectiveTo = &now
	} else {
		current.EffectiveTo = nil
	}
	current.ApprovalReason = strings.TrimSpace(reason)
	return a.Store.UpdateGatewayPool(ctx, current.ID, expected, current, actor, reason)
}
func (a *GatewayPoolAdministration) Events(ctx context.Context, poolID string, limit int) ([]GatewayPoolEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	return a.Store.ListGatewayPoolEvents(ctx, strings.TrimSpace(poolID), limit)
}
