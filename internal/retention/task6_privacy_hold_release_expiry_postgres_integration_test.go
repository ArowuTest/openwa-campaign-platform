package retention

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestTask6PrivacyRetentionRespectsActiveButNotExpiredOrReleasedHolds(t *testing.T) {
	dsn := os.Getenv("POSTGRES_RETENTION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_RETENTION_DATABASE_URL is not set")
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
	ids := make([]string, 10)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, checkerID, orgID, policyID := ids[0], ids[1], ids[2], ids[3]
	activeCaseID, expiredCaseID, releasedCaseID := ids[4], ids[5], ids[6]
	activeHoldID, expiredHoldID, releasedHoldID := ids[7], ids[8], ids[9]
	for _, actor := range []string{actorID, checkerID} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 privacy retention actor','DISABLED',false)`, actor, "task6-privacy-retention-"+actor+"@internal.invalid"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Task 6 privacy retention "+orgID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	old := now.AddDate(0, 0, -40)
	lookups := map[string][]byte{
		activeCaseID:   []byte("task6-active-hold-" + activeCaseID),
		expiredCaseID:  []byte("task6-expired-hold-" + expiredCaseID),
		releasedCaseID: []byte("task6-released-hold-" + releasedCaseID),
	}
	for _, caseID := range []string{activeCaseID, expiredCaseID, releasedCaseID} {
		if _, err := db.ExecContext(ctx, `INSERT INTO privacy_cases(id,case_type,status,subject_lookup_hmac,subject_masked,organisation_id,requested_at,due_at,created_by,executed_by,request_reason,execution_reason,completed_at,version,created_at,updated_at) VALUES($1::uuid,'ACCESS','COMPLETED',$2,'***5678',$3::uuid,$4,$5,$6::uuid,$6::uuid,'approved access request','completed subject request',$7,4,$4,$7)`, caseID, lookups[caseID], orgID, old, old.Add(30*24*time.Hour), actorID, old.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO privacy_legal_holds(id,subject_lookup_hmac,organisation_id,scope,status,reason,created_by,submitted_by,decided_by,decision_reason,created_at,activated_at,expires_at,version) VALUES($1::uuid,$2,$3::uuid,'CONTACT','ACTIVE','preserve active evidence',$4::uuid,$4::uuid,$5::uuid,'independent approval',$6,$6,$7,3)`, activeHoldID, lookups[activeCaseID], orgID, actorID, checkerID, old, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO privacy_legal_holds(id,subject_lookup_hmac,organisation_id,scope,status,reason,created_by,submitted_by,decided_by,decision_reason,created_at,activated_at,expires_at,version) VALUES($1::uuid,$2,$3::uuid,'CONTACT','ACTIVE','expired preservation hold',$4::uuid,$4::uuid,$5::uuid,'independent approval',$6,$6,$7,3)`, expiredHoldID, lookups[expiredCaseID], orgID, actorID, checkerID, old, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO privacy_legal_holds(id,subject_lookup_hmac,organisation_id,scope,status,reason,created_by,submitted_by,decided_by,decision_reason,created_at,activated_at,released_at,released_by,release_reason,version) VALUES($1::uuid,$2,$3::uuid,'CONTACT','RELEASED','released preservation hold',$4::uuid,$4::uuid,$5::uuid,'independent approval',$6,$6,$7,$5::uuid,'lawful hold release',4)`, releasedHoldID, lookups[releasedCaseID], orgID, actorID, checkerID, old, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	effectiveFrom := old.Add(-24 * time.Hour)
	if _, err := db.ExecContext(ctx, `INSERT INTO retention_policies(id,name,object_type,action,scope_type,scope_id,retention_days,respect_legal_holds,status,effective_from,created_by,submitted_by,approved_by,reason,version,created_at,updated_at) VALUES($1::uuid,$2,'PRIVACY_CASE','DELETE','ORGANISATION',$3,30,true,'ACTIVE',$4,$5::uuid,$5::uuid,$6::uuid,'task 6 privacy retention',3,$4,$4)`, policyID, "task6-privacy-hold-"+policyID, orgID, effectiveFrom, actorID, checkerID); err != nil {
		t.Fatal(err)
	}
	policy := Policy{ID: policyID, ObjectType: ObjectPrivacyCase, Action: ActionDelete, ScopeType: ScopeOrganisation, ScopeID: orgID, RetentionDays: 30, RespectLegalHolds: true, Status: StatusActive, EffectiveFrom: effectiveFrom}
	store := &PostgreSQLStore{DB: db}
	scheduled, err := store.schedulePolicy(ctx, policy, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if scheduled != 2 {
		t.Fatalf("scheduled privacy cases=%d want expired+released only", scheduled)
	}
	var activeJobs, expiredJobs, releasedJobs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE object_id=$2),count(*) FILTER(WHERE object_id=$3),count(*) FILTER(WHERE object_id=$4) FROM retention_jobs WHERE retention_policy_id=$1::uuid`, policyID, activeCaseID, expiredCaseID, releasedCaseID).Scan(&activeJobs, &expiredJobs, &releasedJobs); err != nil {
		t.Fatal(err)
	}
	if activeJobs != 0 || expiredJobs != 1 || releasedJobs != 1 {
		t.Fatalf("privacy hold scheduling active=%d expired=%d released=%d want 0/1/1", activeJobs, expiredJobs, releasedJobs)
	}
}
