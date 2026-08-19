package execution

import (
	"context"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/metacloud"
	"campaign-platform/internal/provider"
	"campaign-platform/internal/sender"
)

func TestRoutingRequestHashBindsMetaEndpointAndDistributionMode(t *testing.T) {
	base := RoutingPlan{CampaignID: "campaign-meta", DistributionMode: DistributionWeighted, RoutingPolicyVersion: "rp", CapacityEvidenceVersion: "cap", PacingPolicyVersion: "pace", FallbackMode: "NONE", ApprovedBy: "checker", Routes: []PoolRoute{{SenderPoolID: "pool-meta", MetaSenderID: "meta-a", Provider: "META", Engine: "CLOUD_API", AllocationWeight: 100, MaximumRecipients: 10, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 1000}}}
	a, err := base.computeRequestHash()
	if err != nil {
		t.Fatal(err)
	}
	changedSender := base
	changedSender.Routes = append([]PoolRoute(nil), base.Routes...)
	changedSender.Routes[0].MetaSenderID = "meta-b"
	b, err := changedSender.computeRequestHash()
	if err != nil {
		t.Fatal(err)
	}
	changedMode := base
	changedMode.DistributionMode = DistributionAuto
	c, err := changedMode.computeRequestHash()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || a == c {
		t.Fatalf("routing hash failed to bind Meta endpoint/mode: %s %s %s", a, b, c)
	}
}

func setupMixedRoutingGovernance(t *testing.T, now, end time.Time) (*provider.Service, *sender.GatewayPoolService, *metacloud.Service, *metacloud.MemoryTemplateStore, metacloud.Sender) {
	t.Helper()
	ps := provider.NewMemoryStore()
	for _, d := range []provider.Definition{
		{ID: "def-baileys", Provider: "OPENWA", Channel: provider.ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "0.13.0", Capabilities: []provider.Capability{provider.CapabilitySendText}, Status: provider.StatusActive, EffectiveFrom: now.Add(-time.Hour), EffectiveTo: &end, Version: 1, CreatedBy: "maker", ApprovedBy: "checker", Reason: "approved OpenWA route", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
		{ID: "def-meta", Provider: "META", Channel: provider.ChannelWhatsApp, Engine: "CLOUD_API", AdapterVersion: "1.0.0", Capabilities: []provider.Capability{provider.CapabilitySendText, provider.CapabilitySendTemplate}, Status: provider.StatusActive, EffectiveFrom: now.Add(-time.Hour), EffectiveTo: &end, Version: 1, CreatedBy: "maker", ApprovedBy: "checker", Reason: "approved Meta route", CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
	} {
		if _, err := ps.Create(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}
	gs := sender.NewMemoryGovernanceStore()
	gatewaySvc := &sender.GatewayPoolService{Store: gs}
	if _, err := gatewaySvc.Create(context.Background(), sender.GatewayPool{ID: "gw-baileys", Name: "Baileys", Provider: sender.GatewayProviderOpenWA, Engine: sender.GatewayEngineBaileys, AdapterVersion: "0.13.0", Status: sender.GatewayPoolActive, Capabilities: []sender.Capability{sender.CapabilitySendText}, MinimumHealthyNodes: 1}, "actor", "approved gateway route"); err != nil {
		t.Fatal(err)
	}
	metaStore := metacloud.NewMemoryStore()
	metaSvc := &metacloud.Service{Store: metaStore, Clock: func() time.Time { return now }}
	input := metacloud.Sender{OrganisationID: "org-1", SenderPoolID: "pool-meta", WABAID: "waba-1", PhoneNumberID: "phone-1", DisplayName: "Meta Sender", BusinessPhoneDisplay: "+234 *** 1", CredentialKey: "meta-test-1", GraphAPIVersion: "v23.0"}
	draft, err := metaSvc.CreateDraft(context.Background(), input, "maker", "create Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := metaSvc.Submit(context.Background(), draft.ID, draft.Version, "submitter", "submit Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	active, err := metaSvc.Decide(context.Background(), pending.ID, pending.Version, true, "checker", "approve Meta sender", now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := metaSvc.ObserveHealth(context.Background(), active.ID, active.Version, metacloud.HealthHealthy, now, "worker", "verified Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	templates := metacloud.NewMemoryTemplateStore()
	components := []byte(`[{"type":"BODY","text":"Hello {{1}}"}]`)
	hash, err := metacloud.CanonicalComponentHash(components)
	if err != nil {
		t.Fatal(err)
	}
	if err := templates.ReplaceWABATemplates(context.Background(), "org-1", "waba-1", []metacloud.Template{{MetaTemplateID: "tpl-1", Name: "hello", Language: "en_US", Category: "MARKETING", Status: "APPROVED", Components: components, ComponentHash: hash}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := templates.CreateBinding(context.Background(), metacloud.Binding{MessageVersionID: "message-1", TemplateName: "hello", Language: "en_US", BodyVariableNames: []string{"name"}, TemplateComponentHash: hash, CreatedBy: "checker", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return &provider.Service{Store: ps}, gatewaySvc, metaSvc, templates, healthy
}

func mixedAutoPlan(metaSenderID string) RoutingPlan {
	return RoutingPlan{CampaignID: "campaign-1", DistributionMode: DistributionAuto, RoutingPolicyVersion: "rp1", CapacityEvidenceVersion: "cap1", PacingPolicyVersion: "pace1", FallbackMode: "NONE", IdempotencyKey: "mixed-routing-plan-0001", Routes: []PoolRoute{
		{SenderPoolID: "pool-openwa", GatewayPoolID: "gw-baileys", Provider: "OPENWA", Engine: "BAILEYS", AllocationWeight: 0, MaximumRecipients: 500, ReservedMessagesPerMinute: 30, ReservedHourlyUnits: 300, ReservedDailyUnits: 3000},
		{SenderPoolID: "pool-meta", MetaSenderID: metaSenderID, Provider: "META", Engine: "CLOUD_API", AllocationWeight: 0, MaximumRecipients: 500, ReservedMessagesPerMinute: 70, ReservedHourlyUnits: 700, ReservedDailyUnits: 7000},
	}}
}

func TestRoutingAdministrationFreezesMixedMetaAndOpenWARoutesAndAutoWeights(t *testing.T) {
	now := time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC)
	start := now.Add(time.Hour)
	end := start.Add(6 * time.Hour)
	defsEnd := end.Add(time.Hour)
	providers, gateways, metaSenders, templates, metaSender := setupMixedRoutingGovernance(t, now, defsEnd)
	store := NewMemoryRoutingPlanStore()
	admin := &RoutingAdministration{Store: store, Campaigns: routingCampaignReader{value: campaign.Campaign{ID: "campaign-1", OrganisationID: "org-1", MaximumUniqueRecipients: 1000, RequestedStartAt: &start, CompletionDeadlineAt: &end, MessageVersionID: "message-1", Transport: campaign.TransportSelection{RequiredCapabilities: []string{"SEND_TEXT"}}}}, ProviderCapabilities: providers, GatewayPools: gateways, MetaSenders: metaSenders, MetaTemplates: templates, MetaHealthStaleAfter: 5 * time.Minute, Clock: func() time.Time { return now }}
	created, err := admin.CreateApproved(context.Background(), mixedAutoPlan(metaSender.ID), "approver")
	if err != nil {
		t.Fatal(err)
	}
	var openwa, meta PoolRoute
	for _, route := range created.Routes {
		if route.Provider == "META" {
			meta = route
		} else {
			openwa = route
		}
	}
	if openwa.AllocationWeight != 30 || meta.AllocationWeight != 70 {
		t.Fatalf("AUTO weights openwa=%d meta=%d", openwa.AllocationWeight, meta.AllocationWeight)
	}
	if openwa.GatewayPoolVersion != 1 || meta.GatewayPoolVersion != 0 || meta.MetaSenderVersion <= 0 || meta.ProviderDefinitionID != "def-meta" {
		t.Fatalf("unexpected frozen routes: %#v %#v", openwa, meta)
	}
}

func TestRoutingAdministrationRejectsUnavailableMetaSender(t *testing.T) {
	now := time.Date(2026, 8, 12, 2, 30, 0, 0, time.UTC)
	start := now.Add(time.Hour)
	end := start.Add(6 * time.Hour)
	defsEnd := end.Add(time.Hour)
	providers, gateways, metaSenders, templates, metaSender := setupMixedRoutingGovernance(t, now, defsEnd)
	unavailable, err := metaSenders.ObserveHealth(context.Background(), metaSender.ID, metaSender.Version, metacloud.HealthUnavailable, now.Add(time.Minute), "worker", "Meta endpoint unavailable")
	if err != nil {
		t.Fatal(err)
	}
	admin := &RoutingAdministration{Store: NewMemoryRoutingPlanStore(), Campaigns: routingCampaignReader{value: campaign.Campaign{ID: "campaign-1", OrganisationID: "org-1", MaximumUniqueRecipients: 1000, RequestedStartAt: &start, CompletionDeadlineAt: &end, MessageVersionID: "message-1", Transport: campaign.TransportSelection{RequiredCapabilities: []string{"SEND_TEXT"}}}}, ProviderCapabilities: providers, GatewayPools: gateways, MetaSenders: metaSenders, MetaTemplates: templates, MetaHealthStaleAfter: 5 * time.Minute, Clock: func() time.Time { return now.Add(time.Minute) }}
	if _, err := admin.CreateApproved(context.Background(), mixedAutoPlan(unavailable.ID), "approver"); err == nil {
		t.Fatal("unavailable Meta sender accepted")
	}
}
