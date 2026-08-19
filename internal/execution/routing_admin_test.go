package execution

import (
	"context"
	"errors"
	"strings"
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
func TestRoutingAdministrationRejectsReleaseForDispatchingCampaign(t *testing.T) {
	store := NewMemoryRoutingPlanStore()
	plan := RoutingPlan{
		ID: "routing-plan-active", CampaignID: "campaign-active",
		IdempotencyKey: "routing-plan-active-request",
	}
	reservation := CapacityReservation{
		ID: "reservation-active", CampaignID: plan.CampaignID,
		RoutingPlanID: plan.ID, SenderPoolID: "sender-pool",
		Status: "ACTIVE", FencingVersion: 2,
	}
	if _, err := store.Create(
		context.Background(), plan, []CapacityReservation{reservation},
	); err != nil {
		t.Fatal(err)
	}
	admin := &RoutingAdministration{
		Store: store,
		Campaigns: routingCampaignReader{value: campaign.Campaign{
			ID: plan.CampaignID, Status: campaign.StatusDispatching,
		}},
	}
	if err := admin.Release(
		context.Background(), plan.ID, "operator",
	); !errors.Is(err, ErrRoutingPlanConflict) {
		t.Fatalf("dispatching reservation release error=%v", err)
	}
	reservations, err := store.Reservations(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reservations[0].Status != "ACTIVE" {
		t.Fatalf("dispatching reservation was released: %+v", reservations[0])
	}
}

func TestRoutingAdministrationValidateForExecutionRejectsRouteLessPlan(t *testing.T) {
	plan := validPlan()
	plan.Routes = nil
	svc := &RoutingAdministration{ProviderCapabilities: &provider.Service{}}
	entity := campaign.Campaign{MaximumUniqueRecipients: 10}
	if err := svc.ValidateForExecution(context.Background(), plan, entity, time.Now().UTC()); !errors.Is(err, ErrRoutingPlanInvalid) {
		t.Fatalf("route-less quarantined plan must fail structural validation, got %v", err)
	}
}

func TestRoutingAdministrationClassifiesMalformedMinimumGatewayVersionAsInvalid(t *testing.T) {
	start := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	providerStore := provider.NewMemoryStore()
	if _, err := providerStore.Create(context.Background(), provider.Definition{
		ID: "def-malformed-minimum", Provider: "OPENWA", Channel: provider.ChannelWhatsApp,
		Engine: "WHATSAPP_WEB_JS", AdapterVersion: "0.13.0", MinimumGatewayVersion: "not-a-version",
		Capabilities: []provider.Capability{provider.CapabilitySendText}, Status: provider.StatusActive,
		EffectiveFrom: start.Add(-time.Hour), EffectiveTo: &end, Version: 1,
		CreatedBy: "maker", ApprovedBy: "checker", Reason: "malformed minimum test",
		CreatedAt: start.Add(-time.Hour), UpdatedAt: start.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	gatewayStore := sender.NewMemoryGovernanceStore()
	gateways := &sender.GatewayPoolService{Store: gatewayStore}
	if _, err := gateways.Create(context.Background(), sender.GatewayPool{
		ID: "gw-malformed-minimum", Name: "web", Provider: sender.GatewayProviderOpenWA,
		Engine: sender.GatewayEngineWhatsAppWebJS, AdapterVersion: "0.13.0", Status: sender.GatewayPoolActive,
		Capabilities: []sender.Capability{sender.CapabilitySendText}, MinimumHealthyNodes: 1,
	}, "actor", "approved gateway route"); err != nil {
		t.Fatal(err)
	}
	plan := RoutingPlan{
		CampaignID: "campaign-malformed-minimum", RoutingPolicyVersion: "rp1", CapacityEvidenceVersion: "cap1",
		PacingPolicyVersion: "pace1", FallbackMode: "NONE", IdempotencyKey: "malformed-minimum-plan",
		Routes: []PoolRoute{{
			SenderPoolID: "pool-a", GatewayPoolID: "gw-malformed-minimum", Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS",
			AllocationWeight: 1, MaximumRecipients: 10, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 100,
		}},
	}
	svc := &RoutingAdministration{
		Store: NewMemoryRoutingPlanStore(),
		Campaigns: routingCampaignReader{value: campaign.Campaign{
			ID: plan.CampaignID, MaximumUniqueRecipients: 10, RequestedStartAt: &start, CompletionDeadlineAt: &end,
			Transport: campaign.TransportSelection{RequiredCapabilities: []string{"SEND_TEXT"}},
		}},
		ProviderCapabilities: &provider.Service{Store: providerStore}, GatewayPools: gateways,
		Clock: func() time.Time { return start.Add(-30 * time.Minute) },
	}
	_, err := svc.CreateApproved(context.Background(), plan, "approver")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "invalid") || strings.Contains(strings.ToLower(err.Error()), "below") {
		t.Fatalf("malformed minimum gateway version classification=%v", err)
	}
}

func TestRoutingAdministrationRejectsNewPlanForNonPlannableCampaign(t *testing.T) {
	start := time.Date(2026, 8, 15, 20, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	for _, status := range []campaign.Status{campaign.StatusDispatching, campaign.StatusPaused, campaign.StatusCompleted, campaign.StatusCompletedWithExceptions, campaign.StatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			store := NewMemoryRoutingPlanStore()
			providers, gateways := governedRoutingDependencies(t, start, end.Add(time.Hour))
			svc := &RoutingAdministration{
				Store:                store,
				Campaigns:            routingCampaignReader{value: campaign.Campaign{ID: "campaign-1", Status: status, MaximumUniqueRecipients: 1000, RequestedStartAt: &start, CompletionDeadlineAt: &end, Transport: campaign.TransportSelection{RequiredCapabilities: []string{"SEND_TEXT"}}}},
				ProviderCapabilities: providers, GatewayPools: gateways, Clock: func() time.Time { return start.Add(-time.Hour) },
			}
			if _, err := svc.CreateApproved(context.Background(), validPlan(), "approver-1"); !errors.Is(err, ErrRoutingPlanConflict) {
				t.Fatalf("status %s allowed a new approved routing plan: %v", status, err)
			}
			plans, err := store.ListByCampaign(context.Background(), "campaign-1")
			if err != nil {
				t.Fatal(err)
			}
			if len(plans) != 0 {
				t.Fatalf("status %s persisted %d routing plans", status, len(plans))
			}
		})
	}
}
