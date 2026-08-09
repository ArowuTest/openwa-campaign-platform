package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/consent"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLConsentReviewLifecyclePersistsGovernanceEvidence(t *testing.T) {
	dsn := os.Getenv("POSTGRES_CONSENT_REVIEW_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_CONSENT_REVIEW_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var maker, checker, revoker, orgID string
	if err = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&maker, &checker, &revoker, &orgID); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{maker, checker, revoker} {
		if _, err = db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'CRV actor','DISABLED',false)`, actor, "crv-"+actor+"@internal.invalid"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "CRV Organisation "+orgID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	from, to := now.AddDate(0, -2, 0), now.AddDate(0, -1, 0)
	expires, nextReview := now.Add(30*24*time.Hour), now.Add(20*24*time.Hour)
	service := consent.NewService(&ConsentRepository{DB: db}).WithOrganisationReader(&OrganisationRepository{DB: db})
	input := consent.CreateInput{
		OrganisationID: orgID, Scope: consent.ReviewScopeSource, SourceSystem: "event-registration-" + orgID,
		Name: "CRV governed consent", PurposeDescription: "Event notification and promotion", PurposeCode: "EVENT_PROMOTION",
		Channel: "WHATSAPP", ConsentSource: "registration-form:event-2026", CollectionMethod: "WEB_FORM",
		CollectionPeriodFrom: &from, CollectionPeriodTo: &to, ControllerRole: "CONTROLLER",
		WordingVersion: "v3", ExactConsentWording: "I agree to receive this event notification on WhatsApp.", PrivacyNoticeVersion: "privacy-v2",
		ExternalEvidenceReferences: []consent.ExternalEvidenceReference{{Reference: "compliance/" + orgID, SHA256Checksum: strings.Repeat("a", 64)}},
		PrivacyNoticeReviewed:      true, OptOutProcessReviewed: true, SampleRecordsReviewed: true,
		SampleReviewNotes:  "Reviewed 20 random records; direct identifiers were not retained.",
		PermittedCountries: []string{"NG"}, PermittedMessageCategory: "EVENT_NOTIFICATION",
		PermittedPartnerOrganisations: []string{"partner-a"}, Restrictions: "Nigeria event notifications only", CreatedBy: maker,
	}
	review, err := service.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	review, err = service.Submit(ctx, review.ID, consent.SubmitInput{ActorID: maker, ExpectedVersion: review.Version, Reason: "evidence ready for independent review"})
	if err != nil {
		t.Fatal(err)
	}
	review, err = service.Decide(ctx, review.ID, consent.DecisionInput{
		ReviewerID: checker, Decision: consent.StatusApproved, ApprovedWithRestrictions: true,
		ExpiresAt: &expires, NextReviewAt: &nextReview, Reason: "approved with geographic restrictions", ExpectedVersion: review.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if review.Outcome != consent.OutcomeApprovedWithRestrictions || review.ReviewedBy != checker || review.ReviewedAt == nil || review.ExpiresAt == nil || review.Restrictions == "" {
		t.Fatalf("approved review missing decision evidence: %+v", review)
	}
	persisted, err := service.Get(ctx, review.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.CollectionMethod != "WEB_FORM" || persisted.ConsentSource != input.ConsentSource || persisted.PurposeCode != "EVENT_PROMOTION" || persisted.WordingVersion != "v3" || persisted.ExactConsentWording == "" || persisted.ControllerRole != "CONTROLLER" || len(persisted.PermittedPartnerOrganisations) != 1 {
		t.Fatalf("persisted review lost provenance/scope: %+v", persisted)
	}
	if err = service.ValidateCampaignReview(ctx, review.ID, "campaign-any", orgID, "WHATSAPP", now.Add(time.Hour)); err != nil {
		t.Fatalf("valid approved review rejected before expiry: %v", err)
	}
	if err = service.ValidateCampaignReview(ctx, review.ID, "campaign-any", orgID, "WHATSAPP", expires.Add(time.Second)); err == nil {
		t.Fatal("expired approved review remained campaign-eligible")
	}

	var initialEvents string
	if err = db.QueryRowContext(ctx, `SELECT string_agg(event_type,',' ORDER BY review_version) FROM consent_review_events WHERE consent_review_id=$1::uuid`, review.ID).Scan(&initialEvents); err != nil {
		t.Fatal(err)
	}
	if initialEvents != "CREATED,SUBMITTED,APPROVED_WITH_RESTRICTIONS" {
		t.Fatalf("unexpected initial review events=%q", initialEvents)
	}
	if _, err = db.ExecContext(ctx, `UPDATE consent_review_events SET reason='tampered' WHERE consent_review_id=$1::uuid`, review.ID); err == nil {
		t.Fatal("consent review audit event mutation was accepted")
	}
	revisionInput := input
	revisionInput.Name = "CRV governed consent revision"
	revisionInput.WordingVersion = "v4"
	revisionInput.ExactConsentWording = "I agree to receive the revised event notification on WhatsApp."
	revisionInput.RevisionReason = "consent wording materially changed"
	replacement, err := service.CreateRevision(ctx, review.ID, review.Version, revisionInput)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := service.Get(ctx, review.ID)
	if err != nil {
		t.Fatal(err)
	}
	if previous.Status != consent.StatusSuperseded || previous.SupersededByID != replacement.ID || replacement.ParentReviewID != previous.ID {
		t.Fatalf("revision lineage incomplete: previous=%+v replacement=%+v", previous, replacement)
	}
	if err = service.ValidateCampaignReview(ctx, previous.ID, "campaign-any", orgID, "WHATSAPP", time.Now().UTC()); err == nil {
		t.Fatal("superseded consent review remained campaign-eligible")
	}

	replacement, err = service.Submit(ctx, replacement.ID, consent.SubmitInput{ActorID: maker, ExpectedVersion: replacement.Version, Reason: "revised evidence ready for review"})
	if err != nil {
		t.Fatal(err)
	}
	expires2 := expires.Add(30 * 24 * time.Hour)
	replacement, err = service.Decide(ctx, replacement.ID, consent.DecisionInput{
		ReviewerID: checker, Decision: consent.StatusApproved, ApprovedWithRestrictions: true,
		ExpiresAt: &expires2, Reason: "revised wording independently approved", ExpectedVersion: replacement.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	replacement, err = service.Revoke(ctx, replacement.ID, consent.RevokeInput{
		ActorID: revoker, ExpectedVersion: replacement.Version, Reason: "source consent authority withdrawn",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Status != consent.StatusRevoked || replacement.RevokedBy != revoker || replacement.RevokedAt == nil || replacement.RevokeReason == "" {
		t.Fatalf("revocation evidence incomplete: %+v", replacement)
	}

	var oldEvents, replacementEvents string
	if err = db.QueryRowContext(ctx, `SELECT string_agg(event_type,',' ORDER BY review_version) FROM consent_review_events WHERE consent_review_id=$1::uuid`, previous.ID).Scan(&oldEvents); err != nil {
		t.Fatal(err)
	}
	if oldEvents != "CREATED,SUBMITTED,APPROVED_WITH_RESTRICTIONS,SUPERSEDED" {
		t.Fatalf("unexpected superseded review events=%q", oldEvents)
	}
	if err = db.QueryRowContext(ctx, `SELECT string_agg(event_type,',' ORDER BY review_version) FROM consent_review_events WHERE consent_review_id=$1::uuid`, replacement.ID).Scan(&replacementEvents); err != nil {
		t.Fatal(err)
	}
	if replacementEvents != "CREATED,SUBMITTED,APPROVED_WITH_RESTRICTIONS,REVOKED" {
		t.Fatalf("unexpected replacement review events=%q", replacementEvents)
	}
}
