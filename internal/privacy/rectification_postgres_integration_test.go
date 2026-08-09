package privacy

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLRectificationPreservesAttributedProfileHistory(t *testing.T) {
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
	now := time.Date(2099, 10, 2, 12, 0, 0, 0, time.UTC)
	ids := make([]string, 5)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	creatorID, deciderID, executorID, contactID, caseID := ids[0], ids[1], ids[2], ids[3], ids[4]
	for _, actor := range []struct{ id, name string }{
		{creatorID, "Rectification Creator"}, {deciderID, "Rectification Decider"}, {executorID, "Rectification Executor"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,$3,'DISABLED',false)`, actor.id, "rectify-"+actor.id+"@internal.invalid", actor.name); err != nil {
			t.Fatal(err)
		}
	}
	lookup := []byte("rectification-" + contactID)
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,reported_age,age_recorded_at,age_source,age_verified,gender_code,status,source_system,source_record_id,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),$2,'+234******5678',27,$3::date,'SELF_DECLARED_IMPORT',false,'FEMALE','ACTIVE','CRM_A','row-42',$4)`, contactID, lookup, now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	changes := `{"reportedAge":31,"ageRecordedAt":"2099-10-01","ageSource":"VERIFIED_CORRECTION","ageVerified":true}`
	if _, err := db.ExecContext(ctx, `INSERT INTO privacy_cases(id,case_type,status,subject_lookup_hmac,subject_masked,contact_id,requested_at,due_at,created_by,decided_by,request_reason,decision_reason,requested_changes,version,created_at,updated_at) VALUES($1::uuid,'RECTIFICATION','APPROVED',$2,'+234******5678',$3::uuid,$4,$5,$6::uuid,$7::uuid,'correct verified profile','independent approval',$8::jsonb,3,$4,$4)`, caseID, lookup, contactID, now.Add(-2*time.Hour), now.Add(24*time.Hour), creatorID, deciderID, changes); err != nil {
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
	reason := "apply independently verified profile correction"
	updated, err := repo.Execute(ctx, current, executorID, reason, []byte{1, 2, 3}, "v1", strings.Repeat("a", 64), now, Event{
		ID: eventID, CaseID: caseID, Type: "COMPLETED", ActorID: executorID, Reason: reason, Evidence: []byte(`{}`), OccurredAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusCompleted || updated.ExecutedBy != executorID {
		t.Fatalf("unexpected completed case: %+v", updated)
	}
	var age int
	var ageSource string
	var ageVerified bool
	if err := db.QueryRowContext(ctx, `SELECT reported_age,coalesce(age_source,''),age_verified FROM contacts WHERE id=$1::uuid`, contactID).Scan(&age, &ageSource, &ageVerified); err != nil {
		t.Fatal(err)
	}
	if age != 31 || ageSource != "VERIFIED_CORRECTION" || !ageVerified {
		t.Fatalf("rectified profile age=%d source=%q verified=%v", age, ageSource, ageVerified)
	}
	var historicalAge int
	var historyCaseID, historyActor, historyReason, sourceSystem, sourceRecord string
	if err := db.QueryRowContext(ctx, `SELECT reported_age,privacy_case_id::text,actor_id::text,change_reason,coalesce(source_system,''),coalesce(source_record_id,'') FROM contact_profile_history WHERE contact_id=$1::uuid AND privacy_case_id=$2::uuid`, contactID, caseID).Scan(&historicalAge, &historyCaseID, &historyActor, &historyReason, &sourceSystem, &sourceRecord); err != nil {
		t.Fatal(err)
	}
	if historicalAge != 27 || historyCaseID != caseID || historyActor != executorID || historyReason != reason || sourceSystem != "CRM_A" || sourceRecord != "row-42" {
		t.Fatalf("history age=%d case=%q actor=%q reason=%q source=%q record=%q", historicalAge, historyCaseID, historyActor, historyReason, sourceSystem, sourceRecord)
	}
	var eventActor, eventReason string
	if err := db.QueryRowContext(ctx, `SELECT actor_id::text,coalesce(reason,'') FROM privacy_case_events WHERE id=$1::uuid`, eventID).Scan(&eventActor, &eventReason); err != nil {
		t.Fatal(err)
	}
	if eventActor != executorID || eventReason != reason {
		t.Fatalf("event actor=%q reason=%q", eventActor, eventReason)
	}
}
