package campaign

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/provider"
	"campaign-platform/internal/sender"
)

type campaignGatewayPools struct {
	pool sender.GatewayPool
	err  error
}

func (g campaignGatewayPools) RequireCapabilities(_ context.Context, poolID string, providerName sender.GatewayProvider, engine sender.GatewayEngine, required []sender.Capability) (sender.GatewayPool, error) {
	if g.err != nil {
		return sender.GatewayPool{}, g.err
	}
	service := &sender.GatewayPoolService{Store: &singleGatewayPoolStore{pool: g.pool}}
	return service.RequireCapabilities(context.Background(), poolID, providerName, engine, required)
}

type singleGatewayPoolStore struct{ pool sender.GatewayPool }

func (s *singleGatewayPoolStore) ListGatewayPools(context.Context) ([]sender.GatewayPool, error) {
	return []sender.GatewayPool{s.pool}, nil
}
func (s *singleGatewayPoolStore) CreateGatewayPool(context.Context, sender.GatewayPool, string, string) (sender.GatewayPool, error) {
	return sender.GatewayPool{}, nil
}
func (s *singleGatewayPoolStore) UpdateGatewayPool(context.Context, string, int64, sender.GatewayPool, string, string) (sender.GatewayPool, error) {
	return sender.GatewayPool{}, nil
}
func (s *singleGatewayPoolStore) GetGatewayPool(_ context.Context, id string) (sender.GatewayPool, error) {
	if id != s.pool.ID {
		return sender.GatewayPool{}, sender.ErrSenderNotFound
	}
	return s.pool, nil
}

func matchingGatewayPool(engine sender.GatewayEngine, adapter string, capabilities ...sender.Capability) campaignGatewayPools {
	return campaignGatewayPools{pool: sender.GatewayPool{ID: "gateway-1", Provider: sender.GatewayProviderOpenWA, Engine: engine, AdapterVersion: adapter, Status: sender.GatewayPoolActive, Capabilities: capabilities}}
}

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
	if created.Transport.ProviderDefinitionID != "provider-definition" || created.Transport.ProviderDefinitionVersion != 1 {
		t.Fatalf("provider definition was not frozen: %+v", created.Transport)
	}
}

func TestCampaignCreationRequiresCompatibleGatewayPool(t *testing.T) {
	registry := activeProviderRegistry(t, "BAILEYS", "0.13.0", provider.CapabilitySendText)
	service := NewService(NewMemoryRepository()).WithProviderCapabilities(registry).WithGatewayPools(matchingGatewayPool(sender.GatewayEngineBaileys, "0.12.0", sender.CapabilitySendText))
	if _, err := service.Create(context.Background(), providerCampaignInput(EngineBaileys, "0.13.0", "SEND_TEXT")); err == nil {
		t.Fatal("expected mismatched gateway adapter to reject campaign creation")
	}
}

func TestCampaignCreationFreezesCompatibleGatewayAndProviderEvidence(t *testing.T) {
	registry := activeProviderRegistry(t, "BAILEYS", "0.13.0", provider.CapabilitySendText)
	service := NewService(NewMemoryRepository()).WithProviderCapabilities(registry).WithGatewayPools(matchingGatewayPool(sender.GatewayEngineBaileys, "0.13.0", sender.CapabilitySendText))
	created, err := service.Create(context.Background(), providerCampaignInput(EngineBaileys, "0.13.0", "SEND_TEXT"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Transport.ProviderDefinitionID != "provider-definition" || created.Transport.ProviderDefinitionVersion != 1 {
		t.Fatalf("unexpected provider binding %+v", created.Transport)
	}
}

func TestCampaignCreationResolvesProviderDefinitionAtFutureExecutionTime(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	start := now.Add(24 * time.Hour)
	store := provider.NewMemoryStore()
	currentEnd := start
	for _, definition := range []provider.Definition{
		{ID: "provider-current", Provider: "OPENWA", Channel: provider.ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "0.13.0", Capabilities: []provider.Capability{provider.CapabilitySendText}, Status: provider.StatusActive, EffectiveFrom: now.Add(-time.Hour), EffectiveTo: &currentEnd, Version: 1, CreatedBy: "maker", ApprovedBy: "checker", Reason: "current provider route", CreatedAt: now, UpdatedAt: now},
		{ID: "provider-future", Provider: "OPENWA", Channel: provider.ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "0.14.0", Capabilities: []provider.Capability{provider.CapabilitySendText}, Status: provider.StatusActive, EffectiveFrom: start, Version: 1, CreatedBy: "maker", ApprovedBy: "checker", Reason: "future provider route", CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := store.Create(context.Background(), definition); err != nil {
			t.Fatal(err)
		}
	}
	input := providerCampaignInput(EngineBaileys, "0.14.0", "SEND_TEXT")
	input.RequestedStartAt = &start
	service := NewService(NewMemoryRepository())
	service.clock = func() time.Time { return now }
	service.WithProviderCapabilities(&provider.Service{Store: store, Clock: func() time.Time { return now }})
	created, err := service.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Transport.ProviderDefinitionID != "provider-future" {
		t.Fatalf("expected future provider definition, got %+v", created.Transport)
	}
}
