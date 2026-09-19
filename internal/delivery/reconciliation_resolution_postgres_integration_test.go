package delivery

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLResolveReconciliationCanAuthoriseGovernedRetry(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DELIVERY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_DELIVERY_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	ids := make([]string, 8)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, purposeID, campaignID, contactID, snapshotID, messageID, recipientID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], ids[7]
	now := time.Now().UTC().Truncate(time.Microsecond)
	must := func(query string, values ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, values...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Retry approver','ACTIVE',true)`, actorID, "retry-"+actorID+"@internal.invalid")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "retry-"+orgID)
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Retry purpose','WHATSAPP','v1')`, purposeID, orgID, "RETRY_"+purposeID)
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status) VALUES($1::uuid,$2::uuid,'retry-test',$3::uuid,'DISPATCHING')`, campaignID, orgID, purposeID)
	must(`INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***6060','ACTIVE',$3)`, contactID, "retry-"+contactID, now)
	must(`INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1)`, snapshotID, campaignID, "retry-"+snapshotID)
	must(`INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','retry',$3,'APPROVED',$4)`, messageID, campaignID, "retry-"+messageID, "retry-"+messageID)
	must(`INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at,reconciliation_required,last_error_code,last_error_detail) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'UNKNOWN',$7,$7,true,'OUTCOME_UNKNOWN','ambiguous provider outcome')`, recipientID, campaignID, snapshotID, contactID, messageID, "retry-"+recipientID, now)

	t.Cleanup(func() {
		c := context.Background()
		_, _ = db.ExecContext(c, `DELETE FROM delivery_exception_resolutions WHERE campaign_recipient_id=$1::uuid`, recipientID)
		_, _ = db.ExecContext(c, `DELETE FROM campaign_recipients WHERE id=$1::uuid`, recipientID)
		_, _ = db.ExecContext(c, `DELETE FROM message_versions WHERE id=$1::uuid`, messageID)
		_, _ = db.ExecContext(c, `DELETE FROM audience_snapshots WHERE id=$1::uuid`, snapshotID)
		_, _ = db.ExecContext(c, `DELETE FROM contacts WHERE id=$1::uuid`, contactID)
		_, _ = db.ExecContext(c, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(c, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(c, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
		_, _ = db.ExecContext(c, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	})

	repository := &PostgreSQLRepository{DB: db}
	resolvedAt := now.Add(time.Second)
	if _, err := repository.ResolveReconciliation(ctx, recipientID, StatusUnknown, Status("BOGUS"), actorID, "CONFIRM_NOT_SUBMITTED", "evidence://invalid", "invalid transition must fail closed", resolvedAt); err == nil {
		t.Fatal("invalid reconciliation status was accepted")
	}
	var beforeStatus string
	var beforeRequired bool
	if err := db.QueryRowContext(ctx, `SELECT status,reconciliation_required FROM campaign_recipients WHERE id=$1::uuid`, recipientID).Scan(&beforeStatus, &beforeRequired); err != nil {
		t.Fatal(err)
	}
	if beforeStatus != "UNKNOWN" || !beforeRequired {
		t.Fatalf("invalid reconciliation mutated recipient status=%s reconciliation=%v", beforeStatus, beforeRequired)
	}
	value, err := repository.ResolveReconciliation(ctx, recipientID, StatusUnknown, StatusFailedRetryable, actorID, "CONFIRM_NOT_SUBMITTED", "evidence://retry", "provider confirms submission did not occur", resolvedAt)
	if err != nil {
		t.Fatal(err)
	}
	if value.Status != StatusFailedRetryable || value.ReconciliationRequired || value.LastErrorCode != "OPERATOR_RECONCILIATION" {
		t.Fatalf("resolved recipient=%+v", value)
	}
	var status, code, resultStatus string
	var required bool
	if err := db.QueryRowContext(ctx, `SELECT status,reconciliation_required,coalesce(last_error_code,'') FROM campaign_recipients WHERE id=$1::uuid`, recipientID).Scan(&status, &required, &code); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT result_status FROM delivery_exception_resolutions WHERE campaign_recipient_id=$1::uuid AND action='CONFIRM_NOT_SUBMITTED'`, recipientID).Scan(&resultStatus); err != nil {
		t.Fatal(err)
	}
	if status != "FAILED_RETRYABLE" || required || code != "OPERATOR_RECONCILIATION" || resultStatus != "FAILED_RETRYABLE" {
		t.Fatalf("persisted status=%s reconciliation=%v code=%s resultStatus=%s", status, required, code, resultStatus)
	}
	if _, err := repository.ResolveReconciliation(ctx, recipientID, StatusFailedRetryable, StatusFailedPermanent, actorID, "MARK_FAILED_PERMANENT", "evidence://second", "already resolved recipient must remain closed", resolvedAt.Add(time.Second)); err == nil {
		t.Fatal("recipient no longer awaiting reconciliation was resolved again")
	}
}
