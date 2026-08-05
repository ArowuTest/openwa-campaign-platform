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
	EngineWhatsAppWebJS    Engine       = "WHATSAPP_WEB_JS"
	EngineBaileys          Engine       = "BAILEYS"
	RoutingSpecificSession RoutingMode  = "SPECIFIC_SESSION"
	RoutingSenderPool      RoutingMode  = "SENDER_POOL"
	FallbackNone           FallbackMode = "NONE"
)

type TransportSelection struct {
	Channel                   string       `json:"channel"`
	Provider                  Provider     `json:"provider"`
	Engine                    Engine       `json:"engine"`
	RoutingMode               RoutingMode  `json:"routingMode"`
	GatewayPoolID             string       `json:"gatewayPoolId"`
	GatewayPoolVersion        int64        `json:"gatewayPoolVersion,omitempty"`
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
	if strings.ToUpper(strings.TrimSpace(t.Channel)) != "WHATSAPP" {
		return errors.New("transport channel must be WHATSAPP")
	}
	if t.Provider != ProviderOpenWA {
		return errors.New("initial release supports OPENWA as the WhatsApp provider")
	}
	if t.Engine != EngineWhatsAppWebJS && t.Engine != EngineBaileys {
		return errors.New("unsupported OpenWA engine")
	}
	if strings.TrimSpace(t.GatewayPoolID) == "" || strings.TrimSpace(t.AdapterVersion) == "" || strings.TrimSpace(t.RoutingPolicyVersion) == "" || strings.TrimSpace(t.CapacityEvidenceVersion) == "" {
		return errors.New("gateway pool, adapter, routing policy and capacity evidence versions are required")
	}
	if t.GatewayPoolVersion < 0 {
		return errors.New("gateway pool version cannot be negative")
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
		if strings.TrimSpace(t.SessionID) == "" || strings.TrimSpace(t.SenderPoolID) != "" {
			return errors.New("specific-session routing requires one session and no sender pool")
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

// ValidateAgainstGatewayPool binds a frozen campaign route to an active, engine-compatible
// gateway pool whose governed capability catalogue satisfies the message requirements.
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
