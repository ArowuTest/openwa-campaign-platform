package operations

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLReportingPrivacyActivationUsesGovernedTransaction(t *testing.T) {
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
	var creatorID, approverID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&creatorID, &approverID); err != nil {
		t.Fatal(err)
	}
	for _, user := range []struct{ id, email string }{
		{creatorID, "reporting-creator-" + creatorID + "@internal.invalid"},
		{approverID, "reporting-approver-" + approverID + "@internal.invalid"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 reporting privacy','DISABLED',false)`, user.id, user.email); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2099, 3, 2, 12, 0, 0, 0, time.UTC)
	admin := &ReportingPrivacyAdministration{
		Store: &PostgreSQLReportingPrivacyStore{DB: db},
		Clock: func() time.Time { return now },
	}
	created, err := admin.Create(ctx, ReportingPrivacyPolicy{MinimumCohortSize: 10}, creatorID, "task six reporting privacy draft", "")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	submitted, err := admin.Submit(ctx, created.ID, created.Version, creatorID, "task six reporting privacy submit", "")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	activated, err := admin.Decide(ctx, submitted.ID, submitted.Version, true, approverID, "task six reporting privacy approval", "")
	if err != nil {
		t.Fatal(err)
	}
	if activated.Status != ReportingPrivacyActive || activated.ApprovedBy != approverID {
		t.Fatalf("unexpected activated policy: %#v", activated)
	}
	resolved, err := admin.Resolve(ctx, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != activated.ID {
		t.Fatalf("resolved policy=%s want=%s", resolved.ID, activated.ID)
	}
	events, err := admin.Events(ctx, activated.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].EventType != "ACTIVATED" {
		t.Fatalf("unexpected reporting privacy events: %#v", events)
	}
}
