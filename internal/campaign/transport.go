package campaign

import (
	"context"
	"errors"
	"strings"

	"campaign-platform/internal/sender"
)

type Provider string
type Engine string
type RoutingMode string
type FallbackMode string

const (
	ProviderOpenWA         Provider     = "OPENWA"
	ProviderMeta           Provider     = "META"
	EngineWhatsAppWebJS    Engine       = "WHATSAPP_WEB_JS"
	EngineBaileys          Engine       = "BAILEYS"
	EngineMetaCloud        Engine       = "CLOUD_API"
	RoutingSpecificSession RoutingMode  = "SPECIFIC_SESSION"
	RoutingSenderPool      RoutingMode  = "SENDER_POOL"
	FallbackNone           FallbackMode = "NONE"
)

type TransportSelection struct {
	Channel                   string       `json:"channel"`
	Provider                  Provider     `json:"provider"`
	Engine                    Engine       `json:"engine"`
	RoutingMode               RoutingMode  `json:"routingMode"`
	GatewayPoolID             string       `json:"gatewayPoolId,omitempty"`
	GatewayPoolVersion        int64        `json:"gatewayPoolVersion,omitempty"`
	MetaSenderID              string       `json:"metaSenderId,omitempty"`
	SessionID                 string       `json:"sessionId,omitempty"`
	SenderPoolID              string       `json:"senderPoolId,omitempty"`
	AdapterVersion            string       `json:"adapterVersion"`
	ProviderDefinitionID      string       `json:"providerDefinitionId,omitempty"`
	ProviderDefinitionVersion int64        `json:"providerDefinitionVersion,omitempty"`
	RequiredCapabilities      []string     `json:"requiredCapabilities,omitempty"`
	FallbackMode              FallbackMode `json:"fallbackMode"`
	RoutingPolicyVersion      string       `json:"routingPolicyVersion"`
	CapacityEvidenceVersion   string       `json:"capacityEvidenceVersion"`
}

func (t TransportSelection) Validate() error {
	if t.Channel != "WHATSAPP" {
		return errors.New("transport channel must be canonical WHATSAPP")
	}
	provider, engine := t.Provider, t.Engine
	if string(provider) != strings.TrimSpace(string(provider)) || string(engine) != strings.TrimSpace(string(engine)) {
		return errors.New("transport provider and engine must be canonical")
	}
	switch provider {
	case ProviderOpenWA:
		if engine != EngineWhatsAppWebJS && engine != EngineBaileys {
			return errors.New("unsupported OpenWA engine")
		}
		if strings.TrimSpace(t.GatewayPoolID) == "" || strings.TrimSpace(t.MetaSenderID) != "" {
			return errors.New("OpenWA transport requires one gateway pool and no Meta sender")
		}
	case ProviderMeta:
		if engine != EngineMetaCloud {
			return errors.New("unsupported Meta engine")
		}
		if strings.TrimSpace(t.MetaSenderID) == "" || strings.TrimSpace(t.GatewayPoolID) != "" {
			return errors.New("Meta Cloud transport requires one Meta sender and no gateway pool")
		}
	default:
		return errors.New("unsupported WhatsApp provider")
	}
	if strings.TrimSpace(t.AdapterVersion) == "" || strings.TrimSpace(t.RoutingPolicyVersion) == "" || strings.TrimSpace(t.CapacityEvidenceVersion) == "" {
		return errors.New("adapter, routing policy and capacity evidence versions are required")
	}
	if t.GatewayPoolVersion < 0 {
		return errors.New("gateway pool version cannot be negative")
	}
	if provider == ProviderMeta && t.GatewayPoolVersion != 0 {
		return errors.New("Meta Cloud transport cannot freeze a gateway pool version")
	}
	if (strings.TrimSpace(t.ProviderDefinitionID) == "") != (t.ProviderDefinitionVersion == 0) {
		return errors.New("provider capability definition ID and version must be supplied together")
	}
	if t.ProviderDefinitionVersion < 0 {
		return errors.New("provider capability definition version cannot be negative")
	}
	if t.FallbackMode != FallbackNone {
		return errors.New("initial release requires fallback mode NONE")
	}
	switch t.RoutingMode {
	case RoutingSpecificSession:
		if provider != ProviderOpenWA || strings.TrimSpace(t.SessionID) == "" || strings.TrimSpace(t.SenderPoolID) != "" {
			return errors.New("specific-session routing requires one OpenWA session and no sender pool")
		}
	case RoutingSenderPool:
		if strings.TrimSpace(t.SenderPoolID) == "" || strings.TrimSpace(t.SessionID) != "" {
			return errors.New("sender-pool routing requires one sender pool and no specific session")
		}
	default:
		return errors.New("unsupported transport routing mode")
	}
	return nil
}

// ValidateAgainstGatewayPool binds an OpenWA campaign route to an active,
// engine-compatible gateway pool. Meta Cloud routes are governed independently.
func (t TransportSelection) ValidateAgainstGatewayPool(ctx context.Context, service *sender.GatewayPoolService) error {
	if err := t.Validate(); err != nil {
		return err
	}
	required := make([]sender.Capability, 0, len(t.RequiredCapabilities))
	for _, capability := range t.RequiredCapabilities {
		required = append(required, sender.Capability(capability))
	}
	_, err := service.RequireCapabilities(ctx, t.GatewayPoolID, sender.GatewayProvider(t.Provider), sender.GatewayEngine(t.Engine), required)
	return err
}
