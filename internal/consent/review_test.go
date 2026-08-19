package consent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type reviewEvidenceResolver struct{}

func (reviewEvidenceResolver) ResolveConsentEvidence(_ context.Context, id string) (EvidenceAsset, error) {
	if id == "missing" {
		return EvidenceAsset{}, errors.New("missing evidence")
	}
	return EvidenceAsset{ID: id, ObjectKey: "evidence/" + id}, nil
}

func completeReviewInput(actor string) CreateInput {
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	return CreateInput{
		OrganisationID:           "org-1",
		Scope:                    ReviewScopeSource,
		SourceSystem:             "event-registration",
		Name:                     "Festival consent",
		PurposeDescription:       "Event notification and promotion",
		PurposeCode:              "EVENT_PROMOTION",
		Channel:                  "WHATSAPP",
		ConsentSource:            "registration form",
		CollectionMethod:         "WEB_FORM",
		CollectionPeriodFrom:     &from,
		CollectionPeriodTo:       &to,
		ControllerRole:           "CONTROLLER",
		WordingVersion:           "v3",
		ExactConsentWording:      "I agree to receive this event notification on WhatsApp.",
		PrivacyNoticeVersion:     "privacy-v2",
		EvidenceAssetIDs:         []string{"asset-1"},
		PrivacyNoticeReviewed:    true,
		OptOutProcessReviewed:    true,
		SampleRecordsReviewed:    true,
		SampleReviewNotes:        "Reviewed 20 randomly selected records; identifiers were not retained.",
		PermittedCountries:       []string{"ng"},
		PermittedMessageCategory: "EVENT_NOTIFICATION",
		CreatedBy:                actor,
	}
}

func TestApprovalRequiresSubmissionChecksAndIndependentReviewer(t *testing.T) {
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	input := completeReviewInput("maker")
	input.SampleRecordsReviewed = false
	review, err := NewReview(input, now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	review, err = review.Submit(SubmitInput{ActorID: "submitter", ExpectedVersion: review.Version, Reason: "ready for review"}, now)
	if err != nil {
		t.Fatal(err)
	}
	expiry := now.Add(30 * 24 * time.Hour)
	if _, err = review.Decide(DecisionInput{ReviewerID: "checker", Decision: StatusApproved, ExpiresAt: &expiry, Reason: "approved evidence", ExpectedVersion: review.Version}, now); err == nil {
		t.Fatal("expected approval to fail when mandatory checks are incomplete")
	}

	input.SampleRecordsReviewed = true
	review, err = NewReview(input, now)
	if err != nil {
		t.Fatal(err)
	}
	review, err = review.Submit(SubmitInput{ActorID: "submitter", ExpectedVersion: review.Version, Reason: "evidence is complete"}, now)
	if err != nil {
		t.Fatal(err)
	}
	expiry = now.Add(30 * 24 * time.Hour)
	if _, err = review.Decide(DecisionInput{ReviewerID: "submitter", Decision: StatusApproved, ExpiresAt: &expiry, Reason: "approved evidence", ExpectedVersion: review.Version}, now); err == nil {
		t.Fatal("expected submitter independence enforcement")
	}
	if _, err = review.Decide(DecisionInput{ReviewerID: "maker", Decision: StatusApproved, ExpiresAt: &expiry, Reason: "creator self approval", ExpectedVersion: review.Version}, now); err == nil {
		t.Fatal("creator approved own consent review after another actor submitted it")
	}
	approved, err := review.Decide(DecisionInput{ReviewerID: "checker", Decision: StatusApproved, ExpiresAt: &expiry, Reason: "approved evidence", ExpectedVersion: review.Version}, now)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != StatusApproved || approved.Outcome != OutcomeApproved || approved.ReviewedBy != "checker" {
		t.Fatalf("unexpected approval: %+v", approved)
	}
}

func TestServiceResolvesTrustedEvidenceAndSupportsRevocationAndRevision(t *testing.T) {
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository()
	service := NewService(repo)
	service.clock = func() time.Time { return now }
	service.Evidence = reviewEvidenceResolver{}

	review, err := service.Create(context.Background(), completeReviewInput("maker"))
	if err != nil {
		t.Fatal(err)
	}
	if len(review.EvidenceObjectKeys) != 1 || review.EvidenceObjectKeys[0] != "evidence/asset-1" {
		t.Fatalf("trusted evidence was not resolved: %+v", review)
	}
	review, err = service.Submit(context.Background(), review.ID, SubmitInput{ActorID: "submitter", ExpectedVersion: review.Version, Reason: "evidence is complete"})
	if err != nil {
		t.Fatal(err)
	}
	expires := now.Add(30 * 24 * time.Hour)
	review, err = service.Decide(context.Background(), review.ID, DecisionInput{ReviewerID: "checker", Decision: StatusApproved, ExpiresAt: &expires, Reason: "consent basis approved", ExpectedVersion: review.Version})
	if err != nil {
		t.Fatal(err)
	}

	revisionInput := completeReviewInput("revision-maker")
	revisionInput.Name = "Festival consent revision"
	revisionInput.RevisionReason = "privacy wording was updated"
	replacement, err := service.CreateRevision(context.Background(), review.ID, review.Version, revisionInput)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := service.Get(context.Background(), review.ID)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Status != StatusSuperseded || previous.SupersededByID != replacement.ID || replacement.ParentReviewID != previous.ID {
		t.Fatalf("revision lineage is incomplete: previous=%+v replacement=%+v", previous, replacement)
	}

	replacement, err = service.Submit(context.Background(), replacement.ID, SubmitInput{ActorID: "revision-submitter", ExpectedVersion: replacement.Version, Reason: "revision is ready"})
	if err != nil {
		t.Fatal(err)
	}
	replacement, err = service.Decide(context.Background(), replacement.ID, DecisionInput{ReviewerID: "revision-checker", Decision: StatusApproved, ExpiresAt: &expires, Reason: "revision approved", ExpectedVersion: replacement.Version})
	if err != nil {
		t.Fatal(err)
	}
	replacement, err = service.Revoke(context.Background(), replacement.ID, RevokeInput{ActorID: "compliance-admin", ExpectedVersion: replacement.Version, Reason: "consent source was withdrawn"})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Status != StatusRevoked || replacement.RevokedBy != "compliance-admin" {
		t.Fatalf("unexpected revocation: %+v", replacement)
	}
}

func TestCampaignScopedReviewCannotAuthoriseAnotherCampaign(t *testing.T) {
	now := time.Date(2026, 8, 4, 6, 0, 0, 0, time.UTC)
	review := Review{
		ID: "review", OrganisationID: "org", Scope: ReviewScopeCampaign, CampaignID: "campaign-a",
		Channel: "WHATSAPP", Status: StatusApproved, ExpiresAt: func() *time.Time { v := now.Add(time.Hour); return &v }(),
	}
	repo := NewMemoryRepository()
	if err := repo.Create(context.Background(), review); err != nil {
		t.Fatal(err)
	}
	service := NewService(repo)
	if err := service.ValidateCampaignReview(context.Background(), review.ID, "campaign-b", "org", "WHATSAPP", now); err == nil {
		t.Fatal("expected campaign-scope mismatch rejection")
	}
	if err := service.ValidateCampaignReview(context.Background(), review.ID, "campaign-a", "org", "WHATSAPP", now); err != nil {
		t.Fatal(err)
	}
}

func TestExternalEvidenceRequiresChecksumAndSampleNotesRejectPII(t *testing.T) {
	input := completeReviewInput("maker")
	input.EvidenceAssetIDs = nil
	input.ExternalEvidenceReferences = []ExternalEvidenceReference{{Reference: "compliance-case/ABC-2026-14", SHA256Checksum: strings.Repeat("a", 64)}}
	review, err := NewReview(input, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := review.ValidateForSubmission(); err != nil {
		t.Fatalf("checksummed external evidence was rejected: %v", err)
	}

	input.ExternalEvidenceReferences[0].SHA256Checksum = "not-a-checksum"
	if _, err := NewReview(input, time.Now().UTC()); err == nil {
		t.Fatal("expected invalid external evidence checksum rejection")
	}

	input = completeReviewInput("maker")
	input.SampleReviewNotes = "Reviewed sample for +2348012345678"
	if _, err := NewReview(input, time.Now().UTC()); err == nil {
		t.Fatal("expected sample telephone number rejection")
	}
	input.SampleReviewNotes = "Reviewed sample for person@example.com"
	if _, err := NewReview(input, time.Now().UTC()); err == nil {
		t.Fatal("expected sample email address rejection")
	}
}
