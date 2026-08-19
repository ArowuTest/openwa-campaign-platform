package delivery

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestTask6CancelledCampaignCannotCrossSubmissionBoundary(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TASK6_CANCELLATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TASK6_CANCELLATION_DATABASE_URL is not set")
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

	var orgID, purposeID, contactID, campaignID, messageID, snapshotID, recipientID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&orgID, &purposeID, &contactID, &campaignID, &messageID, &snapshotID, &recipientID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Task6 Cancel "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***8000','ACTIVE',now())`, contactID, "cancel-"+contactID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Task6 Cancel','WHATSAPP','v1')`, purposeID, orgID, "CAN_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Task6 Cancel',$3::uuid,'DISPATCHING',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','cancel',repeat('c',64),'APPROVED',$3)`, messageID, campaignID, "cancel-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1)`, snapshotID, campaignID, "cancel-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	key, err := NewIdempotencyKey(campaignID, contactID, messageID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'CLAIMED',$7,$7)`, recipientID, campaignID, snapshotID, contactID, messageID, key, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET status='CANCELLED' WHERE id=$1::uuid`, campaignID); err != nil {
		t.Fatal(err)
	}

	repo := &PostgreSQLRepository{DB: db}
	event := Event{DeduplicationKey: "task6-cancel-submit-" + recipientID, Type: EventSubmitting, OccurredAt: now.Add(time.Second)}
	if _, _, err := repo.ApplyEvent(ctx, recipientID, event); err == nil {
		t.Fatal("cancelled campaign crossed the SUBMITTING boundary")
	}
	var status string
	var attempts, events int
	if err := db.QueryRowContext(ctx, `SELECT status,attempt_count FROM campaign_recipients WHERE id=$1::uuid`, recipientID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM delivery_events WHERE event_deduplication_key=$1`, event.DeduplicationKey).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if status != "CLAIMED" || attempts != 0 || events != 0 {
		t.Fatalf("cancelled submission mutated recipient status=%s attempts=%d events=%d", status, attempts, events)
	}
}
