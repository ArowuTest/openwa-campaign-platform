package privacy

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLErasureRetainsMinimisedSuppressionIdentity(t *testing.T) {
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
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	ids := make([]string, 5)
	args := make([]any, 5)
	for i := range ids {
		args[i] = &ids[i]
	}
	if err = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	creator, decider, executor, contactID, caseID := ids[0], ids[1], ids[2], ids[3], ids[4]
	for _, actor := range []struct{ id, name string }{{creator, "Erasure Creator"}, {decider, "Erasure Decider"}, {executor, "Erasure Executor"}} {
		if _, err = db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,$3,'DISABLED',false)`, actor.id, "erase-"+actor.id+"@internal.invalid", actor.name); err != nil {
			t.Fatal(err)
		}
	}
	lookup := []byte("erasure-" + contactID)
	if _, err = db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,reported_age,age_recorded_at,age_source,age_verified,gender_code,status,profile_recorded_at) VALUES($1::uuid,$2,$3,'+234******7654',32,$4::date,'SELF_DECLARED_IMPORT',false,'MALE','ACTIVE',$4)`, contactID, []byte("original-ciphertext"), lookup, now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO privacy_cases(id,case_type,status,subject_lookup_hmac,subject_masked,contact_id,requested_at,due_at,created_by,decided_by,request_reason,decision_reason,requested_changes,version,created_at,updated_at) VALUES($1::uuid,'ERASURE','APPROVED',$2,'+234******7654',$3::uuid,$4,$5,$6::uuid,$7::uuid,'lawful erasure request','independent approval','{}'::jsonb,3,$4,$4)`, caseID, lookup, contactID, now.Add(-2*time.Hour), now.Add(24*time.Hour), creator, decider); err != nil {
		t.Fatal(err)
	}
	var eventID string
	if err = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	repo := &PostgreSQLRepository{DB: db}
	current, err := repo.Get(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	reason := "execute lawful erasure while retaining suppression identity"
	updated, err := repo.Execute(ctx, current, executor, reason, []byte{4, 5, 6}, "v1", strings.Repeat("b", 64), now, Event{ID: eventID, CaseID: caseID, Type: "COMPLETED", ActorID: executor, Reason: reason, Evidence: []byte(`{}`), OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusCompleted {
		t.Fatalf("case=%+v", updated)
	}
	var storedLookup []byte
	var masked, status string
	var restricted bool
	var age sql.NullInt64
	var encrypted []byte
	if err = db.QueryRowContext(ctx, `SELECT msisdn_lookup_hmac,encrypted_msisdn,masked_msisdn,status,processing_restricted,reported_age FROM contacts WHERE id=$1::uuid`, contactID).Scan(&storedLookup, &encrypted, &masked, &status, &restricted, &age); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedLookup, lookup) || masked != "ANONYMISED" || status != "ANONYMISED" || !restricted || age.Valid {
		t.Fatalf("erased contact lookup=%x masked=%q status=%q restricted=%v age=%v", storedLookup, masked, status, restricted, age)
	}
	if bytes.Equal(encrypted, []byte("original-ciphertext")) {
		t.Fatal("erasure retained original encrypted MSISDN payload")
	}
	var suppressionCount int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM suppressions WHERE contact_id=$1::uuid AND msisdn_lookup_hmac=$2 AND scope='GLOBAL' AND active=true`, contactID, lookup).Scan(&suppressionCount); err != nil {
		t.Fatal(err)
	}
	if suppressionCount != 1 {
		t.Fatalf("active minimised suppressions=%d want=1", suppressionCount)
	}
	duplicateID := caseID
	if _, err = db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),$2,'***','ACTIVE',$3)`, duplicateID, lookup, now); err == nil {
		t.Fatal("duplicate canonical contact accepted after erasure")
	}
}
