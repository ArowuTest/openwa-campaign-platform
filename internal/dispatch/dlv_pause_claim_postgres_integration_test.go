package dispatch

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/jobs"
	_ "campaign-platform/internal/persistence/database"
)

func TestDLV017PausedCampaignStopsNewDispatchClaimsUntilResume(t *testing.T) {
	dsn := os.Getenv("POSTGRES_FINAL_ELIGIBILITY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_FINAL_ELIGIBILITY_DATABASE_URL is not set")
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

	f := seedFinalEligibilityFixture(t, ctx, db)
	now := f.AsOf.Add(time.Minute)
	var jobID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	dedup := "dlv017-pause-claim:" + jobID
	if _, err := db.ExecContext(ctx, `
INSERT INTO durable_jobs(
 id,job_type,deduplication_key,payload,status,priority,attempt_count,max_attempts,
 available_at,created_at,updated_at
) VALUES(
 $1::uuid,$2,$3,jsonb_build_object('campaignRecipientId',$4::text),
 'PENDING',0,0,8,$5,$5,$5
)`, jobID, JobType, dedup, f.Recipient.ID, now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM durable_jobs WHERE id=$1::uuid`, jobID)
	})

	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET status='PAUSED' WHERE id=$1::uuid`, f.CampaignID); err != nil {
		t.Fatal(err)
	}
	repo := &jobs.PostgreSQLRepository{DB: db}
	claimed, err := repo.Claim(ctx, "dlv017-paused", now, time.Minute, 10, []string{JobType})
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range claimed {
		if job.ID == jobID {
			t.Fatalf("paused campaign dispatch job was claimed: %+v", job)
		}
	}
	stored, err := repo.Get(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != jobs.StatusPending || stored.AttemptCount != 0 {
		t.Fatalf("paused job mutated status=%s attempts=%d", stored.Status, stored.AttemptCount)
	}

	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET status='DISPATCHING' WHERE id=$1::uuid`, f.CampaignID); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.Claim(ctx, "dlv017-resumed", now.Add(time.Second), time.Minute, 10, []string{JobType})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, job := range claimed {
		if job.ID == jobID {
			found = true
			if job.AttemptCount != 1 {
				t.Fatalf("resumed job attempt_count=%d want 1", job.AttemptCount)
			}
		}
	}
	if !found {
		t.Fatal("resumed campaign dispatch job was not claimable")
	}
}
