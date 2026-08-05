package sender

import (
	"context"
	"errors"
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
	GatewayPoolActive          GatewayPoolStatus = "ACTIVE"
	GatewayPoolPaused          GatewayPoolStatus = "PAUSED"
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

func (s *GatewayPoolService) Create(ctx context.Context, value GatewayPool, actor, reason string) (GatewayPool, error) {
	if s == nil || s.Store == nil {
		return GatewayPool{}, errors.New("gateway pool store is required")
	}
	if err := normalizeAndValidateGatewayPool(&value, actor, reason); err != nil {
		return GatewayPool{}, err
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
	if value.Status != GatewayPoolActive && value.Status != GatewayPoolPaused && value.Status != GatewayPoolRetired {
		return errors.New("invalid gateway pool status")
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
