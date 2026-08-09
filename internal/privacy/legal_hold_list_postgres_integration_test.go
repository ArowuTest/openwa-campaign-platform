package privacy

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func TestPostgreSQLLegalHoldPaginationContinuesWithoutSkipping(t *testing.T) {
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
	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	lookup := protector.LookupHMAC("+2348012345678")
	if _, err := db.ExecContext(ctx, `DELETE FROM privacy_legal_holds WHERE subject_lookup_hmac=$1`, lookup); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(context.Background(), `DELETE FROM privacy_legal_holds WHERE subject_lookup_hmac=$1`, lookup)
	var actor string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Legal Hold Pagination','DISABLED',false)`, actor, "hold-pagination-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2099, 4, 1, 12, 0, 0, 0, time.UTC)
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO privacy_legal_holds(id,subject_lookup_hmac,scope,status,reason,created_by,created_at,version) VALUES($1::uuid,$2,'CONTACT','DRAFT','pagination evidence hold',$3::uuid,$4,1)`, ids[i], lookup, actor, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{Repository: &PostgreSQLRepository{DB: db}, Protector: protector}
	first, err := service.ListLegalHoldsPage(ctx, "+2348012345678", false, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != ids[2] || first.Items[1].ID != ids[1] {
		t.Fatalf("unexpected first legal-hold page: %#v", first)
	}
	second, err := service.ListLegalHoldsPage(ctx, "+2348012345678", false, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != ids[0] {
		t.Fatalf("unexpected second legal-hold page: %#v", second)
	}
}
