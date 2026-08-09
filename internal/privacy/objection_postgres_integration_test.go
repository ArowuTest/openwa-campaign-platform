package privacy

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLPrivacyObjectionCreatesImmediateGlobalSuppression(t *testing.T) {
	dsn := os.Getenv("POSTGRES_CONTACT_RECTIFICATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_CONTACT_RECTIFICATION_DATABASE_URL is not set")
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

	ids := make([]string, 5)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	creatorID, deciderID, executorID, contactID, caseID := ids[0], ids[1], ids[2], ids[3], ids[4]
	for _, actor := range []struct{ id, name string }{{creatorID, "Objection Creator"}, {deciderID, "Objection Decider"}, {executorID, "Objection Executor"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,$3,'DISABLED',false)`, actor.id, "privacy-objection-"+actor.id+"@internal.invalid", actor.name); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM privacy_cases WHERE id=$1::uuid`, caseID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM suppressions WHERE contact_id=$1::uuid`, contactID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM contact_profile_history WHERE contact_id=$1::uuid`, contactID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM contacts WHERE id=$1::uuid`, contactID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id IN ($1::uuid,$2::uuid,$3::uuid)`, creatorID, deciderID, executorID)
	}()

	now := time.Now().UTC().Truncate(time.Microsecond)
	lookup := []byte("privacy-objection-" + contactID)
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),$2,'+234******7654','ACTIVE',$3)`, contactID, lookup, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO privacy_cases(id,case_type,status,subject_lookup_hmac,subject_masked,contact_id,requested_at,due_at,created_by,decided_by,request_reason,decision_reason,requested_changes,version,created_at,updated_at) VALUES($1::uuid,'OBJECTION','APPROVED',$2,'+234******7654',$3::uuid,$4,$5,$6::uuid,$7::uuid,'lawful objection request','independent approval','{}'::jsonb,3,$4,$4)`, caseID, lookup, contactID, now.Add(-2*time.Hour), now.Add(24*time.Hour), creatorID, deciderID); err != nil {
		t.Fatal(err)
	}
	var eventID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	repo := &PostgreSQLRepository{DB: db}
	current, err := repo.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	reason := "execute approved objection and suppress future processing"
	completed, err := repo.Execute(ctx, current, executorID, reason, nil, "", "", now, Event{ID: eventID, CaseID: caseID, Type: "COMPLETED", ActorID: executorID, Reason: reason, Evidence: []byte(`{}`), OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusCompleted {
		t.Fatalf("case=%+v", completed)
	}

	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM contacts WHERE id=$1::uuid`, contactID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "SUPPRESSED" {
		t.Fatalf("contact status=%q, want SUPPRESSED", status)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM suppressions WHERE contact_id=$1::uuid AND msisdn_lookup_hmac=$2 AND channel='WHATSAPP' AND scope='GLOBAL' AND active=true AND effective_at<=$3`, contactID, lookup, now).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("active global WhatsApp suppressions=%d, want 1", count)
	}
}
