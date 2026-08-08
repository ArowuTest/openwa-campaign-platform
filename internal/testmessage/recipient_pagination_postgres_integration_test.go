package testmessage

import (
	_ "campaign-platform/internal/persistence/database"
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestPostgreSQLRecipientPaginationContinuesWithoutSkipping(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Test Recipient Pagination','DISABLED',false)`, actor, "test-recipient-pagination-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2099, 5, 1, 12, 0, 0, 0, time.UTC)
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		at := base.Add(time.Duration(i) * time.Minute)
		encrypted := []byte(ids[i])
		lookup := []byte("lookup-" + ids[i])
		if _, err := db.ExecContext(ctx, `INSERT INTO approved_test_recipients(id,label,msisdn_encrypted,msisdn_lookup_hash,masked_msisdn,status,created_by,reason,version,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,'+234 ***','ACTIVE',$5::uuid,'pagination integration',1,$6,$6)`, ids[i], "recipient-"+ids[i], encrypted, lookup, actor, at); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{Repository: &PostgreSQLRepository{DB: db}}
	first, err := service.ListRecipientsPage(ctx, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != ids[2] || first.Items[1].ID != ids[1] {
		t.Fatalf("unexpected first test-recipient page: %#v", first)
	}
	second, err := service.ListRecipientsPage(ctx, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != ids[0] {
		t.Fatalf("unexpected second test-recipient page: %#v", second)
	}
}
