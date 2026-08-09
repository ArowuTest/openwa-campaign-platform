package integration_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgresAdversarialConcurrency(t *testing.T) {
	dsn := os.Getenv("POSTGRES_ADVERSARIAL_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_ADVERSARIAL_DATABASE_URL is not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(12)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	schema := "adversarial_" + randomHex(t, 6)
	quotedSchema := quoteIdentifier(schema)
	if _, err := db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS btree_gist`); err != nil {
		t.Fatalf("create btree_gist: %v", err)
	}
	setupSQL := fmt.Sprintf(`
CREATE SCHEMA %s;
CREATE TABLE %s.release_targets(id uuid PRIMARY KEY DEFAULT gen_random_uuid(), status text NOT NULL, version bigint NOT NULL);
CREATE TABLE %s.jobs(id bigint PRIMARY KEY, status text NOT NULL, owner text);
CREATE TABLE %s.provider_events(provider text NOT NULL, event_id text NOT NULL, payload jsonb NOT NULL, PRIMARY KEY(provider,event_id));
CREATE TABLE %s.fences(id uuid PRIMARY KEY DEFAULT gen_random_uuid(), fence_version bigint NOT NULL, state text NOT NULL);
CREATE TABLE %s.outbox(id uuid PRIMARY KEY DEFAULT gen_random_uuid(), status text NOT NULL);
CREATE TABLE %s.obligations(id uuid PRIMARY KEY DEFAULT gen_random_uuid(), outbox_id uuid NOT NULL REFERENCES %s.outbox(id));
CREATE TABLE %s.capacity_reservations(id uuid PRIMARY KEY DEFAULT gen_random_uuid(), pool_id uuid NOT NULL, reserved_window tstzrange NOT NULL, EXCLUDE USING gist(pool_id WITH =, reserved_window WITH &&));
INSERT INTO %s.release_targets(status,version) VALUES('PENDING',1);
INSERT INTO %s.jobs(id,status) VALUES(1,'QUEUED'),(2,'QUEUED');
INSERT INTO %s.fences(fence_version,state) VALUES(7,'READY');`,
		quotedSchema, quotedSchema, quotedSchema, quotedSchema, quotedSchema, quotedSchema,
		quotedSchema, quotedSchema, quotedSchema, quotedSchema, quotedSchema, quotedSchema,
	)
	for _, statement := range strings.Split(setupSQL, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("create adversarial schema: %v", err)
		}
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop adversarial schema: %v", err)
		}
	}()

	t.Run("concurrent release has one winner", func(t *testing.T) {
		query := fmt.Sprintf(`UPDATE %s.release_targets SET status='APPROVED',version=2 WHERE status='PENDING' AND version=1`, quotedSchema)
		counts := concurrentExec(t, ctx, db, query, query)
		if counts[0]+counts[1] != 1 {
			t.Fatalf("expected one release winner, got %v", counts)
		}
	})

	t.Run("skip locked claims distinct jobs", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`UPDATE %s.jobs SET status='QUEUED',owner=NULL`, quotedSchema)); err != nil {
			t.Fatal(err)
		}
		firstQuery := fmt.Sprintf(`WITH picked AS MATERIALIZED (
  SELECT id FROM %s.jobs WHERE status='QUEUED' ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1
), held AS MATERIALIZED (
  SELECT pg_sleep(0.8) FROM picked
), changed AS (
  UPDATE %s.jobs AS job SET status='CLAIMED',owner='worker-one'
  FROM picked,held WHERE job.id=picked.id RETURNING job.id
) SELECT id FROM changed`, quotedSchema, quotedSchema)
		secondQuery := fmt.Sprintf(`WITH picked AS MATERIALIZED (
  SELECT id FROM %s.jobs WHERE status='QUEUED' ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1
), changed AS (
  UPDATE %s.jobs AS job SET status='CLAIMED',owner='worker-two'
  FROM picked WHERE job.id=picked.id RETURNING job.id
) SELECT id FROM changed`, quotedSchema, quotedSchema)

		var firstID, secondID int64
		var firstErr, secondErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			firstErr = db.QueryRowContext(ctx, firstQuery).Scan(&firstID)
		}()
		time.Sleep(150 * time.Millisecond)
		go func() {
			defer wg.Done()
			secondErr = db.QueryRowContext(ctx, secondQuery).Scan(&secondID)
		}()
		wg.Wait()
		if firstErr != nil || secondErr != nil {
			t.Fatalf("claim errors: first=%v second=%v", firstErr, secondErr)
		}
		if firstID == secondID {
			t.Fatalf("workers claimed the same job %d", firstID)
		}
	})

	t.Run("duplicate provider event persists once", func(t *testing.T) {
		queryOne := fmt.Sprintf(`INSERT INTO %s.provider_events(provider,event_id,payload) VALUES('openwa','duplicate-1','{"source":"one"}') ON CONFLICT DO NOTHING`, quotedSchema)
		queryTwo := fmt.Sprintf(`INSERT INTO %s.provider_events(provider,event_id,payload) VALUES('openwa','duplicate-1','{"source":"two"}') ON CONFLICT DO NOTHING`, quotedSchema)
		counts := concurrentExec(t, ctx, db, queryOne, queryTwo)
		if counts[0]+counts[1] != 1 {
			t.Fatalf("expected one provider-event insert, got %v", counts)
		}
	})

	t.Run("stale fence loses", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`UPDATE %s.fences SET fence_version=7,state='READY'`, quotedSchema)); err != nil {
			t.Fatal(err)
		}
		queryOne := fmt.Sprintf(`UPDATE %s.fences SET fence_version=8,state='DRAINING' WHERE fence_version=7`, quotedSchema)
		queryTwo := fmt.Sprintf(`UPDATE %s.fences SET fence_version=8,state='PAUSED' WHERE fence_version=7`, quotedSchema)
		counts := concurrentExec(t, ctx, db, queryOne, queryTwo)
		if counts[0]+counts[1] != 1 {
			t.Fatalf("expected one fence winner, got %v", counts)
		}
	})

	t.Run("outbox boundary is atomic", func(t *testing.T) {
		objectID := randomUUID(t)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s.outbox(id,status) VALUES($1,'PENDING')`, quotedSchema), objectID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s.obligations(outbox_id) VALUES($1)`, quotedSchema), objectID); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		assertCounts(t, ctx, db, quotedSchema, 0, 0)

		tx, err = db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s.outbox(id,status) VALUES($1,'PENDING')`, quotedSchema), objectID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s.obligations(outbox_id) VALUES($1)`, quotedSchema), objectID); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		assertCounts(t, ctx, db, quotedSchema, 1, 1)
	})

	t.Run("overlapping capacity reservation has one winner", func(t *testing.T) {
		poolID := randomUUID(t)
		queryOne := fmt.Sprintf(`INSERT INTO %s.capacity_reservations(pool_id,reserved_window) VALUES($1,tstzrange('2026-08-06 20:00:00+00','2026-08-06 21:00:00+00','[)')) ON CONFLICT DO NOTHING`, quotedSchema)
		queryTwo := fmt.Sprintf(`INSERT INTO %s.capacity_reservations(pool_id,reserved_window) VALUES($1,tstzrange('2026-08-06 20:30:00+00','2026-08-06 21:30:00+00','[)')) ON CONFLICT DO NOTHING`, quotedSchema)
		counts := concurrentExecArgs(t, ctx, db, queryOne, queryTwo, poolID)
		if counts[0]+counts[1] != 1 {
			t.Fatalf("expected one capacity reservation, got %v", counts)
		}
	})
}

func concurrentExec(t *testing.T, ctx context.Context, db *sql.DB, first, second string) [2]int64 {
	t.Helper()
	return concurrentExecArgs(t, ctx, db, first, second)
}

func concurrentExecArgs(t *testing.T, ctx context.Context, db *sql.DB, first, second string, args ...any) [2]int64 {
	t.Helper()
	start := make(chan struct{})
	var counts [2]int64
	var errs [2]error
	var wg sync.WaitGroup
	queries := [2]string{first, second}
	wg.Add(2)
	for index := range queries {
		index := index
		go func() {
			defer wg.Done()
			<-start
			result, err := db.ExecContext(ctx, queries[index], args...)
			if err != nil {
				errs[index] = err
				return
			}
			counts[index], errs[index] = result.RowsAffected()
		}()
	}
	close(start)
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("concurrent execution errors: first=%v second=%v", errs[0], errs[1])
	}
	return counts
}

func assertCounts(t *testing.T, ctx context.Context, db *sql.DB, schema string, outbox, obligations int64) {
	t.Helper()
	var outboxCount, obligationCount int64
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+schema+".outbox").Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+schema+".obligations").Scan(&obligationCount); err != nil {
		t.Fatal(err)
	}
	if outboxCount != outbox || obligationCount != obligations {
		t.Fatalf("unexpected atomic counts: outbox=%d obligations=%d", outboxCount, obligationCount)
	}
}

func randomHex(t *testing.T, byteCount int) string {
	t.Helper()
	value := make([]byte, byteCount)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(value)
}

func randomUUID(t *testing.T) string {
	t.Helper()
	raw := randomHex(t, 16)
	return fmt.Sprintf("%s-%s-%s-%s-%s", raw[0:8], raw[8:12], raw[12:16], raw[16:20], raw[20:32])
}

func quoteIdentifier(value string) string {
	return `"` + value + `"`
}
