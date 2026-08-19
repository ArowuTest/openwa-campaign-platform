package retention

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLRetentionCompletionPersistsJSONEvidence(t *testing.T) {
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
	var actorID, policyID, jobID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actorID, &policyID, &jobID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 3, 4, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(time.Minute)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 retention','DISABLED',false)`, actorID, "retention-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO retention_policies(id,name,object_type,action,scope_type,scope_id,retention_days,respect_legal_holds,status,effective_from,created_by,reason,version,created_at,updated_at) VALUES($1::uuid,'Task 6 retention','EXPORT_OBJECT','DELETE','PLATFORM','',30,true,'ACTIVE',$2,$3::uuid,'task 6 retention policy',1,$2,$2)`, policyID, now.Add(-time.Hour), actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO retention_jobs(id,retention_policy_id,object_type,object_id,action,status,available_at,lease_owner,lease_version,lease_expires_at,attempt_count,evidence,created_at,updated_at) VALUES($1::uuid,$2::uuid,'EXPORT_OBJECT','task6-object','DELETE','CLAIMED',$3,'task6-worker',1,$4,1,'{}'::jsonb,$3,$3)`, jobID, policyID, now, leaseUntil); err != nil {
		t.Fatal(err)
	}
	job := Job{ID: jobID, PolicyID: policyID, ObjectType: ObjectExportObject, ObjectID: "task6-object", Action: ActionDelete, Status: JobClaimed, LeaseOwner: "task6-worker", LeaseVersion: 1, LeaseExpiresAt: &leaseUntil}
	evidence := map[string]any{"deleted": true, "objectKey": "exports/task6.csv"}
	store := &PostgreSQLStore{DB: db}
	if err := store.CompleteJob(ctx, job, evidence, now.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	var status string
	var deleted bool
	var objectKey string
	if err := db.QueryRowContext(ctx, `SELECT status,evidence->>'deleted'='true',coalesce(evidence->>'objectKey','') FROM retention_jobs WHERE id=$1::uuid`, jobID).Scan(&status, &deleted, &objectKey); err != nil {
		t.Fatal(err)
	}
	if status != string(JobCompleted) || !deleted || objectKey != "exports/task6.csv" {
		t.Fatalf("unexpected persisted retention completion status=%s deleted=%v objectKey=%q", status, deleted, objectKey)
	}
}
