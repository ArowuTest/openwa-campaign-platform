package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/jobs"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLOutboxPublisherReclaimsExpiredCrashAndCreatesOneJob(t *testing.T) {
	dsn := os.Getenv("POSTGRES_OUTBOX_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_OUTBOX_DATABASE_URL is not set")
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
	var outboxID, recipientID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&outboxID, &recipientID); err != nil {
		t.Fatal(err)
	}
	dedup := "dispatch:crash-recovery:" + outboxID
	payload, _ := json.Marshal(map[string]any{"campaignRecipientId": recipientID})
	now := time.Date(2001, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO transactional_outbox(id,deduplication_key,aggregate_type,aggregate_id,event_type,payload,status,available_at,created_at,updated_at) VALUES($1::uuid,$2,'CAMPAIGN_RECIPIENT',$3::uuid,'CAMPAIGN_RECIPIENT_AUTHORISED',$4::jsonb,'PENDING',$5,$5,$5)`, outboxID, dedup, recipientID, string(payload), now); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM durable_jobs WHERE deduplication_key=$1`, dedup)
		_, _ = db.ExecContext(cleanup, `DELETE FROM transactional_outbox WHERE id=$1::uuid`, outboxID)
	}()

	repository := &PostgreSQLRepository{DB: db}
	first, err := repository.Claim(ctx, "publisher-a", now, time.Second, 1)
	if err != nil || len(first) != 1 || first[0].ID != outboxID {
		t.Fatalf("first claim=%+v err=%v", first, err)
	}
	if first[0].LeaseVersion != 1 {
		t.Fatalf("first lease version=%d", first[0].LeaseVersion)
	}
	// Simulate publisher A crashing: do not Complete or Fail its claim.
	recoveredAt := now.Add(2 * time.Second)
	second, err := repository.Claim(ctx, "publisher-b", recoveredAt, time.Minute, 1)
	if err != nil || len(second) != 1 || second[0].ID != outboxID {
		t.Fatalf("recovered claim=%+v err=%v", second, err)
	}
	if second[0].LeaseVersion <= first[0].LeaseVersion {
		t.Fatalf("lease fence did not advance: first=%d second=%d", first[0].LeaseVersion, second[0].LeaseVersion)
	}

	publisher := &Publisher{Outbox: repository, Jobs: jobs.NewService(&jobs.PostgreSQLRepository{DB: db})}
	if err := publisher.Publish(ctx, second[0]); err != nil {
		t.Fatal(err)
	}
	// Replay the same recovered record before completing the outbox claim. Durable-job
	// deduplication must still leave only one queue job.
	if err := publisher.Publish(ctx, second[0]); err != nil {
		t.Fatal(err)
	}
	if err := repository.Complete(ctx, outboxID, "publisher-b", second[0].LeaseVersion, recoveredAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	var status string
	var jobCount int
	if err := db.QueryRowContext(ctx, `SELECT status FROM transactional_outbox WHERE id=$1::uuid`, outboxID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM durable_jobs WHERE deduplication_key=$1`, dedup).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if status != "PUBLISHED" || jobCount != 1 {
		t.Fatalf("outbox status=%s durable jobs=%d", status, jobCount)
	}
}
