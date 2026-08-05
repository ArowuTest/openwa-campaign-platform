package campaign

import (
	"context"
	"errors"
	"testing"
	"time"
)

type consentReviewGateStub struct{ err error }

func (s consentReviewGateStub) ValidateCampaignReview(context.Context, string, string, string, time.Time) error {
	return s.err
}

func TestCampaignConsentReviewRecheckedAtMaterialBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	start, deadline := now.Add(time.Hour), now.Add(2*time.Hour)
	entity, err := New(CreateInput{OrganisationID: "org", Name: "campaign", PurposeID: "purpose", ConsentReviewID: "review", RequestedStartAt: &start, CompletionDeadlineAt: &deadline, MaximumUniqueRecipients: 10, MaximumMessagesPerRecipient: 1, CreatedBy: "maker", Transport: TransportSelection{Channel: "WHATSAPP", Provider: ProviderOpenWA, Engine: EngineBaileys, RoutingMode: RoutingSenderPool, GatewayPoolID: "gateway", SenderPoolID: "sender", AdapterVersion: "v1", FallbackMode: FallbackNone, RoutingPolicyVersion: "r1", CapacityEvidenceVersion: "c1"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewMemoryRepository()
	if err := repo.Create(context.Background(), entity); err != nil {
		t.Fatal(err)
	}
	gateErr := errors.New("consent review expired")
	svc := NewService(repo).WithConsentReviews(consentReviewGateStub{err: gateErr})

	pending, err := svc.Transition(context.Background(), entity.ID, TransitionInput{Action: ActionSubmitConsentReview, ActorID: "maker", ExpectedVersion: entity.Version})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Transition(context.Background(), entity.ID, TransitionInput{Action: ActionApproveConsent, ActorID: "checker", ExpectedVersion: pending.Version})
	if !errors.Is(err, gateErr) {
		t.Fatalf("expected consent review gate, got %v", err)
	}
}

func TestCampaignSafetyActionsRemainAvailableAfterReviewInvalidation(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	entity, err := New(CreateInput{OrganisationID: "org", Name: "campaign", PurposeID: "purpose", ConsentReviewID: "review", MaximumUniqueRecipients: 10, MaximumMessagesPerRecipient: 1, CreatedBy: "maker", Transport: TransportSelection{Channel: "WHATSAPP", Provider: ProviderOpenWA, Engine: EngineBaileys, RoutingMode: RoutingSenderPool, GatewayPoolID: "gateway", SenderPoolID: "sender", AdapterVersion: "v1", FallbackMode: FallbackNone, RoutingPolicyVersion: "r1", CapacityEvidenceVersion: "c1"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	entity.Status = StatusScheduled
	repo := NewMemoryRepository()
	if err := repo.Create(context.Background(), entity); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo).WithConsentReviews(consentReviewGateStub{err: errors.New("expired")})
	cancelled, err := svc.Transition(context.Background(), entity.ID, TransitionInput{Action: ActionCancel, ActorID: "operator", Reason: "review invalid", ExpectedVersion: entity.Version})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != StatusCancelled {
		t.Fatalf("status=%s", cancelled.Status)
	}
}
