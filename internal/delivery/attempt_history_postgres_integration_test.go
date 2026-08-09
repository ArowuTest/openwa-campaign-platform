package delivery

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLDeliveryPreservesEveryRetryAttemptEvent(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DELIVERY_HISTORY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_DELIVERY_HISTORY_DATABASE_URL is not set")
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
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Attempt history "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***8012','ACTIVE',now())`, contactID, "attempt-"+contactID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Attempt history','WHATSAPP','v1')`, purposeID, orgID, "ATT_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Attempt history',$3::uuid,'DISPATCHING',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','attempt history',repeat('e',64),'APPROVED',$3)`, messageID, campaignID, "attempt-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1)`, snapshotID, campaignID, "attempt-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	key, err := NewIdempotencyKey(campaignID, contactID, messageID)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 8, 11, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'CLAIMED',$7,$7)`, recipientID, campaignID, snapshotID, contactID, messageID, key, base); err != nil {
		t.Fatal(err)
	}

	repo := &PostgreSQLRepository{DB: db}
	events := []Event{
		{DeduplicationKey: "attempt-1-submit-" + recipientID, Type: EventSubmitting, OccurredAt: base.Add(time.Second)},
		{DeduplicationKey: "attempt-1-fail-" + recipientID, Type: EventFailedRetryable, ErrorCode: "CONNECT_FAILED", OccurredAt: base.Add(2 * time.Second)},
		{DeduplicationKey: "attempt-2-queue-" + recipientID, Type: EventQueued, OccurredAt: base.Add(3 * time.Second)},
		{DeduplicationKey: "attempt-2-claim-" + recipientID, Type: EventClaimed, OccurredAt: base.Add(4 * time.Second)},
		{DeduplicationKey: "attempt-2-submit-" + recipientID, Type: EventSubmitting, OccurredAt: base.Add(5 * time.Second)},
		{DeduplicationKey: "attempt-2-accept-" + recipientID, Type: EventGatewayAccepted, ProviderMessageID: "provider-" + recipientID, OccurredAt: base.Add(6 * time.Second)},
	}
	for _, event := range events {
		if _, changed, err := repo.ApplyEvent(ctx, recipientID, event); err != nil || !changed {
			t.Fatalf("event=%s changed=%v err=%v", event.Type, changed, err)
		}
	}
	stored, err := repo.Get(ctx, recipientID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AttemptCount != 2 || stored.Status != StatusGatewayAccepted {
		t.Fatalf("recipient=%+v", stored)
	}
	rows, err := db.QueryContext(ctx, `SELECT event_type FROM delivery_events WHERE campaign_recipient_id=$1::uuid ORDER BY occurred_at,id`, recipientID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		got = append(got, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(events) {
		t.Fatalf("delivery history=%v want %d events", got, len(events))
	}
	for i, event := range events {
		if got[i] != string(event.Type) {
			t.Fatalf("history[%d]=%s want=%s", i, got[i], event.Type)
		}
	}
}
