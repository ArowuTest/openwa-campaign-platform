package operations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/delivery"
	_ "campaign-platform/internal/persistence/database"
)

func task6OperationsAuditDB(t *testing.T) (*sql.DB, context.Context, context.CancelFunc) {
	t.Helper()
	dsn := os.Getenv("POSTGRES_OPERATIONS_AUDIT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_OPERATIONS_AUDIT_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		cancel()
		t.Fatal(err)
	}
	return db, ctx, cancel
}
func TestTask6DownloadAuthorizationCannotCommitWithoutDurableAuditEvidence(t *testing.T) {
	db, ctx, cancel := task6OperationsAuditDB(t)
	defer db.Close()
	defer cancel()
	var maker, actor, exportID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&maker, &actor, &exportID); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ id, email string }{{maker, "task6-auth-maker-" + maker + "@internal.invalid"}, {actor, "task6-auth-actor-" + actor + "@internal.invalid"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task6 audit actor','DISABLED',false)`, v.id, v.email); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `INSERT INTO export_requests(id,kind,format,status,requested_by,reason,expires_at,object_key,content_type,sha256,size_bytes,generated_at,created_at,updated_at) VALUES($1::uuid,'AUDIT_LOG','JSON','READY',$2::uuid,'task 6 governed export',$3,'task6/object','application/json',repeat('a',64),128,$4,$4,$4)`, exportID, maker, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	requestID := "task6-download-authorise-" + exportID
	svc := &Service{Repo: &PostgreSQLRepository{DB: db}, Audit: audit.NewRecorder(task6FailingAuditRepository{}), Clock: func() time.Time { return now }}
	_, callErr := svc.AuthorizeDownload(ctx, exportID, actor, requestID, 5*time.Minute)
	var grants, durable int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM export_download_grants WHERE export_id=$1::uuid`, exportID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM transactional_outbox WHERE aggregate_type='AUDIT' AND payload->>'correlationId'=$1`, requestID).Scan(&durable); err != nil {
		t.Fatal(err)
	}
	if grants == 1 && durable == 0 {
		t.Fatalf("download grant committed without durable audit evidence: callErr=%v", callErr)
	}
}
func TestTask6DownloadConsumptionCannotCommitWithoutDurableAuditEvidence(t *testing.T) {
	db, ctx, cancel := task6OperationsAuditDB(t)
	defer db.Close()
	defer cancel()
	var maker, actor, exportID, grantID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&maker, &actor, &exportID, &grantID); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ id, email string }{{maker, "task6-use-maker-" + maker + "@internal.invalid"}, {actor, "task6-use-actor-" + actor + "@internal.invalid"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task6 audit actor','DISABLED',false)`, v.id, v.email); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	token := "task6-consume-token-" + grantID
	digest := sha256.Sum256([]byte(token))
	if _, err := db.ExecContext(ctx, `INSERT INTO export_requests(id,kind,format,status,requested_by,reason,expires_at,object_key,content_type,sha256,size_bytes,generated_at,created_at,updated_at) VALUES($1::uuid,'AUDIT_LOG','JSON','READY',$2::uuid,'task 6 governed export',$3,'task6/object','application/json',repeat('b',64),256,$4,$4,$4)`, exportID, maker, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO export_download_grants(id,export_id,actor_id,token_hash,request_id,expires_at,created_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7)`, grantID, exportID, actor, hex.EncodeToString(digest[:]), "task6-grant-"+grantID, now.Add(5*time.Minute), now); err != nil {
		t.Fatal(err)
	}
	requestID := "task6-download-consume-" + exportID
	svc := &Service{Repo: &PostgreSQLRepository{DB: db}, Audit: audit.NewRecorder(task6FailingAuditRepository{}), Clock: func() time.Time { return now.Add(time.Second) }}
	_, _, callErr := svc.ConsumeDownload(ctx, exportID, token, actor, requestID)
	var used bool
	var downloads, durable int
	if err := db.QueryRowContext(ctx, `SELECT used_at IS NOT NULL FROM export_download_grants WHERE id=$1::uuid`, grantID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT download_count FROM export_requests WHERE id=$1::uuid`, exportID).Scan(&downloads); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM transactional_outbox WHERE aggregate_type='AUDIT' AND payload->>'correlationId'=$1`, requestID).Scan(&durable); err != nil {
		t.Fatal(err)
	}
	if used && downloads == 1 && durable == 0 {
		t.Fatalf("download consumption committed without durable audit evidence: callErr=%v", callErr)
	}
}
func TestTask6DeliveryResolutionCannotCommitWithoutDurableAuditEvidence(t *testing.T) {
	db, ctx, cancel := task6OperationsAuditDB(t)
	defer db.Close()
	defer cancel()
	ids := make([]string, 8)
	args := make([]any, 8)
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actor, orgID, purposeID, contactID, campaignID, messageID, snapshotID, recipientID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], ids[7]
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task6 reconciler','DISABLED',false)`, actor, "task6-reconcile-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Task6 reconcile "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***7000','ACTIVE',now())`, contactID, "reconcile-"+contactID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Task6 reconcile','WHATSAPP','v1')`, purposeID, orgID, "REC_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Task6 reconcile',$3::uuid,'DISPATCHING',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','reconcile',repeat('d',64),'APPROVED',$3)`, messageID, campaignID, "reconcile-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1)`, snapshotID, campaignID, "reconcile-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at,reconciliation_required,last_error_detail) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'UNKNOWN',$7,$7,true,'ambiguous provider outcome')`, recipientID, campaignID, snapshotID, contactID, messageID, "reconcile-"+recipientID, now); err != nil {
		t.Fatal(err)
	}
	requestID := "task6-delivery-resolution-" + recipientID
	svc := &Service{Repo: &PostgreSQLRepository{DB: db}, Deliveries: delivery.NewService(&delivery.PostgreSQLRepository{DB: db}), Audit: audit.NewRecorder(task6FailingAuditRepository{}), Clock: func() time.Time { return now.Add(time.Second) }}
	_, callErr := svc.ResolveDeliveryException(ctx, recipientID, ResolutionConfirmNotSubmitted, "evidence://task6", "confirmed provider did not accept submission", actor, requestID)
	var resolutions, durable int
	var reconciliation bool
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status,reconciliation_required FROM campaign_recipients WHERE id=$1::uuid`, recipientID).Scan(&status, &reconciliation); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM delivery_exception_resolutions WHERE campaign_recipient_id=$1::uuid`, recipientID).Scan(&resolutions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM transactional_outbox WHERE aggregate_type='AUDIT' AND payload->>'correlationId'=$1`, requestID).Scan(&durable); err != nil {
		t.Fatal(err)
	}
	if resolutions == 1 && !reconciliation && durable == 0 {
		t.Fatalf("delivery reconciliation committed without durable audit evidence: status=%s callErr=%v", status, callErr)
	}
}
