package retention

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestTask6RetentionExecutesOnlyUnheldContentAndRechecksHoldAfterClaim(t *testing.T) {
	dsn := os.Getenv("POSTGRES_RETENTION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_RETENTION_DATABASE_URL is not set")
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
	// This is a dedicated disposable Task-6 database. Remove pending retention
	// jobs left by earlier RED fixture attempts so ClaimJobs is deterministic.
	if _, err := db.ExecContext(ctx, `DELETE FROM retention_jobs`); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 10)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, purposeID, campaignID, contactID := ids[0], ids[1], ids[2], ids[3], ids[4]
	messageID, snapshotID, recipientID, policyID, freeReplyID := ids[5], ids[6], ids[7], ids[8], ids[9]
	var heldReplyID, raceReplyID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&heldReplyID, &raceReplyID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	cutoff := now.Add(-time.Minute)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 retention actor','DISABLED',false)`, actorID, "task6-retention-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Task 6 retention "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Task 6 retention','WHATSAPP','v1')`, purposeID, orgID, "T6RET_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Task 6 retention',$3::uuid,'COMPLETED',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***5678','ACTIVE',$3)`, contactID, "task6-retention-"+contactID, cutoff); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','retention',repeat('a',64),'APPROVED',$3)`, messageID, campaignID, "task6-retention-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1)`, snapshotID, campaignID, "task6-retention-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'DELIVERED',$7,$7)`, recipientID, campaignID, snapshotID, contactID, messageID, "task6-retention-"+recipientID, cutoff); err != nil {
		t.Fatal(err)
	}
	var encryptedReplyID, invalidEmptyReplyID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&encryptedReplyID, &invalidEmptyReplyID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO inbound_replies(id,event_id,recipient_id,contact_id,campaign_id,session_id,message_text,message_text_cipher,content_key_version,message_fingerprint,occurred_at,created_at,content_retain_until) VALUES($1::uuid,$2,$3::uuid,$4::uuid,$5::uuid,'task6-session','',decode('01','hex'),'v1',$6,$7,$7,$8)`, encryptedReplyID, "task6-encrypted-"+encryptedReplyID, recipientID, contactID, campaignID, "fp-"+encryptedReplyID, cutoff, now.Add(24*time.Hour)); err != nil {
		t.Fatalf("encrypted inbound content with empty legacy plaintext was rejected: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO inbound_replies(id,event_id,recipient_id,contact_id,campaign_id,session_id,message_text,message_fingerprint,occurred_at,created_at,content_retain_until) VALUES($1::uuid,$2,$3::uuid,$4::uuid,$5::uuid,'task6-session','',$6,$7,$7,$8)`, invalidEmptyReplyID, "task6-invalid-empty-"+invalidEmptyReplyID, recipientID, contactID, campaignID, "fp-"+invalidEmptyReplyID, cutoff, now.Add(24*time.Hour)); err == nil {
		t.Fatal("unencrypted unredacted empty inbound content was accepted")
	}
	for _, reply := range []struct {
		id      string
		eventID string
		hold    bool
		text    string
	}{{freeReplyID, "task6-free-" + freeReplyID, false, "free content"}, {heldReplyID, "task6-held-" + heldReplyID, true, "held content"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO inbound_replies(id,event_id,recipient_id,contact_id,campaign_id,session_id,message_text,message_fingerprint,occurred_at,created_at,content_retain_until,legal_hold,legal_hold_reason,legal_hold_applied_by,legal_hold_applied_at) VALUES($1::uuid,$2,$3::uuid,$4::uuid,$5::uuid,'task6-session',$6,$7,$8::timestamptz,$8::timestamptz,$9::timestamptz,$10::boolean,CASE WHEN $10::boolean THEN 'task 6 preservation hold' ELSE NULL END,CASE WHEN $10::boolean THEN $11::uuid ELSE NULL END,CASE WHEN $10::boolean THEN $8::timestamptz ELSE NULL END)`, reply.id, reply.eventID, recipientID, contactID, campaignID, reply.text, "fp-"+reply.id, cutoff.Add(-time.Hour), cutoff, reply.hold, actorID); err != nil {
			t.Fatal(err)
		}
	}
	effectiveFrom := cutoff.Add(-time.Hour)
	if _, err := db.ExecContext(ctx, `INSERT INTO retention_policies(id,name,object_type,action,scope_type,scope_id,retention_days,respect_legal_holds,status,effective_from,created_by,submitted_by,approved_by,reason,version,created_at,updated_at) VALUES($1::uuid,$2,'INBOUND_CONTENT','ANONYMISE','ORGANISATION',$5,30,true,'ACTIVE',$3,$4::uuid,$4::uuid,$4::uuid,'task 6 inbound retention',3,$3,$3)`, policyID, "task6-inbound-"+policyID, effectiveFrom, actorID, orgID); err != nil {
		t.Fatal(err)
	}
	policy := Policy{ID: policyID, ObjectType: ObjectInboundContent, Action: ActionAnonymise, ScopeType: ScopeOrganisation, ScopeID: orgID, RetentionDays: 30, RespectLegalHolds: true, Status: StatusActive, EffectiveFrom: effectiveFrom}
	store := &PostgreSQLStore{DB: db}
	scheduled, err := store.schedulePolicy(ctx, policy, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if scheduled != 1 {
		t.Fatalf("initial scheduled=%d want=1 unheld reply", scheduled)
	}
	jobs, err := store.ClaimJobs(ctx, "task6-retention-worker", now, time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ObjectID != freeReplyID {
		t.Fatalf("initial claimed jobs=%+v, want only unheld reply %s", jobs, freeReplyID)
	}
	executor := &PostgreSQLExecutor{DB: db}
	evidence, err := executor.Execute(ctx, jobs[0], now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteJob(ctx, jobs[0], evidence, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	var freeText, heldText string
	var freeRedacted, heldRedacted sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT message_text,content_redacted_at FROM inbound_replies WHERE id=$1::uuid`, freeReplyID).Scan(&freeText, &freeRedacted); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT message_text,content_redacted_at FROM inbound_replies WHERE id=$1::uuid`, heldReplyID).Scan(&heldText, &heldRedacted); err != nil {
		t.Fatal(err)
	}
	if freeText != "" || !freeRedacted.Valid || heldText != "held content" || heldRedacted.Valid {
		t.Fatalf("retention result free=(%q,%v) held=(%q,%v)", freeText, freeRedacted.Valid, heldText, heldRedacted.Valid)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO inbound_replies(id,event_id,recipient_id,contact_id,campaign_id,session_id,message_text,message_fingerprint,occurred_at,created_at,content_retain_until,legal_hold) VALUES($1::uuid,$2,$3::uuid,$4::uuid,$5::uuid,'task6-session','race content',$6,$7,$7,$8,false)`, raceReplyID, "task6-race-"+raceReplyID, recipientID, contactID, campaignID, "fp-"+raceReplyID, cutoff.Add(-time.Hour), cutoff); err != nil {
		t.Fatal(err)
	}
	scheduled, err = store.schedulePolicy(ctx, policy, now.Add(3*time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if scheduled != 1 {
		t.Fatalf("race scheduled=%d want=1", scheduled)
	}
	jobs, err = store.ClaimJobs(ctx, "task6-retention-worker", now.Add(3*time.Second), time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ObjectID != raceReplyID {
		t.Fatalf("race claimed jobs=%+v", jobs)
	}
	if _, err := db.ExecContext(ctx, `UPDATE inbound_replies SET legal_hold=true,legal_hold_reason='task 6 post-claim preservation hold',legal_hold_applied_by=$2::uuid,legal_hold_applied_at=$3 WHERE id=$1::uuid`, raceReplyID, actorID, now.Add(3500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(ctx, jobs[0], now.Add(4*time.Second))
	var review ReviewRequiredError
	if !errors.As(err, &review) {
		t.Fatalf("post-claim legal hold did not stop execution: %v", err)
	}
	if err := store.HoldJob(ctx, jobs[0], review.Reason, review.Evidence, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	var raceText, raceStatus string
	var raceRedacted sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT message_text,content_redacted_at FROM inbound_replies WHERE id=$1::uuid`, raceReplyID).Scan(&raceText, &raceRedacted); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM retention_jobs WHERE retention_policy_id=$1::uuid AND object_id=$2`, policyID, raceReplyID).Scan(&raceStatus); err != nil {
		t.Fatal(err)
	}
	if raceText != "race content" || raceRedacted.Valid || raceStatus != string(JobHeldReview) {
		t.Fatalf("post-claim hold evidence content=(%q,%v) job=%s", raceText, raceRedacted.Valid, raceStatus)
	}
}
