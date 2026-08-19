package privacy

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

const task6ErasurePauseLock int64 = 26081101

func TestTask6LegalHoldActivationSerializesWithInFlightErasure(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PRIVACY_RACE_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PRIVACY_RACE_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 7)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	makerID, submitterID, checkerID, executorID := ids[0], ids[1], ids[2], ids[3]
	contactID, caseID, holdID := ids[4], ids[5], ids[6]
	now := time.Now().UTC().Truncate(time.Microsecond)
	lookup := []byte("task6-legal-hold-race-" + contactID)
	for _, actor := range []string{makerID, submitterID, checkerID, executorID} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 privacy race','DISABLED',false)`, actor, "privacy-race-"+actor+"@internal.invalid"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),$2,'***5678','ACTIVE',$3)`, contactID, lookup, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO privacy_cases(id,case_type,status,subject_lookup_hmac,subject_masked,contact_id,requested_at,due_at,created_by,submitted_by,decided_by,request_reason,decision_reason,requested_changes,version,created_at,updated_at) VALUES($1::uuid,'ERASURE','APPROVED',$2,'***5678',$3::uuid,$4,$5,$6::uuid,$7::uuid,$8::uuid,'lawful erasure request','independent erasure approval','{}'::jsonb,3,$4,$4)`, caseID, lookup, contactID, now.Add(-time.Hour), now.Add(24*time.Hour), makerID, submitterID, checkerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO privacy_legal_holds(id,subject_lookup_hmac,contact_id,scope,status,reason,created_by,submitted_by,created_at,version) VALUES($1::uuid,$2,$3::uuid,'CONTACT','PENDING_APPROVAL','preserve subject evidence',$4::uuid,$5::uuid,$6,2)`, holdID, lookup, contactID, makerID, submitterID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE OR REPLACE FUNCTION task6_pause_erasure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(26081101); RETURN NULL; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER IF EXISTS task6_pause_erasure ON contact_attribute_values`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER task6_pause_erasure BEFORE DELETE ON contact_attribute_values FOR EACH STATEMENT EXECUTE FUNCTION task6_pause_erasure()`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS task6_pause_erasure ON contact_attribute_values`)
		_, _ = db.ExecContext(context.Background(), `DROP FUNCTION IF EXISTS task6_pause_erasure()`)
	}()

	lockConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lockConn.Close()
	var ignored any
	if err := lockConn.QueryRowContext(ctx, `SELECT pg_advisory_lock($1)`, task6ErasurePauseLock).Scan(&ignored); err != nil {
		t.Fatal(err)
	}
	repo := &PostgreSQLRepository{DB: db}
	current, err := repo.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	var erasureEventID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&erasureEventID); err != nil {
		t.Fatal(err)
	}
	erasureDone := make(chan error, 1)
	go func() {
		_, executeErr := repo.Execute(ctx, current, executorID, "execute approved erasure", nil, "", "", now, Event{
			ID: erasureEventID, CaseID: caseID, Type: "COMPLETED", ActorID: executorID,
			Reason: "execute approved erasure", CaseVersion: 4, Evidence: []byte(`{}`), OccurredAt: now,
		})
		erasureDone <- executeErr
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE 'DELETE FROM contact_attribute_values%')`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("erasure never reached the post-hold-check destructive boundary")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var holdEventID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&holdEventID); err != nil {
		t.Fatal(err)
	}
	holdDone := make(chan error, 1)
	go func() {
		_, holdErr := repo.DecideLegalHold(ctx, holdID, true, "approve preservation hold", checkerID, 2, now.Add(time.Second), LegalHoldEvent{
			ID: holdEventID, LegalHoldID: holdID, Type: "APPROVED", ActorID: checkerID,
			Reason: "approve preservation hold", HoldVersion: 3, Evidence: []byte(`{}`), OccurredAt: now.Add(time.Second),
		})
		holdDone <- holdErr
	}()

	holdCommittedBeforeRelease := false
	var holdErr error
	select {
	case holdErr = <-holdDone:
		holdCommittedBeforeRelease = true
	case <-time.After(250 * time.Millisecond):
	}
	if holdCommittedBeforeRelease && holdErr != nil {
		t.Fatalf("legal hold activation failed before erasure release: %v", holdErr)
	}

	var unlocked bool
	if err := lockConn.QueryRowContext(ctx, `SELECT pg_advisory_unlock($1)`, task6ErasurePauseLock).Scan(&unlocked); err != nil || !unlocked {
		t.Fatalf("release erasure test lock: unlocked=%v err=%v", unlocked, err)
	}
	erasureErr := <-erasureDone
	if !holdCommittedBeforeRelease {
		holdErr = <-holdDone
	}
	if holdErr != nil {
		t.Fatalf("legal hold activation failed: %v", holdErr)
	}
	var holdStatus, contactStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM privacy_legal_holds WHERE id=$1::uuid`, holdID).Scan(&holdStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM contacts WHERE id=$1::uuid`, contactID).Scan(&contactStatus); err != nil {
		t.Fatal(err)
	}
	if holdStatus != string(HoldActive) {
		t.Fatalf("legal hold status=%s, want ACTIVE", holdStatus)
	}
	if holdCommittedBeforeRelease && erasureErr == nil {
		t.Fatalf("active legal hold committed while erasure was paused, but erasure still committed; contact status=%s", contactStatus)
	}
}
