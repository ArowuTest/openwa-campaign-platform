package dispatch

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestTask6QueueRepairNeverReconstructsUnknownRecipient(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `UPDATE campaign_recipients SET status='UNKNOWN',reconciliation_required=true WHERE id=$1::uuid`, f.Recipient.ID); err != nil {
		t.Fatal(err)
	}

	var outboxID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&outboxID); err != nil {
		t.Fatal(err)
	}
	dedup := "task6-unknown-repair:" + outboxID
	now := f.AsOf.Add(time.Minute)
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
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM durable_jobs WHERE deduplication_key=$1`, dedup)
		_, _ = db.ExecContext(cleanup, `DELETE FROM transactional_outbox WHERE id=$1::uuid`, outboxID)
	}()

	repair := &PostgreSQLQueueRepairRepository{DB: db}
	created, err := repair.RepairMissing(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("UNKNOWN repair created %d job(s)", created)
	}
	var jobCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM durable_jobs WHERE deduplication_key=$1`, dedup).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if jobCount != 0 {
		t.Fatalf("UNKNOWN repair persisted %d job(s)", jobCount)
	}

	if _, err := db.ExecContext(ctx, `UPDATE campaign_recipients SET status='FAILED_RETRYABLE',reconciliation_required=false WHERE id=$1::uuid`, f.Recipient.ID); err != nil {
		t.Fatal(err)
	}
	created, err = repair.RepairMissing(ctx, now.Add(time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("eligible retry repair created %d jobs, want 1", created)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM durable_jobs WHERE deduplication_key=$1`, dedup).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if jobCount != 1 {
		t.Fatalf("eligible retry repair persisted %d jobs, want 1", jobCount)
	}
}
