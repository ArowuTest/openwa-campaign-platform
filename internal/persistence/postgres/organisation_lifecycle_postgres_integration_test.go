package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/organisation"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLOrganisationLifecycleBlocksSuspendedCampaignAndRejectsDuplicate(t *testing.T) {
	dsn := os.Getenv("POSTGRES_ORGANISATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_ORGANISATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	var actorID string
	if err = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'ORG lifecycle actor','DISABLED',false)`, actorID, "org-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(context.Background(), `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)

	repo := &OrganisationRepository{DB: db}
	orgs := organisation.NewService(repo)
	orgName := "ORG Persistent " + actorID + " Ltd"
	org, err := orgs.Create(ctx, organisation.CreateInput{LegalName: orgName})
	if err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(context.Background(), `DELETE FROM organisation_events WHERE organisation_id=$1::uuid; DELETE FROM organisations WHERE id=$1::uuid`, org.ID)
	if org.Status != organisation.StatusUnderReview {
		t.Fatalf("new organisation status=%s want=%s", org.Status, organisation.StatusUnderReview)
	}

	org, err = orgs.SetStatus(ctx, org.ID, organisation.StatusInput{
		ExpectedVersion: org.Version, Status: organisation.StatusActive,
		ActorID: actorID, Reason: "approved organisation onboarding",
	})
	if err != nil {
		t.Fatal(err)
	}
	org, err = orgs.SetStatus(ctx, org.ID, organisation.StatusInput{
		ExpectedVersion: org.Version, Status: organisation.StatusSuspended,
		ActorID: actorID, Reason: "compliance review suspension",
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := orgs.ListEvents(ctx, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].ActorID != actorID || events[1].BeforeStatus != organisation.StatusActive || events[1].AfterStatus != organisation.StatusSuspended {
		t.Fatalf("unexpected organisation events: %+v", events)
	}

	campaigns := campaign.NewService(campaign.NewMemoryRepository()).WithOrganisationReader(orgs)
	_, err = campaigns.Create(ctx, campaign.CreateInput{
		OrganisationID: org.ID, Name: "Blocked while suspended", PurposeID: "purpose", ConsentReviewID: "review",
		MaximumUniqueRecipients: 1, MaximumMessagesPerRecipient: 1, CreatedBy: "operator",
		Transport: campaign.TransportSelection{
			Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineWhatsAppWebJS,
			RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "pool", GatewayPoolID: "gateway",
			AdapterVersion: "1", RoutingPolicyVersion: "1", CapacityEvidenceVersion: "1", FallbackMode: campaign.FallbackNone,
		},
	})
	if !errors.Is(err, organisation.ErrNotActive) {
		t.Fatalf("suspended organisation campaign err=%v want=%v", err, organisation.ErrNotActive)
	}

	_, err = orgs.Create(ctx, organisation.CreateInput{LegalName: "  " + strings.ToLower(orgName) + "  "})
	if !errors.Is(err, organisation.ErrDuplicate) {
		t.Fatalf("normalised duplicate organisation err=%v want=%v", err, organisation.ErrDuplicate)
	}
}
