package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestTask6AuditContentionReplayTamperAndAttribution(t *testing.T) {
	dsn := os.Getenv("POSTGRES_AUDIT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_AUDIT_DATABASE_URL is not set")
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
	var actorID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	repo := &PostgreSQLRepository{DB: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed, err := New(Input{
		ActorType: "USER", ActorID: actorID, Action: "TASK6_AUDIT_SEED", ObjectType: "TASK6", ObjectID: "audit-seed",
		Outcome: "SUCCESS", Sensitivity: "HIGH", CorrelationID: "task6-audit-seed-" + actorID, OccurredAt: now,
		Before: map[string]any{"version": 1}, After: map[string]any{"version": 2}, Reason: "task 6 audit replay evidence",
	})
	if err != nil {
		t.Fatal(err)
	}
	sequence, hash, err := repo.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Append(ctx, seed, sequence, hash)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.Append(ctx, seed, sequence, hash)
	if err != nil || replayed.ID != stored.ID || replayed.Sequence != stored.Sequence || replayed.Hash != stored.Hash {
		t.Fatalf("idempotent replay changed audit event: replay=%+v err=%v", replayed, err)
	}
	conflict := seed
	conflict.Action = "TASK6_AUDIT_CONFLICT"
	if _, err := repo.Append(ctx, conflict, sequence, hash); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay was not rejected: %v", err)
	}
	page, err := repo.Search(ctx, Query{ActorID: actorID, CorrelationID: seed.CorrelationID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ActorID != actorID || page.Items[0].CorrelationID != seed.CorrelationID {
		t.Fatalf("audit attribution lost: %+v", page.Items)
	}
	if _, err := db.ExecContext(ctx, `UPDATE audit_events SET action='TAMPERED' WHERE id=$1::uuid`, stored.ID); err == nil {
		t.Fatal("audit tamper UPDATE was accepted")
	}

	const writers = 12
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			recorder := NewRecorder(&PostgreSQLRepository{DB: db})
			_, recordErr := recorder.Record(ctx, Input{
				ActorType: "USER", ActorID: actorID, Action: "TASK6_AUDIT_CONCURRENT", ObjectType: "TASK6", ObjectID: fmt.Sprintf("writer-%02d", i),
				Outcome: "SUCCESS", Sensitivity: "HIGH", CorrelationID: fmt.Sprintf("task6-audit-concurrent-%02d-%s", i, actorID),
				OccurredAt: now.Add(time.Duration(i+1) * time.Microsecond),
			})
			errs <- recordErr
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent audit append failed: %v", err)
		}
	}
	if err := repo.Verify(ctx); err != nil {
		t.Fatalf("audit chain failed verification after contention: %v", err)
	}
	results, err := repo.Search(ctx, Query{Action: "TASK6_AUDIT_CONCURRENT", ActorID: actorID, Limit: writers + 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Items) != writers {
		t.Fatalf("concurrent audit events=%d want=%d", len(results.Items), writers)
	}
}
