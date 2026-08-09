package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/organisation"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLOrganisationPolicyBlocksProhibitedCampaignPurpose(t *testing.T) {
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

	var maker, checker, orgID string
	if err = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&maker, &checker, &orgID); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{maker, checker} {
		if _, err = db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'ORG policy actor','DISABLED',false)`, actor, "org-policy-"+actor+"@internal.invalid"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "ORG Policy "+orgID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM organisation_policy_versions WHERE organisation_id=$1::uuid`, orgID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM internal_users WHERE id IN ($1::uuid,$2::uuid)`, maker, checker)
	}()

	orgs := organisation.NewService(&OrganisationRepository{DB: db})
	policies := &organisation.PolicyAdministration{Store: &OrganisationPolicyRepository{DB: db}, Organisations: orgs}
	draft, err := policies.CreateDraft(ctx, organisation.Policy{
		OrganisationID: orgID, AllowedPurposeIDs: []string{"events"}, ProhibitedPurposeIDs: []string{"finance"},
		ContactRetentionDays: 90, CampaignRetentionDays: 365, ReportBrandName: "Governed Brand", ReportFooter: "Approved footer",
	}, maker, "govern campaign purposes")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := policies.Submit(ctx, draft.ID, draft.Version, maker, "submit for independent approval")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := policies.Decide(ctx, pending.ID, pending.Version, true, checker, "approve organisation restrictions")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != organisation.PolicyActive || approved.ApprovedBy != checker {
		t.Fatalf("approved policy=%+v", approved)
	}
	if err = policies.ValidatePurpose(ctx, orgID, "finance"); !errors.Is(err, organisation.ErrPurposeProhibited) {
		t.Fatalf("prohibited purpose err=%v", err)
	}

	campaigns := campaign.NewService(campaign.NewMemoryRepository()).WithOrganisationReader(orgs).WithOrganisationPolicies(policies)
	input := campaign.CreateInput{
		OrganisationID: orgID, Name: "Blocked Finance Campaign", PurposeID: "finance", ConsentReviewID: "review",
		MaximumUniqueRecipients: 1, MaximumMessagesPerRecipient: 1, CreatedBy: "operator",
		Transport: campaign.TransportSelection{
			Channel: "WHATSAPP", Provider: campaign.ProviderOpenWA, Engine: campaign.EngineWhatsAppWebJS,
			RoutingMode: campaign.RoutingSenderPool, SenderPoolID: "pool", GatewayPoolID: "gateway",
			AdapterVersion: "1", RoutingPolicyVersion: "1", CapacityEvidenceVersion: "1", FallbackMode: campaign.FallbackNone,
		},
	}
	if _, err = campaigns.Create(ctx, input); !errors.Is(err, organisation.ErrPurposeProhibited) {
		t.Fatalf("prohibited campaign purpose err=%v", err)
	}
	input.PurposeID = "events"
	input.Name = "Permitted Event Campaign"
	if _, err = campaigns.Create(ctx, input); err != nil {
		t.Fatalf("permitted campaign purpose rejected: %v", err)
	}
}
