package main

import (
	"net/http"
	"testing"
	"time"

	workerconfig "campaign-platform/internal/worker/config"
)

func TestBuildExecutionRoutingAdministrationWiresGovernance(t *testing.T) {
	routes := buildExecutionRoutingAdministration(nil, nil, nil, 7*time.Minute)
	if routes == nil || routes.ProviderCapabilities == nil || routes.GatewayPools == nil || routes.MetaSenders == nil || routes.MetaTemplates == nil {
		t.Fatalf("execution routing governance is incomplete: %#v", routes)
	}
	if routes.MetaHealthStaleAfter != 7*time.Minute {
		t.Fatalf("unexpected Meta health window: %v", routes.MetaHealthStaleAfter)
	}
}

func TestBuildCampaignTransportRouterLeavesMetaOptional(t *testing.T) {
	router, err := buildCampaignTransportRouter(workerconfig.CampaignConfig{
		GatewayURL: "http://openwa-gateway:2785", GatewayCommandSecret: "01234567890123456789012345678901",
		GatewayMaxResponse: 1 << 20,
	}, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if router.OpenWA == nil || router.Meta != nil {
		t.Fatalf("unexpected optional Meta router: %#v", router)
	}
}

func TestBuildCampaignTransportRouterEnablesMetaFromCredentialSet(t *testing.T) {
	router, err := buildCampaignTransportRouter(workerconfig.CampaignConfig{
		GatewayURL: "http://openwa-gateway:2785", GatewayCommandSecret: "01234567890123456789012345678901",
		GatewayMaxResponse:       1 << 20,
		MetaCloudCredentialsJSON: `[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`,
	}, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if router.OpenWA == nil || router.Meta == nil {
		t.Fatalf("Meta transport not enabled: %#v", router)
	}
}

func TestBuildCampaignTransportRouterRejectsMalformedMetaCredentials(t *testing.T) {
	_, err := buildCampaignTransportRouter(workerconfig.CampaignConfig{
		GatewayURL: "http://openwa-gateway:2785", GatewayCommandSecret: "01234567890123456789012345678901",
		GatewayMaxResponse: 1 << 20, MetaCloudCredentialsJSON: `[{"key":"bad"}]`,
	}, &http.Client{})
	if err == nil {
		t.Fatal("malformed Meta credentials were accepted")
	}
}

func TestBuildCampaignTransportRouterAllowsMetaOnlyTransport(t *testing.T) {
	router, err := buildCampaignTransportRouter(workerconfig.CampaignConfig{
		GatewayMaxResponse:       1 << 20,
		MetaCloudCredentialsJSON: `[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`,
	}, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if router.OpenWA != nil || router.Meta == nil {
		t.Fatalf("Meta-only router retained OpenWA dependency: %#v", router)
	}
}

func TestBuildCampaignTransportRouterAllowsGovernedNodeAddressingWithoutStaticURL(t *testing.T) {
	router, err := buildCampaignTransportRouter(workerconfig.CampaignConfig{
		GatewayCommandSecret: "01234567890123456789012345678901",
		GatewayMaxResponse:   1 << 20,
	}, &http.Client{})
	if err != nil {
		t.Fatalf("node-addressed OpenWA router was rejected: %v", err)
	}
	if router.OpenWA == nil {
		t.Fatal("node-addressed OpenWA transport was not enabled")
	}
}
