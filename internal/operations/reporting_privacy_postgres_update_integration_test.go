package operations

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLReportingPrivacySupportsCreateAndSubmit(t *testing.T) {
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
	var actor string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Reporting Update Regression','DISABLED',false)`, actor, "reporting-update-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	admin := &ReportingPrivacyAdministration{Store: &PostgreSQLReportingPrivacyStore{DB: db}}
	admin.Clock = func() time.Time { return time.Date(2099, 3, 1, 12, 0, 0, 0, time.UTC) }
	created, err := admin.Create(ctx, ReportingPrivacyPolicy{MinimumCohortSize: 10}, actor, "reporting update regression", "")
	if err != nil {
		t.Fatal(err)
	}
	admin.Clock = func() time.Time { return time.Date(2099, 3, 1, 12, 1, 0, 0, time.UTC) }
	submitted, err := admin.Submit(ctx, created.ID, created.Version, actor, "reporting submit regression", "")
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Status != ReportingPrivacyPending {
		t.Fatalf("unexpected submitted policy: %#v", submitted)
	}
}
