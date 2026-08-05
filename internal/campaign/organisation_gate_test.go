package campaign

import (
	"context"
	"errors"
	"testing"

	"campaign-platform/internal/organisation"
)

func TestCreateRequiresActiveOrganisationWhenGovernanceIsConfigured(t *testing.T) {
	orgs := organisation.NewService(organisation.NewMemoryRepository())
	org, err := orgs.Create(context.Background(), organisation.CreateInput{LegalName: "Governed Org"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewMemoryRepository()).WithOrganisationReader(orgs)
	input := CreateInput{
		OrganisationID: org.ID, Name: "Campaign", PurposeID: "purpose", ConsentReviewID: "review",
		MaximumUniqueRecipients: 1, MaximumMessagesPerRecipient: 1, CreatedBy: "operator",
		Transport: TransportSelection{Channel: "WHATSAPP", Provider: ProviderOpenWA, Engine: EngineWhatsAppWebJS, RoutingMode: RoutingSenderPool, SenderPoolID: "pool", GatewayPoolID: "gateway", AdapterVersion: "1", RoutingPolicyVersion: "1", CapacityEvidenceVersion: "1", FallbackMode: FallbackNone},
	}
	if _, err := svc.Create(context.Background(), input); !errors.Is(err, organisation.ErrNotActive) {
		t.Fatalf("expected inactive organisation rejection, got %v", err)
	}
	org, err = orgs.SetStatus(context.Background(), org.ID, organisation.StatusInput{ExpectedVersion: org.Version, Status: organisation.StatusActive, ActorID: "approver", Reason: "approved onboarding"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(context.Background(), input); err != nil {
		t.Fatalf("active organisation rejected: %v", err)
	}
}

func TestCampaignProgressionStopsAfterOrganisationSuspension(t *testing.T) {
	ctx := context.Background()
	orgs := organisation.NewService(organisation.NewMemoryRepository())
	org, err := orgs.Create(ctx, organisation.CreateInput{LegalName: "Governed Org"})
	if err != nil {
		t.Fatal(err)
	}
	org, err = orgs.SetStatus(ctx, org.ID, organisation.StatusInput{ExpectedVersion: org.Version, Status: organisation.StatusActive, ActorID: "approver", Reason: "approved onboarding"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewMemoryRepository()).WithOrganisationReader(orgs)
	created, err := svc.Create(ctx, CreateInput{
		OrganisationID: org.ID, Name: "Campaign", PurposeID: "purpose", ConsentReviewID: "review",
		MaximumUniqueRecipients: 1, MaximumMessagesPerRecipient: 1, CreatedBy: "operator",
		Transport: TransportSelection{Channel: "WHATSAPP", Provider: ProviderOpenWA, Engine: EngineWhatsAppWebJS, RoutingMode: RoutingSenderPool, SenderPoolID: "pool", GatewayPoolID: "gateway", AdapterVersion: "1", RoutingPolicyVersion: "1", CapacityEvidenceVersion: "1", FallbackMode: FallbackNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	org, err = orgs.SetStatus(ctx, org.ID, organisation.StatusInput{ExpectedVersion: org.Version, Status: organisation.StatusSuspended, ActorID: "compliance", Reason: "consent concern"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Transition(ctx, created.ID, TransitionInput{Action: ActionSubmitConsentReview, ActorID: "operator", ExpectedVersion: created.Version})
	if !errors.Is(err, organisation.ErrNotActive) {
		t.Fatalf("expected suspended organisation to block progression, got %v", err)
	}
	// Safety controls remain available when an organisation is inactive.
	cancelled, err := svc.Transition(ctx, created.ID, TransitionInput{Action: ActionCancel, ActorID: "operator", ExpectedVersion: created.Version, Reason: "organisation suspended"})
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("cancel safety control failed: campaign=%+v err=%v", cancelled, err)
	}
}
