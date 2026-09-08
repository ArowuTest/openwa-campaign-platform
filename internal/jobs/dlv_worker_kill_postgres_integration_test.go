package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestDLV019WorkerKillRecoveryDoesNotLoseOrDuplicateObligation(t *testing.T) {
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

	var jobID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	dedup := "dlv019-worker-kill:" + jobID
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
INSERT INTO durable_jobs(id,job_type,deduplication_key,payload,status,available_at,created_at,updated_at)
VALUES($1::uuid,'DLV019_WORKER_KILL',$2,'{}'::jsonb,'PENDING',$3,$3,$3)`, jobID, dedup, now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM durable_jobs WHERE id=$1::uuid`, jobID)
	})

	cmd := exec.Command(os.Args[0], "-test.run=^TestDLV019WorkerKillHelper$")
	cmd.Env = append(os.Environ(),
		"DLV019_WORKER_KILL_HELPER=1",
		"DLV019_JOB_ID="+jobID,
		"DLV019_CLAIM_AT="+now.Format(time.RFC3339Nano),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("worker helper failed: %v output=%s", err, output)
	}
	if string(output) == "" {
		t.Fatal("worker helper produced no claim evidence")
	}

	repo := &PostgreSQLRepository{DB: db}
	stored, err := repo.Get(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusProcessing || stored.LeaseOwner != "dlv019-dead-worker" || stored.AttemptCount != 1 || stored.LeaseVersion != 1 {
		t.Fatalf("dead worker claim not persisted as expected: %+v", stored)
	}

	recoveredAt := now.Add(2 * time.Second)
	recovered, err := repo.Claim(ctx, "dlv019-recovery-worker", recoveredAt, time.Minute, 1, []string{"DLV019_WORKER_KILL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 1 || recovered[0].ID != jobID {
		t.Fatalf("recovered=%+v want exactly job %s", recovered, jobID)
	}
	if recovered[0].AttemptCount != 2 || recovered[0].LeaseVersion != 2 {
		t.Fatalf("recovered attempts=%d leaseVersion=%d want 2/2", recovered[0].AttemptCount, recovered[0].LeaseVersion)
	}
	if err := repo.Complete(ctx, jobID, "dlv019-dead-worker", 1, recoveredAt.Add(time.Second)); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("dead worker completed reclaimed obligation: %v", err)
	}
	if err := repo.Complete(ctx, jobID, "dlv019-recovery-worker", recovered[0].LeaseVersion, recoveredAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	stored, err = repo.Get(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusCompleted || stored.AttemptCount != 2 {
		t.Fatalf("final job=%+v", stored)
	}
}

func TestDLV019WorkerKillHelper(t *testing.T) {
	if os.Getenv("DLV019_WORKER_KILL_HELPER") != "1" {
		t.Skip("helper subprocess only")
	}
	dsn := os.Getenv("POSTGRES_JOBS_DATABASE_URL")
	jobID := os.Getenv("DLV019_JOB_ID")
	claimAt, err := time.Parse(time.RFC3339Nano, os.Getenv("DLV019_CLAIM_AT"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer db.Close()
	repo := &PostgreSQLRepository{DB: db}
	claimed, err := repo.Claim(context.Background(), "dlv019-dead-worker", claimAt, time.Second, 1, []string{"DLV019_WORKER_KILL"})
	if err != nil || len(claimed) != 1 || claimed[0].ID != jobID {
		fmt.Fprintf(os.Stderr, "claim=%+v err=%v\n", claimed, err)
		os.Exit(3)
	}
	fmt.Printf("CLAIMED %s lease=%d\n", claimed[0].ID, claimed[0].LeaseVersion)
	os.Exit(0)
}
