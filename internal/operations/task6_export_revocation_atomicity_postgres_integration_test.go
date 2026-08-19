package operations

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/audit"
	_ "campaign-platform/internal/persistence/database"
)

func TestTask6ExportRevocationIsAtomicWithGrantRevocationAndAudit(t *testing.T) {
	dsn := os.Getenv("POSTGRES_OPERATIONS_AUDIT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_OPERATIONS_AUDIT_DATABASE_URL is not set")
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
	var maker, actor, exportID, grantID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&maker, &actor, &exportID, &grantID); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ id, email string }{{maker, "task6-revoke-maker-" + maker + "@internal.invalid"}, {actor, "task6-revoke-actor-" + actor + "@internal.invalid"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task6 revoke actor','DISABLED',false)`, v.id, v.email); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `INSERT INTO export_requests(id,kind,format,status,requested_by,reason,expires_at,object_key,content_type,sha256,size_bytes,generated_at,created_at,updated_at) VALUES($1::uuid,'AUDIT_LOG','JSON','READY',$2::uuid,'task 6 governed export',$3,'task6/revoke','application/json',repeat('c',64),512,$4,$4,$4)`, exportID, maker, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO export_download_grants(id,export_id,actor_id,token_hash,request_id,expires_at,created_at) VALUES($1::uuid,$2::uuid,$3::uuid,encode(digest($4,'sha256'),'hex'),$4,$5,$6)`, grantID, exportID, actor, "task6-revoke-grant-"+grantID, now.Add(5*time.Minute), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE OR REPLACE FUNCTION task6_fail_grant_revoke() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'task6 injected grant revoke failure'; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER task6_fail_grant_revoke BEFORE UPDATE ON export_download_grants FOR EACH ROW EXECUTE FUNCTION task6_fail_grant_revoke()`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS task6_fail_grant_revoke ON export_download_grants`)
		_, _ = db.ExecContext(context.Background(), `DROP FUNCTION IF EXISTS task6_fail_grant_revoke()`)
	}()
	requestID := "task6-export-revoke-" + exportID
	svc := &Service{Repo: &PostgreSQLRepository{DB: db}, Audit: audit.NewRecorder(&audit.PostgreSQLRepository{DB: db}), Clock: func() time.Time { return now.Add(time.Second) }}
	_, callErr := svc.RevokeExport(ctx, exportID, 1, "security revocation required", actor, requestID)
	if callErr == nil {
		t.Fatal("injected grant revocation failure was not surfaced")
	}
	var status string
	var activeGrant, durable int
	if err := db.QueryRowContext(ctx, `SELECT status FROM export_requests WHERE id=$1::uuid`, exportID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM export_download_grants WHERE id=$1::uuid AND revoked_at IS NULL`, grantID).Scan(&activeGrant); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM transactional_outbox WHERE aggregate_type='AUDIT' AND payload->>'correlationId'=$1`, requestID).Scan(&durable); err != nil {
		t.Fatal(err)
	}
	if status == string(ExportRevoked) || durable != 0 || activeGrant != 1 {
		t.Fatalf("partial export revocation committed: status=%s activeGrant=%d durableAudit=%d err=%v", status, activeGrant, durable, callErr)
	}
}
