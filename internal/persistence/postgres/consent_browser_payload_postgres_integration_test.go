package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/organisation"
	_ "campaign-platform/internal/persistence/database"
)

// Real PostgreSQL acceptance for the same field set assembled by the Admin UI.
// Draft creation must not require evidence; submission does.
func TestPostgreSQLConsentReviewServiceAcceptsProductionShapedDraft(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	actor, orgID := complianceUUID(t, ctx, db), complianceUUID(t, ctx, db)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO internal_users(id,email,display_name,status,mfa_required)
         VALUES($1::uuid,$2,'Consent Draft Integration','ACTIVE',false)`,
		actor, "openwa-consent-draft-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`,
		orgID, "Consent Draft Integration "+orgID); err != nil {
		t.Fatal(err)
	}

	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	reviews := consent.NewService(&ConsentRepository{DB: db}).
		WithOrganisationReader(organisation.NewService(&OrganisationRepository{DB: db}))
	input := consent.CreateInput{
		OrganisationID: orgID, Scope: consent.ReviewScopeOrganisation,
		Name: "Synthetic operator consent assessment", PurposeDescription: "Synthetic campaign purpose",
		PurposeCode: "PROMOTIONAL_MESSAGING", Channel: "WHATSAPP", ConsentSource: "Test registration form",
		CollectionMethod: "explicit-checkbox", CollectionPeriodFrom: &from, CollectionPeriodTo: &to,
		ControllerRole: "Data controller", WordingVersion: "v1",
		ExactConsentWording:  "I agree to receive test communications.",
		PrivacyNoticeVersion: "v1", EvidenceAssetIDs: []string{},
		ExternalEvidenceReferences: []consent.ExternalEvidenceReference{
			{Reference: "consent-proof.pdf", SHA256Checksum: strings.Repeat("a", 64)},
		},
		PrivacyNoticeReviewed: true, OptOutProcessReviewed: true, SampleRecordsReviewed: true,
		SampleReviewNotes:  "Reviewed synthetic records without identifiers.",
		PermittedCountries: []string{"NG"}, PermittedMessageCategory: "Event promotions", CreatedBy: actor,
	}
	saved, err := reviews.Create(ctx, input)
	if err != nil {
		t.Fatalf("valid browser-shaped consent draft rejected: %v", err)
	}
	if saved.Status != consent.StatusDraft || saved.OrganisationID != orgID {
		t.Fatalf("wrong status or organisation: %q %q", saved.Status, saved.OrganisationID)
	}
	loaded, err := reviews.Get(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != consent.StatusDraft || len(loaded.ExternalEvidenceReferences) != 1 ||
		loaded.ExternalEvidenceReferences[0].SHA256Checksum != strings.Repeat("a", 64) {
		t.Fatalf("consent draft evidence was not durably stored: %#v", loaded)
	}
	var events int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM consent_review_events WHERE consent_review_id=$1::uuid AND event_type='CREATED'`,
		saved.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("expected exactly one immutable CREATED event, got %d", events)
	}
}
