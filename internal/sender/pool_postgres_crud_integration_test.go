package sender

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLSenderPoolCreateAndUpdatePreservesOrganisationProjection(t *testing.T) {
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
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var actor string
	if err = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Sender Pool Regression','DISABLED',false)`, actor, "sender-pool-regression-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLGovernanceStore{DB: db}
	gov := &GovernanceService{Store: store}
	created, err := gov.CreatePool(ctx, Pool{Name: "sender-pool-regression-" + actor, Status: "ACTIVE", MaxMessagesPerMinute: 10, DailyCapacity: 1000}, actor, "create regression")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := gov.UpdatePool(ctx, created.ID, created.Version, Pool{Name: created.Name, Status: "PAUSED", MaxMessagesPerMinute: 20, DailyCapacity: 2000}, actor, "update regression")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "PAUSED" || updated.MaxMessagesPerMinute != 20 {
		t.Fatalf("unexpected sender pool update: %#v", updated)
	}
}
