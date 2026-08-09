package retention

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLPrivacyRetentionSchedulingExcludesActiveLegalHold(t *testing.T) {
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

	ids := make([]string, 6)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, checkerID, policyID, heldCaseID, freeCaseID, holdID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	for _, actor := range []struct{ id, name string }{{actorID, "Retention Actor"}, {checkerID, "Retention Checker"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,$3,'DISABLED',false)`, actor.id, "retention-privacy-"+actor.id+"@internal.invalid", actor.name); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM retention_jobs WHERE retention_policy_id=$1::uuid`, policyID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM retention_policies WHERE id=$1::uuid`, policyID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM privacy_legal_holds WHERE id=$1::uuid`, holdID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM privacy_cases WHERE id IN ($1::uuid,$2::uuid)`, heldCaseID, freeCaseID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id IN ($1::uuid,$2::uuid)`, actorID, checkerID)
	}()

	now := time.Now().UTC().Truncate(time.Microsecond)
	old := now.AddDate(0, 0, -40)
	heldLookup := []byte("retention-held-" + heldCaseID)
	freeLookup := []byte("retention-free-" + freeCaseID)
	for _, item := range []struct {
		id     string
		lookup []byte
	}{{heldCaseID, heldLookup}, {freeCaseID, freeLookup}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO privacy_cases(id,case_type,status,subject_lookup_hmac,subject_masked,requested_at,due_at,created_by,executed_by,request_reason,execution_reason,completed_at,version,created_at,updated_at) VALUES($1::uuid,'ACCESS','COMPLETED',$2,'***5678',$3,$4,$5::uuid,$5::uuid,'approved access request','completed subject request',$6,4,$3,$6)`, item.id, item.lookup, old, old.Add(30*24*time.Hour), actorID, old.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO privacy_legal_holds(id,subject_lookup_hmac,scope,status,reason,created_by,submitted_by,decided_by,decision_reason,created_at,activated_at,version) VALUES($1::uuid,$2,'CONTACT','ACTIVE','preserve held privacy evidence',$3::uuid,$3::uuid,$4::uuid,'independent legal hold approval',$5,$5,3)`, holdID, heldLookup, actorID, checkerID, old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO retention_policies(id,name,object_type,action,scope_type,scope_id,retention_days,respect_legal_holds,status,effective_from,created_by,submitted_by,approved_by,reason,version,created_at,updated_at) VALUES($1::uuid,$2,'PRIVACY_CASE','DELETE','PLATFORM','',30,true,'ACTIVE',$3,$4::uuid,$4::uuid,$5::uuid,'approved privacy retention policy',3,$3,$3)`, policyID, "privacy-retention-"+policyID, old.Add(-24*time.Hour), actorID, checkerID); err != nil {
		t.Fatal(err)
	}

	store := &PostgreSQLStore{DB: db}
	scheduled, err := store.ScheduleDue(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if scheduled != 1 {
		t.Fatalf("scheduled=%d, want 1 unheld privacy case", scheduled)
	}

	var heldJobs, freeJobs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE object_id=$2),count(*) FILTER (WHERE object_id=$3) FROM retention_jobs WHERE retention_policy_id=$1::uuid`, policyID, heldCaseID, freeCaseID).Scan(&heldJobs, &freeJobs); err != nil {
		t.Fatal(err)
	}
	if heldJobs != 0 || freeJobs != 1 {
		t.Fatalf("privacy retention jobs held=%d free=%d, want 0/1", heldJobs, freeJobs)
	}
}
