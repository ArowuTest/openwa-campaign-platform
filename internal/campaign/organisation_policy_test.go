package campaign

import (
	"campaign-platform/internal/organisation"
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateRequiresActiveOrganisationPolicyPurpose(t *testing.T) {
	ctx := context.Background()
	orgs := organisation.NewService(organisation.NewMemoryRepository())
	org, err := orgs.Create(ctx, organisation.CreateInput{LegalName: "Campaign Policy Ltd", CountryISO2: "NG"})
	if err != nil {
		t.Fatal(err)
	}
	org, err = orgs.SetStatus(ctx, org.ID, organisation.StatusInput{ExpectedVersion: org.Version, Status: organisation.StatusActive, ActorID: "checker", Reason: "approved organisation"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	policies := &organisation.PolicyAdministration{Store: organisation.NewMemoryPolicyStore(), Organisations: orgs, Clock: func() time.Time { return now }}
	p, err := policies.CreateDraft(ctx, organisation.Policy{OrganisationID: org.ID, AllowedPurposeIDs: []string{"events"}, ContactRetentionDays: 365, CampaignRetentionDays: 365}, "maker", "campaign purpose policy")
	if err != nil {
		t.Fatal(err)
	}
	p, err = policies.Submit(ctx, p.ID, p.Version, "maker", "submit controlled policy")
	if err != nil {
		t.Fatal(err)
	}
	_, err = policies.Decide(ctx, p.ID, p.Version, true, "checker", "approve controlled policy")
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(NewMemoryRepository()).WithOrganisationReader(orgs).WithOrganisationPolicies(policies)
	input := CreateInput{OrganisationID: org.ID, Name: "Blocked", PurposeID: "finance", ConsentReviewID: "review-1", MaximumUniqueRecipients: 1, MaximumMessagesPerRecipient: 1, CreatedBy: "maker", Transport: TransportSelection{Channel: "WHATSAPP", Provider: ProviderOpenWA, Engine: EngineBaileys, RoutingMode: RoutingSenderPool, GatewayPoolID: "gw", SenderPoolID: "pool", AdapterVersion: "0.13.0", FallbackMode: FallbackNone, RoutingPolicyVersion: "r1", CapacityEvidenceVersion: "c1"}}
	if _, err = service.Create(ctx, input); !errors.Is(err, organisation.ErrPurposeNotPermitted) {
		t.Fatalf("expected policy rejection, got %v", err)
	}
	input.PurposeID = "events"
	if _, err = service.Create(ctx, input); err != nil {
		t.Fatalf("permitted create failed: %v", err)
	}
}
