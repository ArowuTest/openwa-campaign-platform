package dispatch

import (
	"strings"
	"testing"
	"time"
)

func validGovernedRouteEvidence() governedRouteEvidence {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	return governedRouteEvidence{
		SessionID:                       "session-1",
		SessionSenderPoolID:             "sender-pool-1",
		SessionConfigurationVersion:     4,
		GatewayNodeID:                   "node-1",
		GatewayNodeVersion:              9,
		GatewayNodeStatus:               "READY",
		SessionLeaseVersion:             12,
		SessionLeaseExpiresAt:           now.Add(10 * time.Minute),
		GatewayPoolID:                   "gateway-1",
		GatewayPoolVersion:              7,
		GatewayProvider:                 "OPENWA",
		GatewayEngine:                   "BAILEYS",
		GatewayAdapterVersion:           "0.13.0",
		GatewayStatus:                   "ACTIVE",
		GatewayCapabilities:             []string{"SEND_TEXT", "DELIVERY_EVENTS"},
		ExpectedSenderPoolID:            "sender-pool-1",
		ExpectedGatewayPoolID:           "gateway-1",
		ExpectedGatewayPoolVersion:      7,
		ExpectedProvider:                "OPENWA",
		ExpectedEngine:                  "BAILEYS",
		ExpectedAdapterVersion:          "0.13.0",
		ProviderDefinitionID:            "definition-1",
		ProviderDefinitionVersion:       3,
		CampaignRequiredCapabilities:    []string{"DELIVERY_EVENTS"},
		DefinitionVersion:               3,
		DefinitionProvider:              "OPENWA",
		DefinitionChannel:               "WHATSAPP",
		DefinitionEngine:                "BAILEYS",
		DefinitionAdapterVersion:        "0.13.0",
		DefinitionMinimumGatewayVersion: "0.13.0",
		DefinitionCapabilities:          []string{"SEND_TEXT", "DELIVERY_EVENTS"},
		DefinitionStatus:                "ACTIVE",
		DefinitionEffectiveFrom:         now.Add(-time.Hour),
	}
}

func TestValidateGovernedRouteEvidence(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	if err := validateGovernedRouteEvidence(validGovernedRouteEvidence(), "text", now); err != nil {
		t.Fatal(err)
	}
}

func TestValidateGovernedRouteEvidenceFailsClosed(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		edit func(*governedRouteEvidence)
		want string
	}{
		{"missing binding", func(v *governedRouteEvidence) { v.ProviderDefinitionID = "" }, "no frozen"},
		{"definition changed", func(v *governedRouteEvidence) { v.DefinitionVersion++ }, "version has changed"},
		{"retired definition", func(v *governedRouteEvidence) { v.DefinitionStatus = "RETIRED" }, "not active"},
		{"wrong gateway", func(v *governedRouteEvidence) { v.GatewayPoolID = "gateway-2" }, "does not match"},
		{"gateway version drift", func(v *governedRouteEvidence) { v.GatewayPoolVersion++ }, "pool version"},
		{"wrong sender pool", func(v *governedRouteEvidence) { v.SessionSenderPoolID = "sender-pool-2" }, "sender pool"},
		{"adapter drift", func(v *governedRouteEvidence) { v.GatewayAdapterVersion = "0.14.0" }, "adapter version"},
		{"minimum version", func(v *governedRouteEvidence) { v.DefinitionMinimumGatewayVersion = "0.14.0" }, "below"},
		{"missing message capability", func(v *governedRouteEvidence) { v.GatewayCapabilities = []string{"DELIVERY_EVENTS"} }, "SEND_TEXT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := validGovernedRouteEvidence()
			tc.edit(&value)
			err := validateGovernedRouteEvidence(value, "text", now)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}
