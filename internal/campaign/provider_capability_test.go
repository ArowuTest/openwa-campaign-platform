package campaign

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/provider"
)

func activeProviderRegistry(t *testing.T, engine, adapter string, capabilities ...provider.Capability) *provider.Service {
	t.Helper()
	store := provider.NewMemoryStore()
	now := time.Now().UTC()
	_, err := store.Create(context.Background(), provider.Definition{
		ID: "provider-definition", Provider: "OPENWA", Channel: provider.ChannelWhatsApp,
		Engine: engine, AdapterVersion: adapter, Capabilities: capabilities,
		Status: provider.StatusActive, EffectiveFrom: now.Add(-time.Hour), Version: 1,
		CreatedBy: "maker", ApprovedBy: "checker", Reason: "approved provider route", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &provider.Service{Store: store, Clock: func() time.Time { return now }}
}

func providerCampaignInput(engine Engine, adapter string, required ...string) CreateInput {
	return CreateInput{
		OrganisationID: "org-1", Name: "Provider governed campaign", PurposeID: "purpose-1", ConsentReviewID: "review-1",
		MaximumUniqueRecipients: 100, MaximumMessagesPerRecipient: 1, CreatedBy: "maker",
		Transport: TransportSelection{Channel: "WHATSAPP", Provider: ProviderOpenWA, Engine: engine, RoutingMode: RoutingSenderPool,
			GatewayPoolID: "gateway-1", SenderPoolID: "pool-1", AdapterVersion: adapter, RequiredCapabilities: required,
			FallbackMode: FallbackNone, RoutingPolicyVersion: "route-v1", CapacityEvidenceVersion: "capacity-v1"},
	}
}

func TestCampaignCreationRequiresActiveProviderCapabilities(t *testing.T) {
	service := NewService(NewMemoryRepository()).WithProviderCapabilities(activeProviderRegistry(t, "WHATSAPP_WEB_JS", "0.13.0", provider.CapabilitySendText))
	if _, err := service.Create(context.Background(), providerCampaignInput(EngineWhatsAppWebJS, "0.13.0", "SEND_VIDEO")); err == nil {
		t.Fatal("expected missing provider capability to reject campaign creation")
	}
}

func TestCampaignCreationRejectsAdapterVersionMismatch(t *testing.T) {
	service := NewService(NewMemoryRepository()).WithProviderCapabilities(activeProviderRegistry(t, "BAILEYS", "0.13.0", provider.CapabilitySendText))
	if _, err := service.Create(context.Background(), providerCampaignInput(EngineBaileys, "0.14.0", "SEND_TEXT")); err == nil {
		t.Fatal("expected adapter mismatch to reject campaign creation")
	}
}

func TestCampaignCreationAcceptsMatchingProviderDefinition(t *testing.T) {
	service := NewService(NewMemoryRepository()).WithProviderCapabilities(activeProviderRegistry(t, "BAILEYS", "0.13.0", provider.CapabilitySendText, provider.CapabilityDeliveryEvents))
	created, err := service.Create(context.Background(), providerCampaignInput(EngineBaileys, "0.13.0", "SEND_TEXT", "DELIVERY_EVENTS"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Transport.Engine != EngineBaileys {
		t.Fatalf("unexpected engine %s", created.Transport.Engine)
	}
}
