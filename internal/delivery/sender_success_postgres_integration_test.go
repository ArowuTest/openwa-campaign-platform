package delivery

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLDeliverySuccessAdvancesAssignedSenderHealth(t *testing.T) {
	dsn := os.Getenv("POSTGRES_SENDER_HEALTH_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_SENDER_HEALTH_DATABASE_URL is not set")
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
	var orgID, purposeID, contactID, campaignID, messageID, snapshotID, senderID, recipientID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(
		&orgID, &purposeID, &contactID, &campaignID, &messageID, &snapshotID, &senderID, &recipientID,
	); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM delivery_events WHERE campaign_recipient_id=$1::uuid`, recipientID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_metrics WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_recipients WHERE id=$1::uuid`, recipientID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM audience_snapshots WHERE id=$1::uuid`, snapshotID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM message_versions WHERE id=$1::uuid`, messageID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM contacts WHERE id=$1::uuid`, contactID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "sender-health-integration-"+orgID); err != nil {
		t.Fatalf("insert organisation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),decode(replace($1::text,'-',''),'hex'),'***0001','ACTIVE',now())`, contactID); err != nil {
		t.Fatalf("insert contact: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,'HEALTH_TEST','Health integration','WHATSAPP','v1')`, purposeID, orgID); err != nil {
		t.Fatalf("insert purpose: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Sender health integration',$3::uuid,'DISPATCHING',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatalf("insert campaign: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','health test',repeat('a',64),'APPROVED',$3)`, messageID, campaignID, "sender-health-"+messageID); err != nil {
		t.Fatalf("insert message: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'health-v1',$3,1)`, snapshotID, campaignID, "health-"+snapshotID); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_sessions(id,encrypted_msisdn,masked_msisdn,engine_type,status,safe_messages_per_minute,safe_daily_capacity,last_heartbeat_at) VALUES($1::uuid,decode('00','hex'),'***9001','WHATSAPP_WEB_JS','READY',10,100,now())`, senderID); err != nil {
		t.Fatalf("insert sender: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,assigned_session_id) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'GATEWAY_ACCEPTED',$7::uuid)`, recipientID, campaignID, snapshotID, contactID, messageID, "health-"+recipientID, senderID); err != nil {
		t.Fatalf("insert recipient: %v", err)
	}

	repo := &PostgreSQLRepository{DB: db}
	t1 := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	sent := Event{DeduplicationKey: "health-sent-" + recipientID, Type: EventSent, ProviderMessageID: "provider-" + recipientID, OccurredAt: t1}
	if _, changed, err := repo.ApplyEvent(ctx, recipientID, sent); err != nil || !changed {
		t.Fatalf("apply sent event changed=%v err=%v", changed, err)
	}
	assertSenderLastSuccess(t, ctx, db, senderID, t1)
	if _, changed, err := repo.ApplyEvent(ctx, recipientID, sent); err != nil || changed {
		t.Fatalf("exact replay changed=%v err=%v", changed, err)
	}
	assertSenderLastSuccess(t, ctx, db, senderID, t1)

	t0 := t1.Add(-time.Hour)
	delivered := Event{DeduplicationKey: "health-delivered-" + recipientID, Type: EventDelivered, ProviderMessageID: sent.ProviderMessageID, OccurredAt: t0}
	if _, changed, err := repo.ApplyEvent(ctx, recipientID, delivered); err != nil || !changed {
		t.Fatalf("apply older delivered event changed=%v err=%v", changed, err)
	}
	assertSenderLastSuccess(t, ctx, db, senderID, t1)

	t2 := t1.Add(time.Hour)
	read := Event{DeduplicationKey: "health-read-" + recipientID, Type: EventRead, ProviderMessageID: sent.ProviderMessageID, OccurredAt: t2}
	if _, changed, err := repo.ApplyEvent(ctx, recipientID, read); err != nil || !changed {
		t.Fatalf("apply newer read event changed=%v err=%v", changed, err)
	}
	assertSenderLastSuccess(t, ctx, db, senderID, t2)
}

func assertSenderLastSuccess(t *testing.T, ctx context.Context, db *sql.DB, senderID string, want time.Time) {
	t.Helper()
	var got sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT last_success_at FROM sender_sessions WHERE id=$1::uuid`, senderID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Valid || !got.Time.UTC().Equal(want.UTC()) {
		t.Fatalf("last_success_at=%v want=%v", got, want)
	}
}
