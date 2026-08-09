package gateway

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

func TestPostgreSQLSessionLeaseHasOneOwnerAndFencesExpiredOwner(t *testing.T) {
	dsn := os.Getenv("POSTGRES_GATEWAY_LEASE_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_GATEWAY_LEASE_DATABASE_URL is not set")
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
	var sessionID, workerA, workerB string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&sessionID, &workerA, &workerB); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_session_leases WHERE session_id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id IN ($1::uuid,$2::uuid)`, workerA, workerB)
	}()
	for _, worker := range []struct{ id, name string }{{workerA, "lease-worker-a"}, {workerB, "lease-worker-b"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO sender_nodes(id,name,status,capacity) VALUES($1::uuid,$2,'READY',1)`, worker.id, worker.name+"-"+worker.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_sessions(id,encrypted_msisdn,masked_msisdn,engine_type,status,safe_messages_per_minute,safe_daily_capacity,in_flight_limit) VALUES($1::uuid,decode('00','hex'),'***9007','WHATSAPP_WEB_JS','READY',10,100,1)`, sessionID); err != nil {
		t.Fatal(err)
	}

	store := &PostgreSQLLeaseStore{DB: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	type result struct {
		lease Lease
		err   error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, contender := range []struct{ worker, token string }{{workerA, "token-a"}, {workerB, "token-b"}} {
		contender := contender
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, acquireErr := store.Acquire(ctx, sessionID, contender.worker, contender.token, now, time.Minute)
			results <- result{lease: lease, err: acquireErr}
		}()
	}
	wg.Wait()
	close(results)

	var winner Lease
	held := 0
	for outcome := range results {
		switch {
		case outcome.err == nil:
			if winner.SessionID != "" {
				t.Fatal("both workers acquired the same live sender session")
			}
			winner = outcome.lease
		case errors.Is(outcome.err, ErrLeaseHeld):
			held++
		default:
			t.Fatalf("unexpected acquire error: %v", outcome.err)
		}
	}
	if winner.SessionID == "" || held != 1 {
		t.Fatalf("expected one owner and one held contender; winner=%+v held=%d", winner, held)
	}
	loserWorker, loserToken := workerA, "token-a"
	if winner.WorkerID == workerA {
		loserWorker, loserToken = workerB, "token-b"
	}
	if err := store.Validate(ctx, winner, now.Add(30*time.Second)); err != nil {
		t.Fatalf("current winner failed validation: %v", err)
	}

	takeoverAt := winner.ExpiresAt.Add(time.Second)
	takeover, err := store.Acquire(ctx, sessionID, loserWorker, loserToken, takeoverAt, time.Minute)
	if err != nil {
		t.Fatalf("expired lease could not be taken over: %v", err)
	}
	if takeover.WorkerID != loserWorker || takeover.Version <= winner.Version {
		t.Fatalf("takeover did not advance ownership fence: old=%+v new=%+v", winner, takeover)
	}
	if err := store.Validate(ctx, winner, takeoverAt); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired owner remained valid after takeover: %v", err)
	}
	if err := store.Validate(ctx, takeover, takeoverAt); err != nil {
		t.Fatalf("new fenced owner did not validate: %v", err)
	}
}
