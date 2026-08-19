package outbox

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

func TestPostgreSQLOutboxConcurrentClaimAndStaleFence(t *testing.T) {
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

	var outboxID, aggregateID string
	if err = db.QueryRowContext(ctx,
		`SELECT gen_random_uuid()::text,gen_random_uuid()::text`,
	).Scan(&outboxID, &aggregateID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2000, time.January, 1, 12, 0, 0, 0, time.UTC)
	if _, err = db.ExecContext(ctx, `
INSERT INTO transactional_outbox(
	id,deduplication_key,aggregate_type,aggregate_id,event_type,
	payload,status,available_at,created_at,updated_at
) VALUES(
	$1::uuid,$2,'TEST',$3::uuid,'TEST_EVENT','{}'::jsonb,
	'PENDING',$4,$4,$4
)`, outboxID, "outbox-concurrency:"+outboxID, aggregateID, now); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup,
			`DELETE FROM transactional_outbox WHERE id=$1::uuid`, outboxID)
	}()

	repository := &PostgreSQLRepository{DB: db}
	type outcome struct {
		owner   string
		records []Record
		err     error
	}
	results := make(chan outcome, 2)
	var wait sync.WaitGroup
	for _, owner := range []string{"publisher-a", "publisher-b"} {
		owner := owner
		wait.Add(1)
		go func() {
			defer wait.Done()
			records, claimErr := repository.Claim(
				ctx, owner, now, time.Second, 1,
			)
			results <- outcome{owner: owner, records: records, err: claimErr}
		}()
	}
	wait.Wait()
	close(results)

	var winnerOwner string
	var winner Record
	total := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		total += len(result.records)
		if len(result.records) == 1 {
			winnerOwner, winner = result.owner, result.records[0]
		}
	}
	if total != 1 || winner.ID != outboxID {
		t.Fatalf("concurrent claims=%d winner=%+v", total, winner)
	}
	overlap, err := repository.Claim(
		ctx, winnerOwner, now.Add(500*time.Millisecond), time.Second, 1,
	)
	if err != nil || len(overlap) != 0 {
		t.Fatalf("unexpired overlap=%+v err=%v", overlap, err)
	}

	recoveredAt := now.Add(2 * time.Second)
	recovered, err := repository.Claim(
		ctx, winnerOwner, recoveredAt, time.Minute, 1,
	)
	if err != nil || len(recovered) != 1 {
		t.Fatalf("recovered claim=%+v err=%v", recovered, err)
	}
	if recovered[0].LeaseVersion <= winner.LeaseVersion {
		t.Fatalf("fence did not advance old=%d new=%d",
			winner.LeaseVersion, recovered[0].LeaseVersion)
	}
	err = repository.Complete(
		ctx, outboxID, winnerOwner, winner.LeaseVersion,
		recoveredAt.Add(500*time.Millisecond),
	)
	if !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("stale completion error=%v", err)
	}
	current, err := repository.Get(ctx, outboxID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != StatusProcessing ||
		current.LeaseVersion != recovered[0].LeaseVersion {
		t.Fatalf("stale completion changed record: %+v", current)
	}
	if err = repository.Complete(
		ctx, outboxID, winnerOwner, recovered[0].LeaseVersion,
		recoveredAt.Add(time.Second),
	); err != nil {
		t.Fatal(err)
	}
}
