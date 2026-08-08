package operations

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLAlertPolicyPaginationContinuesWithoutSkipping(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Alert Policy Pagination','DISABLED',false)`, actor, "alert-policy-pagination-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLAlertStore{DB: db}
	admin := &AlertAdministration{Store: store}
	base := time.Now().UTC().AddDate(1000, 0, 0).Truncate(time.Microsecond)
	created := make([]AlertPolicy, 0, 3)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		admin.Clock = func() time.Time { return at }
		value, createErr := admin.CreatePolicy(ctx, AlertPolicy{Name: "Pagination Alert", Metric: AlertQueueDepth, Comparison: ComparisonGT, Threshold: 1, Severity: SeverityWarning, ConsecutiveEvaluations: 1, CooldownSeconds: 60, EscalationSteps: []EscalationStep{}}, actor, "pagination integration")
		if createErr != nil {
			t.Fatal(createErr)
		}
		submitted, submitErr := admin.SubmitPolicy(ctx, value.ID, value.Version, actor, "pagination integration submit")
		if submitErr != nil {
			t.Fatal(submitErr)
		}
		created = append(created, submitted)
	}
	first, err := admin.ListPoliciesPage(ctx, AlertPolicyPending, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != created[2].ID || first.Items[1].ID != created[1].ID {
		t.Fatalf("unexpected first alert-policy page: %#v", first)
	}
	second, err := admin.ListPoliciesPage(ctx, AlertPolicyPending, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) == 0 || second.Items[0].ID != created[0].ID {
		t.Fatalf("alert-policy continuation skipped fixture: %#v", second)
	}
}
