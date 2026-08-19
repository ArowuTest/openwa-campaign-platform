package operations

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLReportingPrivacyPaginationContinuesWithoutSkipping(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var actor, orgID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actor, &orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Reporting Pagination','DISABLED',false)`, actor, "reporting-pagination-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Reporting pagination "+orgID); err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLReportingPrivacyStore{DB: db}
	admin := &ReportingPrivacyAdministration{Store: store}
	base := time.Date(2099, 1, 1, 13, 0, 0, 0, time.UTC)
	created := make([]ReportingPrivacyPolicy, 0, 3)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		admin.Clock = func() time.Time { return at }
		v, createErr := admin.Create(ctx, ReportingPrivacyPolicy{OrganisationID: orgID, MinimumCohortSize: 10}, actor, "pagination integration", "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		created = append(created, v)
	}
	first, err := admin.ListPage(ctx, orgID, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != created[2].ID || first.Items[1].ID != created[1].ID {
		t.Fatalf("unexpected first reporting page: %#v", first)
	}
	second, err := admin.ListPage(ctx, orgID, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != created[0].ID {
		t.Fatalf("unexpected second reporting page: %#v", second)
	}
}
