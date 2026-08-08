package inbound

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLInboundRetentionPolicyPaginationContinuesWithoutSkipping(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if e = db.PingContext(ctx); e != nil {
		t.Fatal(e)
	}
	var actor string
	if e = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actor); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Inbound Pagination','DISABLED',false)`, actor, "inbound-pagination-"+actor+"@internal.invalid"); e != nil {
		t.Fatal(e)
	}
	admin := &RetentionPolicyAdministration{Store: &PostgreSQLRetentionPolicyStore{DB: db}}
	base := time.Date(2099, 5, 5, 12, 0, 0, 0, time.UTC)
	created := make([]RetentionPolicy, 0, 3)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		admin.Clock = func() time.Time { return at }
		v, ce := admin.CreateDraft(ctx, 30, time.Time{}, actor, "pagination retention")
		if ce != nil {
			t.Fatal(ce)
		}
		created = append(created, v)
	}
	first, e := admin.ListPage(ctx, 2, "")
	if e != nil {
		t.Fatal(e)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != created[2].ID || first.Items[1].ID != created[1].ID {
		t.Fatalf("unexpected first inbound retention page: %#v", first)
	}
	second, e := admin.ListPage(ctx, 2, first.NextCursor)
	if e != nil {
		t.Fatal(e)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != created[0].ID {
		t.Fatalf("unexpected second inbound retention page: %#v", second)
	}
}
