package execution

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/sender"
)

type routingCampaignReader struct{ value campaign.Campaign }

func (r routingCampaignReader) Get(context.Context, string) (campaign.Campaign, error) {
	return r.value, nil
}

func governedRoutingDependencies(t *testing.T, start, end time.Time) (*provider.Service, *sender.GatewayPoolService) {
	t.Helper()
	providerStore := provider.NewMemoryStore()
	for _, item := range []provider.Definition{
		{ID: "def-web", Provider: "OPENWA", Channel: provider.ChannelWhatsApp, Engine: "WHATSAPP_WEB_JS", AdapterVersion: "0.13.0", Capabilities: []provider.Capability{provider.CapabilitySendText}, Status: provider.StatusActive, EffectiveFrom: start.Add(-time.Hour), EffectiveTo: &end, Version: 1, CreatedBy: "maker", ApprovedBy: "checker", Reason: "approved route definition", CreatedAt: start.Add(-time.Hour), UpdatedAt: start.Add(-time.Hour)},
		{ID: "def-baileys", Provider: "OPENWA", Channel: provider.ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "0.13.0", Capabilities: []provider.Capability{provider.CapabilitySendText}, Status: provider.StatusActive, EffectiveFrom: start.Add(-time.Hour), EffectiveTo: &end, Version: 1, CreatedBy: "maker", ApprovedBy: "checker", Reason: "approved route definition", CreatedAt: start.Add(-time.Hour), UpdatedAt: start.Add(-time.Hour)},
	} {
		if _, err := providerStore.Create(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	gatewayStore := sender.NewMemoryGovernanceStore()
	service := &sender.GatewayPoolService{Store: gatewayStore}
	for _, item := range []sender.GatewayPool{
		{ID: "gw-a", Name: "web", Provider: sender.GatewayProviderOpenWA, Engine: sender.GatewayEngineWhatsAppWebJS, AdapterVersion: "0.13.0", Status: sender.GatewayPoolActive, Capabilities: []sender.Capability{sender.CapabilitySendText}, MinimumHealthyNodes: 1},
		{ID: "gw-b", Name: "baileys", Provider: sender.GatewayProviderOpenWA, Engine: sender.GatewayEngineBaileys, AdapterVersion: "0.13.0", Status: sender.GatewayPoolActive, Capabilities: []sender.Capability{sender.CapabilitySendText}, MinimumHealthyNodes: 1},
	} {
		if _, err := service.Create(context.Background(), item, "actor", "approved gateway route"); err != nil {
			t.Fatal(err)
		}
	}
	return &provider.Service{Store: providerStore}, service
}

func TestRoutingAdministrationCreatesReservationsForEveryPool(t *testing.T) {
	start := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	store := NewMemoryRoutingPlanStore()
	providers, gateways := governedRoutingDependencies(t, start, end.Add(time.Hour))
	svc := &RoutingAdministration{Store: store, Campaigns: routingCampaignReader{value: campaign.Campaign{ID: "campaign-1", MaximumUniqueRecipients: 1000, RequestedStartAt: &start, CompletionDeadlineAt: &end, Transport: campaign.TransportSelection{RequiredCapabilities: []string{"SEND_TEXT"}}}}, ProviderCapabilities: providers, GatewayPools: gateways, Clock: func() time.Time { return start.Add(-time.Hour) }}
	plan := validPlan()
	plan.ApprovedBy = ""
	plan.ApprovedAt = time.Time{}
	created, err := svc.CreateApproved(context.Background(), plan, "approver-1")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.ApprovedBy != "approver-1" || created.Routes[0].ProviderDefinitionID == "" || created.Routes[0].GatewayPoolVersion != 1 {
		t.Fatalf("unexpected created plan: %+v", created)
	}
	reservations, err := svc.Reservations(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reservations) != 2 {
		t.Fatalf("reservations=%d", len(reservations))
	}
	for _, r := range reservations {
		if r.Status != "HELD" || !r.ReservationStart.Equal(start) || !r.ReservationEnd.Equal(end) {
			t.Fatalf("unexpected reservation: %+v", r)
		}
	}
	if err := svc.Release(context.Background(), created.ID, "operator-1"); err != nil {
		t.Fatal(err)
	}
	reservations, _ = svc.Reservations(context.Background(), created.ID)
	for _, r := range reservations {
		if r.Status != "RELEASED" {
			t.Fatalf("status=%s", r.Status)
		}
	}
}

func TestRoutingAdministrationAssignsMonotonicPlanVersions(t *testing.T) {
	start := time.Date(2026, 8, 6, 8, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	store := NewMemoryRoutingPlanStore()
	providers, gateways := governedRoutingDependencies(t, start, end.Add(time.Hour))
	svc := &RoutingAdministration{Store: store, Campaigns: routingCampaignReader{value: campaign.Campaign{ID: "campaign-1", MaximumUniqueRecipients: 1000, RequestedStartAt: &start, CompletionDeadlineAt: &end, Transport: campaign.TransportSelection{RequiredCapabilities: []string{"SEND_TEXT"}}}}, ProviderCapabilities: providers, GatewayPools: gateways, Clock: func() time.Time { return start.Add(-time.Hour) }}
	first, err := svc.CreateApproved(context.Background(), validPlan(), "approver-1")
	if err != nil {
		t.Fatal(err)
	}
	secondInput := validPlan()
	secondInput.Version = 99
	secondInput.IdempotencyKey = "routing-plan-campaign-1-second"
	second, err := svc.CreateApproved(context.Background(), secondInput, "approver-2")
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 2 {
		t.Fatalf("unexpected versions: first=%d second=%d", first.Version, second.Version)
	}
}

func TestRoutingPlanPoolReportIncludesApprovedCapacity(t *testing.T) {
	store := NewMemoryRoutingPlanStore()
	plan := RoutingPlan{ID: "rp-report", CampaignID: "c-report", Version: 1, Routes: []PoolRoute{{SenderPoolID: "p1", GatewayPoolID: "g1", Provider: "OPENWA", Engine: "BAILEYS", AllocationWeight: 1, MaximumRecipients: 100, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 100}}, RoutingPolicyVersion: "r1", CapacityEvidenceVersion: "c1", PacingPolicyVersion: "p1", FallbackMode: "NONE", ApprovedBy: "actor", ApprovedAt: time.Now()}
	if _, err := store.Create(context.Background(), plan, nil); err != nil {
		t.Fatal(err)
	}
	svc := &RoutingAdministration{Store: store}
	items, err := svc.PoolReport(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Engine != "BAILEYS" || items[0].ReservedMessagesPerMinute != 10 {
		t.Fatalf("unexpected report: %+v", items)
	}
}
