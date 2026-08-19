package delivery

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLDeliveryEventUpdatesMetricsIncrementallyAndReplayIsIdempotent(t *testing.T) {
	dsn := os.Getenv("POSTGRES_METRICS_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_METRICS_DATABASE_URL is not set")
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
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Metrics Idempotency "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***8018','ACTIVE',now())`, contactID, "metrics-"+contactID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Metrics','WHATSAPP','v1')`, purposeID, orgID, "MET_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Metrics',$3::uuid,'DISPATCHING',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','metrics',repeat('f',64),'APPROVED',$3)`, messageID, campaignID, "metrics-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1)`, snapshotID, campaignID, "metrics-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	key, err := NewIdempotencyKey(campaignID, contactID, messageID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 15, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'QUEUED',$7,$7)`, recipientID, campaignID, snapshotID, contactID, messageID, key, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_metrics(campaign_id,queued_total,updated_at) VALUES($1::uuid,1,$2)`, campaignID, now); err != nil {
		t.Fatal(err)
	}

	repository := &PostgreSQLRepository{DB: db}
	event := Event{DeduplicationKey: "metrics-submit-" + recipientID, Type: EventSubmitting, OccurredAt: now.Add(time.Second)}
	first, changed, err := repository.ApplyEvent(ctx, recipientID, event)
	if err != nil || !changed || first.Status != StatusSubmitting {
		t.Fatalf("first apply recipient=%+v changed=%v err=%v", first, changed, err)
	}
	var queued, submitted int64
	if err := db.QueryRowContext(ctx, `SELECT queued_total,submitted_total FROM campaign_metrics WHERE campaign_id=$1::uuid`, campaignID).Scan(&queued, &submitted); err != nil {
		t.Fatal(err)
	}
	if queued != 0 || submitted != 1 {
		t.Fatalf("incremental metrics queued=%d submitted=%d", queued, submitted)
	}

	second, changed, err := repository.ApplyEvent(ctx, recipientID, event)
	if err != nil || changed || second.Status != StatusSubmitting {
		t.Fatalf("replay recipient=%+v changed=%v err=%v", second, changed, err)
	}
	var replayQueued, replaySubmitted int64
	var eventCount int
	if err := db.QueryRowContext(ctx, `SELECT queued_total,submitted_total FROM campaign_metrics WHERE campaign_id=$1::uuid`, campaignID).Scan(&replayQueued, &replaySubmitted); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM delivery_events WHERE event_deduplication_key=$1`, event.DeduplicationKey).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if replayQueued != queued || replaySubmitted != submitted || eventCount != 1 {
		t.Fatalf("replay changed metrics/events: before=(%d,%d) after=(%d,%d) events=%d", queued, submitted, replayQueued, replaySubmitted, eventCount)
	}

	conflict := event
	conflict.Type = EventDelivered
	conflict.ProviderMessageID = "different-provider-message"
	_, changed, err = repository.ApplyEvent(ctx, recipientID, conflict)
	if !errors.Is(err, ErrEventDedupMismatch) || changed {
		t.Fatalf("mismatched replay changed=%v err=%v", changed, err)
	}
	var status string
	if err = db.QueryRowContext(ctx,
		`SELECT status FROM campaign_recipients WHERE id=$1::uuid`,
		recipientID,
	).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx,
		`SELECT queued_total,submitted_total FROM campaign_metrics WHERE campaign_id=$1::uuid`,
		campaignID,
	).Scan(&replayQueued, &replaySubmitted); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx,
		`SELECT count(*) FROM delivery_events WHERE event_deduplication_key=$1`,
		event.DeduplicationKey,
	).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if status != "SUBMITTING" || replayQueued != queued ||
		replaySubmitted != submitted || eventCount != 1 {
		t.Fatalf("mismatched replay mutated status=%s metrics=(%d,%d) events=%d",
			status, replayQueued, replaySubmitted, eventCount)
	}
}
