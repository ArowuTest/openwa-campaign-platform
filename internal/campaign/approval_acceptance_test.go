package campaign

import (
	"testing"
	"time"
)

func TestConsentApprovalBlocksAudienceProgressionUntilApproved(t *testing.T) {
	now := time.Date(2026, 8, 8, 17, 0, 0, 0, time.UTC)
	entity := baseCampaign(t)
	entity = transition(t, entity, TransitionInput{Action: ActionSubmitConsentReview, ActorID: "maker"}, now)
	if _, err := entity.Transition(TransitionInput{Action: ActionStartAudienceBuild, ActorID: "maker", ExpectedVersion: entity.Version}, now); err == nil {
		t.Fatal("audience build started before consent approval")
	}
	entity = transition(t, entity, TransitionInput{Action: ActionApproveConsent, ActorID: "consent-checker"}, now)
	entity = transition(t, entity, TransitionInput{Action: ActionStartAudienceBuild, ActorID: "maker"}, now)
	if entity.Status != StatusAudienceBuilding {
		t.Fatalf("status=%s", entity.Status)
	}
}

func TestUnapprovedMessageCannotReachSchedulingOrDispatch(t *testing.T) {
	now := time.Date(2026, 8, 8, 17, 0, 0, 0, time.UTC)
	entity := baseCampaign(t)
	for _, step := range []TransitionInput{
		{Action: ActionSubmitConsentReview, ActorID: "maker"},
		{Action: ActionApproveConsent, ActorID: "consent-checker"},
		{Action: ActionStartAudienceBuild, ActorID: "maker"},
		{Action: ActionValidateAudience, ActorID: "maker", AudienceSnapshotID: "snapshot", AudienceSnapshotHash: "hash", EligibleAudienceCount: 10},
		{Action: ActionSubmitMessage, ActorID: "maker", MessageVersionID: "message", MessageContentHash: "content-hash"},
	} {
		entity = transition(t, entity, step, now)
	}
	for _, action := range []Action{ActionApproveCommercial, ActionRequestFinalApproval, ActionStartDispatch} {
		if _, err := entity.Transition(TransitionInput{Action: action, ActorID: "operator", ExpectedVersion: entity.Version}, now); err == nil {
			t.Fatalf("action %s succeeded while message remained unapproved", action)
		}
	}
	entity = transition(t, entity, TransitionInput{Action: ActionApproveMessage, ActorID: "message-checker"}, now)
	if entity.Status != StatusMessageApproved {
		t.Fatalf("status=%s", entity.Status)
	}
}

func TestCampaignEntitlementRejectsMoreThanOneMessagePerRecipient(t *testing.T) {
	start := time.Date(2026, 8, 8, 18, 0, 0, 0, time.UTC)
	deadline := start.Add(time.Hour)
	_, err := New(CreateInput{
		OrganisationID: "org", Name: "entitlement", PurposeID: "purpose", ConsentReviewID: "review",
		RequestedStartAt: &start, CompletionDeadlineAt: &deadline,
		MaximumUniqueRecipients: 100, MaximumMessagesPerRecipient: 2, CreatedBy: "maker", Transport: validTransport(),
	}, start.Add(-time.Hour))
	if err == nil {
		t.Fatal("campaign accepted more than one message per recipient for initial release")
	}
}
