package operations

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

var exportWorkerUpdateColumns = map[string]bool{
	"status": true, "lease_owner": true, "lease_expires_at": true,
	"attempt_count": true, "updated_at": true, "version": true,
	"object_key": true, "content_type": true, "sha256": true,
	"size_bytes": true, "generated_at": true, "expires_at": true,
	"failure_code": true, "failure_detail": true,
}

// Exercise the repository's real lock/update statements as the deployment
// privilege role. Narrowing grants must not strand claims, failure or expiry.
func TestPostgreSQLExportWorkerServiceRoleLifecycle(t *testing.T) {
	f := newExportWorkerRoleFixture(t)
	// Keep unrelated export fixtures out of the SKIP LOCKED worker queues.
	blocker, err := f.owner.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Rollback() })
	rows, err := blocker.QueryContext(f.ctx, "SELECT id FROM export_requests FOR UPDATE")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	id := f.approvedExport(t)
	repo := &PostgreSQLRepository{DB: f.worker}
	job, err := repo.ClaimExport(f.ctx, "role-worker", f.now, time.Minute)
	if err != nil || job.ID != id || job.Status != ExportProcessing {
		t.Fatalf("claim under export worker role: job=%+v err=%v", job, err)
	}
	if err := repo.CompleteExport(f.ctx, id, "role-fixture/export", "application/json", strings.Repeat("b", 64), 128, f.now, f.now.Add(time.Hour)); err != nil {
		t.Fatalf("complete under export worker role: %v", err)
	}
	late := f.now.Add(2 * time.Hour)
	expiring, err := repo.ClaimExpiringExports(f.ctx, late, 10)
	if err != nil || len(expiring) != 1 || expiring[0].ID != id {
		t.Fatalf("claim expiration: jobs=%+v err=%v", expiring, err)
	}
	if err := repo.FailExpiration(f.ctx, id, "temporary storage failure", late); err != nil {
		t.Fatalf("fail expiration: %v", err)
	}
	expiring, err = repo.ClaimExpiringExports(f.ctx, late, 10)
	if err != nil || len(expiring) != 1 || expiring[0].ID != id {
		t.Fatalf("retry expiration: jobs=%+v err=%v", expiring, err)
	}
	if err := repo.CompleteExpiration(f.ctx, id, late); err != nil {
		t.Fatalf("complete expiration: %v", err)
	}
	failedID := f.approvedExport(t)
	job, err = repo.ClaimExport(f.ctx, "role-worker", f.now, time.Minute)
	if err != nil || job.ID != failedID {
		t.Fatalf("claim failure fixture: job=%+v err=%v", job, err)
	}
	if err := repo.FailExport(f.ctx, failedID, "RENDER_FAILED", "fixture render failed", f.now); err != nil {
		t.Fatalf("fail export: %v", err)
	}
	for _, exportID := range []string{id, failedID} {
		var unchanged bool
		if err := f.owner.QueryRowContext(f.ctx, `SELECT approved_by=$2::uuid
AND criteria='{"fixture":"locked"}'::jsonb AND frozen_payload='{"evidence":"locked"}'::jsonb
AND audit_head_sequence=7 AND audit_head_hash=repeat('a',64)
AND authentication_event_as_of=$3 AND authentication_event_head_id=$4::uuid
AND as_of=$3 AND watermark_text='role fixture watermark'
FROM export_requests WHERE id=$1::uuid`, exportID, f.checker, f.now, f.authHead).Scan(&unchanged); err != nil || !unchanged {
			t.Fatalf("worker changed governance/frozen evidence: unchanged=%v err=%v", unchanged, err)
		}
	}
}

// Catalog assertions prove every column, including future schema additions,
// while actual denied statements prove PostgreSQL enforces the boundary.
func TestPostgreSQLExportWorkerServiceRoleLeastPrivilege(t *testing.T) {
	f := newExportWorkerRoleFixture(t)
	assertExportWorkerPrivilegeMatrix(t, f.ctx, f.worker)
	id := f.approvedExport(t)
	for _, query := range []string{
		"UPDATE export_requests SET criteria=criteria WHERE id=$1::uuid",
		"UPDATE export_requests SET frozen_payload=frozen_payload WHERE id=$1::uuid",
		"UPDATE export_requests SET approved_by=approved_by WHERE id=$1::uuid",
		"UPDATE export_requests SET audit_head_sequence=audit_head_sequence WHERE id=$1::uuid",
		"UPDATE export_requests SET authentication_event_as_of=authentication_event_as_of WHERE id=$1::uuid",
		"UPDATE export_requests SET authentication_event_head_id=authentication_event_head_id WHERE id=$1::uuid",
		"UPDATE export_requests SET download_count=download_count WHERE id=$1::uuid",
		"UPDATE export_requests SET revoked_by=revoked_by WHERE id=$1::uuid",
		"INSERT INTO export_requests SELECT * FROM export_requests WHERE id=$1::uuid",
		"DELETE FROM export_requests WHERE id=$1::uuid",
	} {
		_, err := f.worker.ExecContext(f.ctx, query, id)
		requireExportPrivilegeDenied(t, query, err)
	}
	for _, query := range []string{
		"SELECT * FROM authentication_events LIMIT 1",
		"SELECT id,user_id,event_type,correlation_id,occurred_at FROM authentication_events LIMIT 1",
		"SELECT id FROM authentication_events LIMIT 1",
		"SELECT metadata FROM authentication_events LIMIT 1",
		"SELECT * FROM export_download_grants LIMIT 1",
		"UPDATE export_download_grants SET revoked_at=revoked_at WHERE false",
	} {
		rows, err := f.worker.QueryContext(f.ctx, query)
		if rows != nil {
			_ = rows.Close()
		}
		requireExportPrivilegeDenied(t, query, err)
	}
	assertExportWorkerAuthenticationSourceDenied(t, f.ctx, f.worker)
}

// Applying just the upgrade migration twice must remove old table grants and
// unexpected column grants, without requiring a separate role bootstrap run.
func TestPostgreSQLExportWorkerPrivilegeUpgrade(t *testing.T) {
	f := newExportWorkerRoleFixture(t)
	tx, err := f.owner.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(f.ctx, "GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE public.export_requests TO campaign_export_worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(f.ctx, "GRANT UPDATE (criteria,frozen_payload,authentication_event_head_id) ON TABLE public.export_requests TO campaign_export_worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(f.ctx, "GRANT INSERT (criteria,frozen_payload), REFERENCES (id) ON TABLE public.export_requests TO campaign_export_worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(f.ctx, "GRANT SELECT (id,user_id,event_type,correlation_id,occurred_at) ON TABLE public.authentication_events TO campaign_export_worker"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join("..", "..", "database", "migrations", "0102_export_worker_column_privileges.sql"))
	if err != nil {
		t.Fatal(err)
	}
	statements, err := splitExportWorkerPrivilegeMigration(string(content))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		for index, statement := range statements {
			if _, err := tx.ExecContext(f.ctx, statement); err != nil {
				t.Fatalf("privilege upgrade pass %d statement %d: %v", i+1, index+1, err)
			}
		}
	}
	if _, err := tx.ExecContext(f.ctx, "SET LOCAL ROLE campaign_export_worker"); err != nil {
		t.Fatal(err)
	}
	assertExportWorkerPrivilegeMatrix(t, f.ctx, tx)
	assertExportWorkerAuthenticationSourceDenied(t, f.ctx, tx)
	for _, query := range []string{
		"UPDATE export_requests SET criteria=criteria WHERE false",
		"UPDATE export_requests SET frozen_payload=frozen_payload WHERE false",
		"UPDATE export_requests SET authentication_event_head_id=authentication_event_head_id WHERE false",
		"INSERT INTO export_requests SELECT * FROM export_requests WHERE false",
		"INSERT INTO export_requests(criteria,frozen_payload) SELECT criteria,frozen_payload FROM export_requests WHERE false",
		"DELETE FROM export_requests WHERE false",
		"SELECT id,user_id,event_type,correlation_id,occurred_at FROM authentication_events LIMIT 1",
	} {
		if _, err := tx.ExecContext(f.ctx, "SAVEPOINT denied_authority"); err != nil {
			t.Fatal(err)
		}
		_, err := tx.ExecContext(f.ctx, query)
		requireExportPrivilegeDenied(t, query, err)
		if _, err := tx.ExecContext(f.ctx, "ROLLBACK TO SAVEPOINT denied_authority"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.ExecContext(f.ctx, "UPDATE export_requests SET status=status,version=version WHERE false"); err != nil {
		t.Fatalf("upgrade removed permitted lifecycle authority: %v", err)
	}
}

func TestPostgreSQLExportWorkerStatusTransitionGuard(t *testing.T) {
	f := newExportWorkerRoleFixture(t)
	id := f.approvedExport(t)
	tx, err := f.owner.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Production credentials LOGIN and inherit the NOLOGIN privilege role,
	// so test that effective identity as well as SET ROLE to the stable role.
	login := "export_role_fixture_" + strings.ReplaceAll(f.authHead, "-", "")
	if _, err := tx.ExecContext(f.ctx, "CREATE ROLE "+login+" LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(f.ctx, "GRANT campaign_export_worker TO "+login); err != nil {
		t.Fatal(err)
	}
	// TEMP privileges normally come from PUBLIC. An attacker-controlled
	// search path must not let a temp pg_roles view impersonate a superuser.
	if _, err := tx.ExecContext(f.ctx, "CREATE TEMP TABLE pg_roles (rolname name,rolsuper boolean) ON COMMIT DROP"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(f.ctx, "GRANT SELECT ON TABLE pg_temp.pg_roles TO campaign_export_worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(f.ctx, "INSERT INTO pg_temp.pg_roles(rolname,rolsuper) VALUES($1,true)", login); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"campaign_export_worker", login} {
		for _, transition := range []struct{ from, to string }{
			{"PENDING_APPROVAL", "APPROVED"},
			{"PENDING_APPROVAL", "PROCESSING"},
			{"PENDING_APPROVAL", "READY"},
			{"APPROVED", "READY"},
			{"PROCESSING", "APPROVED"},
			{"READY", "APPROVED"},
			{"EXPIRING", "APPROVED"},
		} {
			if _, err := tx.ExecContext(f.ctx, "RESET ROLE"); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(f.ctx, `UPDATE export_requests SET status=$2,object_key='guard/fixture',content_type='application/json',sha256=repeat('c',64),size_bytes=128,generated_at=$3,expires_at=$4 WHERE id=$1::uuid`, id, transition.from, f.now, f.now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(f.ctx, "SET LOCAL ROLE "+role); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(f.ctx, "SAVEPOINT denied_transition"); err != nil {
				t.Fatal(err)
			}
			_, err := tx.ExecContext(f.ctx, "UPDATE export_requests SET status=$2 WHERE id=$1::uuid", id, transition.to)
			requireExportPrivilegeDenied(t, role+" "+transition.from+" -> "+transition.to, err)
			if _, err := tx.ExecContext(f.ctx, "ROLLBACK TO SAVEPOINT denied_transition"); err != nil {
				t.Fatal(err)
			}
		}
		for _, transition := range []struct{ from, to string }{
			{"APPROVED", "PROCESSING"},
			{"PROCESSING", "PROCESSING"},
			{"PROCESSING", "READY"},
			{"PROCESSING", "FAILED"},
			{"READY", "EXPIRING"},
			{"EXPIRING", "READY"},
			{"EXPIRING", "EXPIRED"},
		} {
			if _, err := tx.ExecContext(f.ctx, "RESET ROLE"); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(f.ctx, `UPDATE export_requests SET status=$2,approved_by=$3::uuid,object_key='guard/fixture',content_type='application/json',sha256=repeat('c',64),size_bytes=128,generated_at=$4,expires_at=$5 WHERE id=$1::uuid`, id, transition.from, f.checker, f.now, f.now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(f.ctx, "SET LOCAL ROLE "+role); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(f.ctx, "UPDATE export_requests SET status=$2 WHERE id=$1::uuid", id, transition.to); err != nil {
				t.Fatalf("guard denied lifecycle %s %s -> %s: %v", role, transition.from, transition.to, err)
			}
		}
		// The existing API contract requires a distinct checker. Do not allow
		// a legacy APPROVED row missing that evidence to be claimed/published.
		if _, err := tx.ExecContext(f.ctx, "RESET ROLE"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(f.ctx, "UPDATE export_requests SET status='APPROVED',approved_by=NULL WHERE id=$1::uuid", id); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(f.ctx, "SET LOCAL ROLE "+role); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(f.ctx, "SAVEPOINT missing_checker"); err != nil {
			t.Fatal(err)
		}
		_, err := tx.ExecContext(f.ctx, "UPDATE export_requests SET status='PROCESSING' WHERE id=$1::uuid", id)
		requireExportPrivilegeDenied(t, role+" claim without independent checker", err)
		if _, err := tx.ExecContext(f.ctx, "ROLLBACK TO SAVEPOINT missing_checker"); err != nil {
			t.Fatal(err)
		}
		// Missing approval does not prevent deletion of legacy READY artifacts.
		if _, err := tx.ExecContext(f.ctx, "RESET ROLE"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(f.ctx, `UPDATE export_requests SET status='READY',object_key='legacy/fixture',approved_by=NULL WHERE id=$1::uuid`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(f.ctx, "SET LOCAL ROLE "+role); err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{"EXPIRING", "EXPIRED"} {
			if _, err := tx.ExecContext(f.ctx, "UPDATE export_requests SET status=$2 WHERE id=$1::uuid", id, status); err != nil {
				t.Fatalf("guard denied safe legacy artifact cleanup: %v", err)
			}
		}
	}
	if _, err := tx.ExecContext(f.ctx, "RESET ROLE"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(f.ctx, "SET LOCAL ROLE campaign_control_api"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(f.ctx, "UPDATE export_requests SET status='PENDING_APPROVAL' WHERE id=$1::uuid", id); err != nil {
		t.Fatalf("guard denied control API governance authority: %v", err)
	}
	if _, err := tx.ExecContext(f.ctx, "UPDATE export_requests SET status='APPROVED',approved_by=$2::uuid WHERE id=$1::uuid", id, f.checker); err != nil {
		t.Fatalf("guard denied control API approval authority: %v", err)
	}
}

type exportPrivilegeQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func assertExportWorkerPrivilegeMatrix(t *testing.T, ctx context.Context, db exportPrivilegeQueryer) {
	t.Helper()
	var read, update, insert, delete, truncate bool
	if err := db.QueryRowContext(ctx, `SELECT has_table_privilege(current_user,'public.export_requests','SELECT'),
has_table_privilege(current_user,'public.export_requests','UPDATE'),
has_table_privilege(current_user,'public.export_requests','INSERT'),
has_table_privilege(current_user,'public.export_requests','DELETE'),
has_table_privilege(current_user,'public.export_requests','TRUNCATE')`).Scan(&read, &update, &insert, &delete, &truncate); err != nil {
		t.Fatal(err)
	}
	if !read || update || insert || delete || truncate {
		t.Fatalf("export worker table authority: select=%v update=%v insert=%v delete=%v truncate=%v", read, update, insert, delete, truncate)
	}
	rows, err := db.QueryContext(ctx, `SELECT attname,has_column_privilege(current_user,'public.export_requests',attname,'UPDATE'),
has_column_privilege(current_user,'public.export_requests',attname,'INSERT'),
has_column_privilege(current_user,'public.export_requests',attname,'REFERENCES')
FROM pg_attribute WHERE attrelid='public.export_requests'::regclass AND attnum>0 AND NOT attisdropped ORDER BY attnum`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var column string
		var permitted, insert, references bool
		if err := rows.Scan(&column, &permitted, &insert, &references); err != nil {
			t.Fatal(err)
		}
		if permitted != exportWorkerUpdateColumns[column] {
			t.Errorf("export_requests.%s UPDATE=%v want=%v", column, permitted, exportWorkerUpdateColumns[column])
		}
		if insert || references {
			t.Errorf("export_requests.%s retained INSERT=%v REFERENCES=%v", column, insert, references)
		}
		seen++
	}
	if err := rows.Err(); err != nil || seen <= len(exportWorkerUpdateColumns) {
		t.Fatalf("export worker column proof incomplete: seen=%d err=%v", seen, err)
	}
}

func assertExportWorkerAuthenticationSourceDenied(t *testing.T, ctx context.Context, db exportPrivilegeQueryer) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT attname,has_column_privilege(current_user,'public.authentication_events',attname,'SELECT')
FROM pg_catalog.pg_attribute WHERE attrelid='public.authentication_events'::regclass AND attnum>0 AND NOT attisdropped ORDER BY attnum`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var column string
		var permitted bool
		if err := rows.Scan(&column, &permitted); err != nil {
			t.Fatal(err)
		}
		if permitted {
			t.Errorf("export worker retained source read on authentication_events.%s", column)
		}
		seen++
	}
	if err := rows.Err(); err != nil || seen == 0 {
		t.Fatalf("authentication source denial proof incomplete: seen=%d err=%v", seen, err)
	}
}

func requireExportPrivilegeDenied(t *testing.T, query string, err error) {
	t.Helper()
	var state interface{ SQLState() string }
	if !errors.As(err, &state) || state.SQLState() != "42501" {
		t.Errorf("forbidden statement must fail with insufficient_privilege: query=%s err=%v", query, err)
	}
}

type exportWorkerRoleFixture struct {
	ctx                      context.Context
	owner, worker            *sql.DB
	now                      time.Time
	maker, checker, authHead string
}

func newExportWorkerRoleFixture(t *testing.T) *exportWorkerRoleFixture {
	t.Helper()
	dsn := os.Getenv("POSTGRES_EXPORT_ROLE_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_EXPORT_ROLE_DATABASE_URL is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		t.Fatal("POSTGRES_EXPORT_ROLE_DATABASE_URL must be an isolated test database URL")
	}
	owner, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	query := u.Query()
	query.Set("options", strings.TrimSpace(query.Get("options")+" -c role=campaign_export_worker"))
	u.RawQuery = query.Encode()
	worker, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	f := &exportWorkerRoleFixture{ctx: ctx, owner: owner, worker: worker, now: time.Now().UTC().Truncate(time.Microsecond)}
	var role string
	if err := worker.QueryRowContext(ctx, "SELECT current_user").Scan(&role); err != nil || role != "campaign_export_worker" {
		t.Fatalf("worker session role=%q err=%v", role, err)
	}
	if err := owner.QueryRowContext(ctx, "SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text").Scan(&f.maker, &f.checker, &f.authHead); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := owner.ExecContext(cleanupCtx, "DELETE FROM export_requests WHERE requested_by=$1::uuid", f.maker); err != nil {
			t.Errorf("clean export fixtures: %v", err)
		}
		if _, err := owner.ExecContext(cleanupCtx, "DELETE FROM internal_users WHERE id=$1::uuid OR id=$2::uuid", f.maker, f.checker); err != nil {
			t.Errorf("clean export actors: %v", err)
		}
	})
	for _, actor := range []string{f.maker, f.checker} {
		if _, err := owner.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Export role fixture','DISABLED',false)`, actor, "export-role-"+actor+"@internal.invalid"); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *exportWorkerRoleFixture) approvedExport(t *testing.T) string {
	t.Helper()
	var id string
	if err := f.owner.QueryRowContext(f.ctx, `INSERT INTO export_requests(kind,format,status,requested_by,approved_by,reason,criteria,frozen_payload,as_of,audit_head_sequence,audit_head_hash,authentication_event_as_of,authentication_event_head_id,watermark_text,created_at,updated_at)
VALUES('AUDIT_LOG','JSON','APPROVED',$1::uuid,$2::uuid,'role fixture approved export','{"fixture":"locked"}'::jsonb,'{"evidence":"locked"}'::jsonb,$3,7,repeat('a',64),$3,$4::uuid,'role fixture watermark',$3,$3) RETURNING id::text`, f.maker, f.checker, f.now, f.authHead).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestExportWorkerPrivilegeMigrationUsesSingleStatements(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "database", "migrations", "0102_export_worker_column_privileges.sql"))
	if err != nil {
		t.Fatal(err)
	}
	statements, err := splitExportWorkerPrivilegeMigration(string(content))
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 4 {
		t.Fatalf("native driver needs four single top-level commands; got %d", len(statements))
	}
	for i, prefix := range []string{
		"-- Upgrade-only migration runners",
		"CREATE OR REPLACE FUNCTION guard_export_worker_status_transition()",
		"DROP TRIGGER IF EXISTS export_requests_worker_status_transition_guard",
		"CREATE TRIGGER export_requests_worker_status_transition_guard",
	} {
		if !strings.HasPrefix(statements[i], prefix) {
			t.Errorf("statement %d lost its top-level boundary: %s", i+1, statements[i])
		}
	}
	if !strings.Contains(statements[0], "DO $$") || !strings.Contains(statements[0], "END $$;") ||
		!strings.HasSuffix(statements[1], "END $$;") {
		t.Fatal("dollar-quoted DO/function bodies must remain complete")
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(content)), "BEGIN;"), "COMMIT;"))
	if strings.Join(strings.Fields(strings.Join(statements, "\n")), " ") != strings.Join(strings.Fields(body), " ") {
		t.Fatal("migration splitting discarded or changed SQL")
	}
}

// 0102 is a frozen migration fixture with four top-level commands. Split at
// those explicit boundaries instead of semicolons inside dollar-quoted PL/pgSQL.
// The native driver prepares every ExecContext, so it cannot accept a SQL batch.
func splitExportWorkerPrivilegeMigration(content string) ([]string, error) {
	body := strings.TrimSpace(content)
	if !strings.HasPrefix(body, "BEGIN;") || !strings.HasSuffix(body, "COMMIT;") {
		return nil, errors.New("export privilege migration transaction envelope changed")
	}
	body = strings.TrimSpace(strings.TrimPrefix(body, "BEGIN;"))
	body = strings.TrimSpace(strings.TrimSuffix(body, "COMMIT;"))
	var statements []string
	offset := 0
	for _, boundary := range []string{
		"\nCREATE OR REPLACE FUNCTION guard_export_worker_status_transition()",
		"\nDROP TRIGGER IF EXISTS export_requests_worker_status_transition_guard",
		"\nCREATE TRIGGER export_requests_worker_status_transition_guard",
	} {
		index := strings.Index(body[offset:], boundary)
		if index < 0 {
			return nil, errors.New("export privilege migration statement boundary changed")
		}
		index += offset
		statements = append(statements, strings.TrimSpace(body[offset:index]))
		offset = index
	}
	statements = append(statements, strings.TrimSpace(body[offset:]))
	return statements, nil
}
