package jobs

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLJobClaimIsExclusiveAndExpiredClaimIsRecoverable(t *testing.T) {
	dsn := os.Getenv("POSTGRES_JOBS_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_JOBS_DATABASE_URL is not set")
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
	var jobID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	dedup := "claim-evidence-" + jobID
	now := time.Date(2002, 2, 3, 4, 5, 6, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO durable_jobs(id,job_type,deduplication_key,payload,status,available_at,created_at,updated_at) VALUES($1::uuid,'CLAIM_EVIDENCE',$2,'{}'::jsonb,'PENDING',$3,$3,$3)`, jobID, dedup, now); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM durable_jobs WHERE id=$1::uuid`, jobID)
	}()
	repository := &PostgreSQLRepository{DB: db}
	type outcome struct {
		jobs []Job
		err  error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	for _, owner := range []string{"worker-a", "worker-b"} {
		owner := owner
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, claimErr := repository.Claim(ctx, owner, now, time.Second, 1, []string{"CLAIM_EVIDENCE"})
			results <- outcome{jobs: claimed, err: claimErr}
		}()
	}
	wg.Wait()
	close(results)
	var winner Job
	total := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		total += len(result.jobs)
		if len(result.jobs) == 1 {
			winner = result.jobs[0]
		}
	}
	if total != 1 || winner.ID != jobID {
		t.Fatalf("total claims=%d winner=%+v", total, winner)
	}

	otherOwner := "worker-c"
	recoveredAt := now.Add(2 * time.Second)
	recovered, err := repository.Claim(ctx, otherOwner, recoveredAt, time.Minute, 1, []string{"CLAIM_EVIDENCE"})
	if err != nil || len(recovered) != 1 || recovered[0].ID != jobID {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	if recovered[0].LeaseVersion <= winner.LeaseVersion {
		t.Fatalf("lease fence did not advance: old=%d new=%d", winner.LeaseVersion, recovered[0].LeaseVersion)
	}
	if err := repository.Complete(ctx, jobID, winner.LeaseOwner, winner.LeaseVersion, recoveredAt.Add(time.Second)); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("stale owner completed recovered job: %v", err)
	}
	if err := repository.Complete(ctx, jobID, otherOwner, recovered[0].LeaseVersion, recoveredAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.Get(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusCompleted || stored.AttemptCount != 2 {
		t.Fatalf("stored=%+v", stored)
	}
}
