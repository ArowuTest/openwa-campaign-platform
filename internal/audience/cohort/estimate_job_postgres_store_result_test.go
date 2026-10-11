package cohort

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/jobs"
	_ "campaign-platform/internal/persistence/database"
)

type estimateStoreResultFixture struct {
	db       *sql.DB
	repo     *PostgreSQLEstimateRepository
	queue    *jobs.PostgreSQLRepository
	record   EstimateJobRecord
	lease    jobs.Job
	result   Estimate
	evidence EstimateGovernanceEvidence
}

func newEstimateStoreResultFixture(t *testing.T, leaseDuration time.Duration) estimateStoreResultFixture {
	t.Helper()
	dsn := os.Getenv("POSTGRES_COHORT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_COHORT_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var now time.Time
	var actorID, orgID, purposeID, reviewID, policyID string
	if err := db.QueryRowContext(ctx, `SELECT clock_timestamp(),gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&now, &actorID, &orgID, &purposeID, &reviewID, &policyID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		// The estimate/job cleanup registered below runs first. Delete only
		// this fixture's supporting rows, in foreign-key dependency order.
		for _, row := range []struct{ table, id string }{
			{"consent_purposes", purposeID},
			{"consent_reviews", reviewID},
			{"organisation_policy_versions", policyID},
			{"organisations", orgID},
			{"internal_users", actorID},
		} {
			if _, err := db.ExecContext(cleanupCtx, "DELETE FROM "+row.table+" WHERE id=$1::uuid", row.id); err != nil {
				t.Errorf("cleanup fixture %s: %v", row.table, err)
			}
		}
	})
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Store Result Actor','DISABLED',false)`, actorID, "estimate-store-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Store Result "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Store result review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, actorID, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Store result purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "STORE_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisation_policy_versions(id,organisation_id,allowed_purpose_ids,prohibited_purpose_ids,frequency_caps,contact_retention_days,campaign_retention_days,status,effective_from,version,created_by,submitted_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,'[]'::jsonb,'[]'::jsonb,'[]'::jsonb,365,365,'ACTIVE',$3,1,$4::uuid,$4::uuid,$4::uuid,'store result policy',$3,$3)`, policyID, orgID, now.Add(-time.Hour), actorID); err != nil {
		t.Fatal(err)
	}

	queue := &jobs.PostgreSQLRepository{DB: db}
	repo := &PostgreSQLEstimateRepository{DB: db, Queue: queue}
	request := estimateTestRequest()
	request.OrganisationID, request.PurposeID, request.RequestedBy = orgID, purposeID, actorID
	request.ClientRequestID = "store-result-" + orgID
	record, created, err := repo.Schedule(ctx, request, now.Add(-time.Second))
	if err != nil || !created {
		t.Fatalf("schedule created=%v err=%v", created, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, `DELETE FROM audience_cohort_estimates WHERE id=$1::uuid`, record.ID); err != nil {
			t.Errorf("cleanup estimate result: %v", err)
		}
		if _, err := db.ExecContext(cleanupCtx, `DELETE FROM durable_jobs WHERE id=$1::uuid`, record.ID); err != nil {
			t.Errorf("cleanup estimate job: %v", err)
		}
	})
	claimed, err := queue.Claim(ctx, "store-owner-a", now, leaseDuration, 1, []string{EstimateJobType})
	if err != nil || len(claimed) != 1 || claimed[0].ID != record.ID {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	return estimateStoreResultFixture{
		db: db, repo: repo, queue: queue, record: record, lease: claimed[0],
		result:   Estimate{EligibleCount: 42, Breakdown: &EligibilityBreakdown{MatchedProfiles: 42, ConsentEligible: 42, Unsuppressed: 42, Eligible: 42}, CalculatedAt: now},
		evidence: EstimateGovernanceEvidence{ConsentReviewID: reviewID, ConsentReviewVersion: 1, ConsentWordingVersion: "v1", OrganisationPolicyID: policyID, OrganisationPolicyVersion: 1},
	}
}

// The source job must not remain an unlocked statement-snapshot row while the
// result UPDATE waits for the estimate row. A takeover may happen in that gap.
func TestPostgreSQLEstimateStoreResultRejectsExpiredOwnerAfterEstimateLockWait(t *testing.T) {
	fixture := newEstimateStoreResultFixture(t, 2*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	blocker, err := fixture.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	var blockerPID int
	var estimateID string
	if err := blocker.QueryRowContext(ctx, `SELECT pg_backend_pid(),id::text FROM audience_cohort_estimates WHERE id=$1::uuid FOR UPDATE`, fixture.record.ID).Scan(&blockerPID, &estimateID); err != nil {
		t.Fatal(err)
	}

	storeDB, err := sql.Open("postgres", os.Getenv("POSTGRES_COHORT_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer storeDB.Close()
	storeDB.SetMaxOpenConns(1)
	var storePID int
	if err := storeDB.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&storePID); err != nil {
		t.Fatal(err)
	}
	storeRepo := &PostgreSQLEstimateRepository{DB: storeDB}
	var writeAt time.Time
	if err := fixture.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&writeAt); err != nil {
		t.Fatal(err)
	}
	if !writeAt.Before(*fixture.lease.LeaseExpiresAt) {
		t.Fatal("fixture lease expired before StoreResult started")
	}
	done := make(chan error, 1)
	go func() {
		done <- storeRepo.StoreResult(ctx, fixture.record.ID, fixture.lease.LeaseOwner, fixture.lease.LeaseVersion, fixture.result, fixture.evidence, writeAt)
	}()

	waitEstimateStoreResultCondition(t, ctx, fixture.db, `SELECT $2=ANY(pg_blocking_pids($1))`, storePID, blockerPID)
	waitEstimateStoreResultCondition(t, ctx, fixture.db, `SELECT clock_timestamp()>=$1`, fixture.lease.LeaseExpiresAt)
	var reclaimAt time.Time
	if err := fixture.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&reclaimAt); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := fixture.queue.Claim(ctx, "store-owner-b", reclaimAt, time.Minute, 1, []string{EstimateJobType})
	if err != nil {
		t.Fatal(err)
	}
	if len(reclaimed) > 1 || (len(reclaimed) == 1 && reclaimed[0].ID != fixture.record.ID) {
		t.Fatalf("unexpected reclaimed job: %+v", reclaimed)
	}
	// Before the fix this succeeds while StoreResult is blocked. After the
	// fix SKIP LOCKED cannot reclaim the job while StoreResult holds its lock.
	t.Logf("reclaimed while estimate row locked: %d", len(reclaimed))
	if len(reclaimed) != 0 {
		t.Errorf("job was reclaimed while StoreResult was waiting for the estimate row: %+v", reclaimed)
	}
	if err := blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, jobs.ErrLeaseConflict) {
			t.Fatalf("expired/stale owner published after estimate lock wait: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stored, err := fixture.repo.Get(ctx, fixture.record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Result != nil {
		t.Fatalf("expired/stale result was persisted: %+v", stored.Result)
	}
	if len(reclaimed) == 0 {
		if err := fixture.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&reclaimAt); err != nil {
			t.Fatal(err)
		}
		reclaimed, err = fixture.queue.Claim(ctx, "store-owner-b", reclaimAt, time.Minute, 1, []string{EstimateJobType})
		if err != nil || len(reclaimed) != 1 || reclaimed[0].ID != fixture.record.ID {
			t.Fatalf("reclaim after rejected write=%+v err=%v", reclaimed, err)
		}
	}
	if err := fixture.repo.StoreResult(ctx, fixture.record.ID, reclaimed[0].LeaseOwner, reclaimed[0].LeaseVersion, fixture.result, fixture.evidence, reclaimAt); err != nil {
		t.Fatalf("live new owner could not store result: %v", err)
	}
	if err := fixture.repo.StoreResult(ctx, fixture.record.ID, fixture.lease.LeaseOwner, fixture.lease.LeaseVersion, fixture.result, fixture.evidence, writeAt); !errors.Is(err, jobs.ErrLeaseConflict) {
		t.Fatalf("stale owner overwrote new owner result: %v", err)
	}
	stored, err = fixture.repo.Get(ctx, fixture.record.ID)
	if err != nil || stored.Result == nil || stored.Result.EligibleCount != 42 || stored.Evidence != fixture.evidence {
		t.Fatalf("live owner result=%+v err=%v", stored, err)
	}
}

func waitEstimateStoreResultCondition(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		if err := db.QueryRowContext(ctx, query, args...).Scan(&ready); err != nil {
			t.Fatal(err)
		}
		if ready {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestPostgreSQLEstimateStoreResultCancellationWhileWaitingForRowLocks(t *testing.T) {
	for _, table := range []string{"durable_jobs", "audience_cohort_estimates"} {
		t.Run(table, func(t *testing.T) {
			fixture := newEstimateStoreResultFixture(t, time.Minute)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			blocker, err := fixture.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			var identifier string
			if err := blocker.QueryRowContext(ctx, "SELECT id::text FROM "+table+" WHERE id=$1::uuid FOR UPDATE", fixture.record.ID).Scan(&identifier); err != nil {
				t.Fatal(err)
			}
			writeCtx, writeCancel := context.WithTimeout(ctx, 150*time.Millisecond)
			defer writeCancel()
			err = fixture.repo.StoreResult(writeCtx, fixture.record.ID, fixture.lease.LeaseOwner, fixture.lease.LeaseVersion, fixture.result, fixture.evidence, fixture.result.CalculatedAt)
			if err == nil || !errors.Is(writeCtx.Err(), context.DeadlineExceeded) {
				t.Fatalf("blocked write did not honor deadline: err=%v context=%v", err, writeCtx.Err())
			}
			if err := blocker.Rollback(); err != nil {
				t.Fatal(err)
			}
			stored, err := fixture.repo.Get(ctx, fixture.record.ID)
			if err != nil || stored.Result != nil {
				t.Fatalf("cancelled write persisted result=%+v err=%v", stored.Result, err)
			}
			if err := fixture.repo.StoreResult(ctx, fixture.record.ID, fixture.lease.LeaseOwner, fixture.lease.LeaseVersion, fixture.result, fixture.evidence, fixture.result.CalculatedAt); err != nil {
				t.Fatalf("live-owner write after cancellation: %v", err)
			}
		})
	}
}

func TestPostgreSQLEstimateStoreResultWithAudienceWorkerRole(t *testing.T) {
	fixture := newEstimateStoreResultFixture(t, time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	workerDB, err := sql.Open("postgres", os.Getenv("POSTGRES_COHORT_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer workerDB.Close()
	workerDB.SetMaxOpenConns(1)
	workerDB.SetMaxIdleConns(1)
	if _, err := workerDB.ExecContext(ctx, "SET ROLE campaign_audience_worker"); err != nil {
		t.Fatal(err)
	}
	defer workerDB.ExecContext(context.Background(), "RESET ROLE")

	if err := fixture.queue.Fail(ctx, fixture.record.ID, fixture.lease.LeaseOwner, fixture.lease.LeaseVersion, fixture.result.CalculatedAt, true, 0, "ROLE_TEST", "release fixture owner"); err != nil {
		t.Fatal(err)
	}
	queue := &jobs.PostgreSQLRepository{DB: workerDB}
	repo := &PostgreSQLEstimateRepository{DB: workerDB, Queue: queue}
	claimed, err := queue.Claim(ctx, "audience-role-worker", fixture.result.CalculatedAt, time.Minute, 1, []string{EstimateJobType})
	if err != nil || len(claimed) != 1 || claimed[0].ID != fixture.record.ID {
		t.Fatalf("worker-role claim=%+v err=%v", claimed, err)
	}
	record, err := repo.Get(ctx, fixture.record.ID)
	if err != nil || record.Result != nil {
		t.Fatalf("worker-role get=%+v err=%v", record, err)
	}
	if err := repo.StoreResult(ctx, fixture.record.ID, claimed[0].LeaseOwner, claimed[0].LeaseVersion, fixture.result, fixture.evidence, fixture.result.CalculatedAt); err != nil {
		t.Fatalf("worker-role fenced result write: %v", err)
	}
	if err := queue.Complete(ctx, fixture.record.ID, claimed[0].LeaseOwner, claimed[0].LeaseVersion, fixture.result.CalculatedAt); err != nil {
		t.Fatalf("worker-role completion: %v", err)
	}
	stored, err := repo.Get(ctx, fixture.record.ID)
	if err != nil || stored.Result == nil || stored.Result.EligibleCount != 42 || stored.Evidence != fixture.evidence || stored.Job.Status != jobs.StatusCompleted {
		t.Fatalf("worker-role stored result=%+v err=%v", stored, err)
	}
}
