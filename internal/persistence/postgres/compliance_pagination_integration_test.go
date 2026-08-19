package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/commercial"
	"campaign-platform/internal/consent"
	"campaign-platform/internal/organisation"
	_ "campaign-platform/internal/persistence/database"
)

func openCompliancePaginationDB(t *testing.T) (*sql.DB, context.Context, context.CancelFunc) {
	t.Helper()
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := db.PingContext(ctx); err != nil {
		cancel()
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); db.Close() })
	return db, ctx, cancel
}

func complianceUUID(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedComplianceActorOrganisation(t *testing.T, ctx context.Context, db *sql.DB) (string, string) {
	t.Helper()
	actor, org := complianceUUID(t, ctx, db), complianceUUID(t, ctx, db)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Pagination Actor','DISABLED',false)`, actor, "compliance-pagination-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'UNDER_REVIEW')`, org, "Pagination Organisation "+org); err != nil {
		t.Fatal(err)
	}
	return actor, org
}

func TestPostgreSQLOrganisationPolicyPaginationContinuesWithoutSkipping(t *testing.T) {
	db, ctx, _ := openCompliancePaginationDB(t)
	actor, org := seedComplianceActorOrganisation(t, ctx, db)
	admin := &organisation.PolicyAdministration{Store: &OrganisationPolicyRepository{DB: db}}
	base := time.Date(2099, 5, 1, 12, 0, 0, 0, time.UTC)
	created := make([]organisation.Policy, 0, 3)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		admin.Clock = func() time.Time { return at }
		v, e := admin.CreateDraft(ctx, organisation.Policy{OrganisationID: org, ContactRetentionDays: 30, CampaignRetentionDays: 90}, actor, "pagination policy")
		if e != nil {
			t.Fatal(e)
		}
		created = append(created, v)
	}
	first, e := admin.ListPage(ctx, org, 2, "")
	if e != nil {
		t.Fatal(e)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != created[2].ID || first.Items[1].ID != created[1].ID {
		t.Fatalf("unexpected first organisation policy page: %#v", first)
	}
	second, e := admin.ListPage(ctx, org, 2, first.NextCursor)
	if e != nil {
		t.Fatal(e)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != created[0].ID {
		t.Fatalf("unexpected second organisation policy page: %#v", second)
	}
}

func TestPostgreSQLCommercialPaginationContinuesWithoutSkipping(t *testing.T) {
	db, ctx, _ := openCompliancePaginationDB(t)
	actor, org := seedComplianceActorOrganisation(t, ctx, db)
	purpose := complianceUUID(t, ctx, db)
	if _, e := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,'PAGINATION','Pagination','WHATSAPP','v1')`, purpose, org); e != nil {
		t.Fatal(e)
	}
	service := &commercial.Service{Store: &CommercialRepository{DB: db}}
	base := time.Date(2099, 5, 2, 12, 0, 0, 0, time.UTC)
	created := make([]commercial.Record, 0, 3)
	for i := 0; i < 3; i++ {
		campaign := complianceUUID(t, ctx, db)
		if _, e := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status) VALUES($1::uuid,$2::uuid,$3,$4::uuid,'DRAFT')`, campaign, org, "Pagination Campaign "+campaign, purpose); e != nil {
			t.Fatal(e)
		}
		at := base.Add(time.Duration(i) * time.Minute)
		service.Clock = func() time.Time { return at }
		v, e := service.CreateDraft(ctx, commercial.Record{CampaignID: campaign, OrganisationID: org, QuotationReference: "q", InvoiceReference: "i", Currency: "GBP", ApprovedRecipients: 1, UnitPriceMinor: 100, TotalAmountMinor: 100}, actor, "pagination commercial")
		if e != nil {
			t.Fatal(e)
		}
		created = append(created, v)
	}
	first, e := service.ListPage(ctx, org, 2, "")
	if e != nil {
		t.Fatal(e)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != created[2].ID || first.Items[1].ID != created[1].ID {
		t.Fatalf("unexpected first commercial page: %#v", first)
	}
	second, e := service.ListPage(ctx, org, 2, first.NextCursor)
	if e != nil {
		t.Fatal(e)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != created[0].ID {
		t.Fatalf("unexpected second commercial page: %#v", second)
	}
}

func TestPostgreSQLConsentReviewPaginationContinuesWithoutSkipping(t *testing.T) {
	db, ctx, _ := openCompliancePaginationDB(t)
	actor, org := seedComplianceActorOrganisation(t, ctx, db)
	repo := &ConsentRepository{DB: db}
	service := consent.NewService(repo)
	base := time.Date(2099, 5, 3, 12, 0, 0, 0, time.UTC)
	created := make([]consent.Review, 0, 3)
	for i := 0; i < 3; i++ {
		v, e := consent.NewReview(consent.CreateInput{OrganisationID: org, Name: "Pagination Review", Channel: "WHATSAPP", CreatedBy: actor}, base.Add(time.Duration(i)*time.Minute))
		if e != nil {
			t.Fatal(e)
		}
		if e = repo.Create(ctx, v); e != nil {
			t.Fatal(e)
		}
		created = append(created, v)
	}
	first, e := service.ListPage(ctx, org, 2, "")
	if e != nil {
		t.Fatal(e)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != created[2].ID || first.Items[1].ID != created[1].ID {
		t.Fatalf("unexpected first consent review page: %#v", first)
	}
	second, e := service.ListPage(ctx, org, 2, first.NextCursor)
	if e != nil {
		t.Fatal(e)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != created[0].ID {
		t.Fatalf("unexpected second consent review page: %#v", second)
	}
}

func TestPostgreSQLOptOutPolicyPaginationContinuesWithoutSkipping(t *testing.T) {
	db, ctx, _ := openCompliancePaginationDB(t)
	actor, _ := seedComplianceActorOrganisation(t, ctx, db)
	admin := &consent.OptOutPolicyAdministration{Store: &OptOutPolicyStore{DB: db}}
	var base time.Time
	if e := db.QueryRowContext(ctx, `SELECT coalesce(max(created_at), '2099-05-04 12:00:00+00'::timestamptz) + interval '1 minute' FROM opt_out_policies`).Scan(&base); e != nil {
		t.Fatal(e)
	}
	base = base.UTC()
	created := make([]consent.GovernedOptOutPolicy, 0, 3)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		admin.Clock = func() time.Time { return at }
		v, e := admin.CreateDraft(ctx, []string{"STOP"}, time.Time{}, actor, "pagination optout")
		if e != nil {
			t.Fatal(e)
		}
		created = append(created, v)
	}
	first, e := admin.ListPage(ctx, 2, "")
	if e != nil {
		t.Fatal(e)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != created[2].ID || first.Items[1].ID != created[1].ID {
		t.Fatalf("unexpected first optout page: %#v", first)
	}
	second, e := admin.ListPage(ctx, 2, first.NextCursor)
	if e != nil {
		t.Fatal(e)
	}
	if len(second.Items) == 0 || second.Items[0].ID != created[0].ID {
		t.Fatalf("cursor skipped the oldest policy created by this test: %#v", second)
	}
}
