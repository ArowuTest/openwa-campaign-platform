package main

import (
	"context"
	"slices"
	"testing"
	"time"

	"campaign-platform/internal/execution"
	"campaign-platform/internal/message"
	"campaign-platform/internal/metacloud"
	"campaign-platform/internal/platform/httpserver"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/shared/config"
)

func TestApplyMetaControlRuntimeWiresWebhookDeliveriesAndConversationWindow(t *testing.T) {
	resolver := &metacloud.PostgreSQLWebhookDeliveryResolver{}
	windows := &metacloud.PostgreSQLConversationWindowStore{}
	deps := httpserver.Dependencies{}
	applyMetaControlRuntime(&deps, metaControlRuntime{WebhookDeliveries: resolver, ConversationWindows: windows, ConversationWindow: 12 * time.Hour})
	if deps.MetaWebhookDeliveries != resolver {
		t.Fatalf("Meta webhook delivery resolver was not wired into HTTP dependencies: %#v", deps.MetaWebhookDeliveries)
	}
	if deps.MetaConversationWindows != windows || deps.MetaConversationWindow != 12*time.Hour {
		t.Fatalf("Meta conversation-window runtime was not wired: store=%#v window=%s", deps.MetaConversationWindows, deps.MetaConversationWindow)
	}
}

func TestBuildMemoryMetaControlRuntimeLeavesExternalCallsOptional(t *testing.T) {
	messages := message.NewService(message.NewMemoryRepository())
	runtime, err := buildMemoryMetaControlRuntime(config.Config{}, messages)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Senders == nil || runtime.Templates == nil || runtime.WebhookSenders == nil {
		t.Fatalf("Meta governance stores were not constructed: %#v", runtime)
	}
	if runtime.Credentials != nil || runtime.Verifier != nil || runtime.Templates.Client != nil {
		t.Fatalf("Meta external client unexpectedly enabled: %#v", runtime)
	}
}

func TestBuildMemoryMetaControlRuntimeEnablesCredentialBackedClient(t *testing.T) {
	messages := message.NewService(message.NewMemoryRepository())
	runtime, err := buildMemoryMetaControlRuntime(config.Config{MetaCloudCredentialsJSON: `[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`}, messages)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Credentials == nil || runtime.Verifier == nil || runtime.Templates.Client == nil {
		t.Fatalf("Meta credential-backed client was not fully wired: %#v", runtime)
	}
}

func TestBuildMemoryMetaControlRuntimeRejectsMalformedCredentials(t *testing.T) {
	messages := message.NewService(message.NewMemoryRepository())
	if _, err := buildMemoryMetaControlRuntime(config.Config{MetaCloudCredentialsJSON: `[{"key":"bad"}]`}, messages); err == nil {
		t.Fatal("malformed Meta credentials were accepted")
	}
}

func TestBootstrapProviderCapabilitiesIncludesMetaCloudWithoutPairing(t *testing.T) {
	store := provider.NewMemoryStore()
	if err := bootstrapProviderCapabilities(context.Background(), store, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	values, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var meta *provider.Definition
	for i := range values {
		if values[i].Provider == "META" && values[i].Engine == "CLOUD_API" {
			meta = &values[i]
		}
	}
	if meta == nil || !slices.Contains(meta.Capabilities, provider.CapabilitySendTemplate) || !slices.Contains(meta.Capabilities, provider.CapabilityInbound) {
		t.Fatalf("Meta Cloud provider definition missing required capabilities: %#v", meta)
	}
	if slices.Contains(meta.Capabilities, provider.CapabilityPairingQR) || slices.Contains(meta.Capabilities, provider.CapabilityPairingCode) {
		t.Fatalf("Meta Cloud provider must not expose browser/device pairing: %#v", meta.Capabilities)
	}
}

func TestApplyMetaRoutingRuntimeWiresGovernanceAndHealthWindow(t *testing.T) {
	messages := message.NewService(message.NewMemoryRepository())
	runtime, err := buildMemoryMetaControlRuntime(config.Config{}, messages)
	if err != nil {
		t.Fatal(err)
	}
	routing := &execution.RoutingAdministration{}
	applyMetaRoutingRuntime(routing, runtime, 7*time.Minute)
	if routing.MetaSenders != runtime.Senders.Store || routing.MetaTemplates != runtime.Templates.Store {
		t.Fatalf("Meta routing governance was not wired: %#v", routing)
	}
	if routing.MetaHealthStaleAfter != 7*time.Minute {
		t.Fatalf("Meta routing health window=%s want=7m", routing.MetaHealthStaleAfter)
	}
}
