package dispatch

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestDLV020QueueLossReconstructsExactlyOneEligibleDispatchJob(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `UPDATE campaign_recipients SET status='QUEUED' WHERE id=$1::uuid`, f.Recipient.ID); err != nil {
		t.Fatal(err)
	}
	now := f.AsOf.Add(time.Minute)
	var outboxID, lostJobID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&outboxID, &lostJobID); err != nil {
		t.Fatal(err)
	}
	dedup := "dlv020-queue-loss:" + outboxID
	if _, err := db.ExecContext(ctx, `
INSERT INTO transactional_outbox(
 id,deduplication_key,aggregate_type,aggregate_id,event_type,payload,status,
 available_at,published_at,created_at,updated_at
) VALUES(
 $1::uuid,$2,'CAMPAIGN_RECIPIENT',$3::uuid,'CAMPAIGN_RECIPIENT_AUTHORISED',
 jsonb_build_object('campaignRecipientId',$3::text),'PUBLISHED',$4,$4,$4,$4
)`, outboxID, dedup, f.Recipient.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO durable_jobs(
 id,job_type,deduplication_key,payload,status,priority,attempt_count,max_attempts,
 available_at,created_at,updated_at
) VALUES(
 $1::uuid,$2,$3,jsonb_build_object('campaignRecipientId',$4::text),
 'PENDING',0,0,8,$5,$5,$5
)`, lostJobID, JobType, dedup, f.Recipient.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM durable_jobs WHERE id=$1::uuid`, lostJobID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM durable_jobs WHERE deduplication_key=$1`, dedup)
		_, _ = db.ExecContext(cleanup, `DELETE FROM transactional_outbox WHERE id=$1::uuid`, outboxID)
	})

	repair := &PostgreSQLQueueRepairRepository{DB: db}
	created, err := repair.RepairMissing(ctx, now.Add(time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("queue-loss repair created %d jobs, want 1", created)
	}
	var count int
	var jobType, recipientID string
	if err := db.QueryRowContext(ctx, `
SELECT count(*),max(job_type),max(payload->>'campaignRecipientId')
FROM durable_jobs WHERE deduplication_key=$1`, dedup).Scan(&count, &jobType, &recipientID); err != nil {
		t.Fatal(err)
	}
	if count != 1 || jobType != JobType || recipientID != f.Recipient.ID {
		t.Fatalf("reconstructed count=%d type=%s recipient=%s", count, jobType, recipientID)
	}
	created, err = repair.RepairMissing(ctx, now.Add(2*time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("idempotent queue-loss repair recreated %d jobs, want 0", created)
	}
}
