package metacloud

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLConversationWindowRetentionReplayCannotRestoreAuthority(t *testing.T) {
	dsn := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_META_CLOUD_DATABASE_URL is not set")
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
	var actorID, orgID, poolID, senderID, contactID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actorID, &orgID, &poolID, &senderID, &contactID); err != nil {
		t.Fatal(err)
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Retention replay actor','ACTIVE',false)`, actorID, "meta-retained-"+actorID[:8]+"@example.test")
	mustExec(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Retention Replay "+orgID[:8])
	mustExec(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',10,100)`, poolID, "meta-retained-"+poolID[:8], orgID)
	mustExec(`INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,version,created_by,approved_by,reason) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,'Retention Replay','+234 ***','meta-retained-key','v23.0','ACTIVE','HEALTHY',now(),now()-interval '1 hour',1,$6::uuid,$6::uuid,'approved sender')`, senderID, orgID, poolID, "waba-"+orgID[:8], "phone-"+orgID[:8], actorID)
	mustExec(`INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,'x'::bytea,$2::bytea,'+234******7777','ACTIVE',now())`, contactID, []byte("retained-lookup-"+contactID))
	defer func() {
		cleanup := context.Background()
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_conversation_windows WHERE meta_sender_id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM contacts WHERE id=$1::uuid`, contactID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_senders WHERE id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	store := &PostgreSQLConversationWindowStore{DB: db}
	window := 24 * time.Hour
	base := time.Date(2026, 8, 15, 12, 0, 0, 123456789, time.UTC)
	retained := ConversationWindowObservation{
		MetaSenderID: senderID, ContactID: contactID,
		ProviderMessageID: "wamid.retained.replay", OccurredAt: base,
	}
	if _, changed, err := store.ObserveInbound(ctx, retained, window); err != nil || !changed {
		t.Fatalf("initial retained observation changed=%v err=%v", changed, err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM meta_cloud_conversation_windows WHERE meta_sender_id=$1::uuid AND source_provider_message_id=$2`, senderID, retained.ProviderMessageID); err != nil {
		t.Fatalf("retention delete: %v", err)
	}
	var tombstones int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM meta_cloud_conversation_window_tombstones WHERE meta_sender_id=$1::uuid AND source_provider_message_id=$2`, senderID, retained.ProviderMessageID).Scan(&tombstones); err != nil {
		t.Fatal(err)
	}
	if tombstones != 1 {
		t.Fatalf("retention delete did not create tombstone: %d", tombstones)
	}

	deleteTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, deleteErr := deleteTx.ExecContext(ctx, `DELETE FROM meta_cloud_conversation_window_tombstones WHERE meta_sender_id=$1::uuid AND source_provider_message_id=$2`, senderID, retained.ProviderMessageID)
	_ = deleteTx.Rollback()
	tombstoneDeleteAccepted := deleteErr == nil
	updateTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, updateErr := updateTx.ExecContext(ctx, `UPDATE meta_cloud_conversation_window_tombstones SET deleted_at=deleted_at+interval '1 second' WHERE meta_sender_id=$1::uuid AND source_provider_message_id=$2`, senderID, retained.ProviderMessageID)
	_ = updateTx.Rollback()
	tombstoneUpdateAccepted := updateErr == nil

	mixedCaseReplay := retained
	mixedCaseReplay.MetaSenderID = strings.ToUpper(retained.MetaSenderID)
	mixedCaseReplay.ContactID = strings.ToUpper(retained.ContactID)
	replayed, replayChanged, replayErr := store.ObserveInbound(ctx, mixedCaseReplay, window)
	var retainedLive int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM meta_cloud_conversation_windows WHERE meta_sender_id=$1::uuid AND source_provider_message_id=$2`, senderID, retained.ProviderMessageID).Scan(&retainedLive); err != nil {
		t.Fatal(err)
	}

	quarantined := ConversationWindowObservation{
		MetaSenderID: senderID, ContactID: contactID,
		ProviderMessageID: "wamid.quarantined.replay", OccurredAt: base.Add(time.Hour),
	}
	mustExec(`INSERT INTO meta_cloud_conversation_window_quarantine(meta_sender_id,contact_id,source_provider_message_id,inbound_occurred_at,eligible_until,recorded_at,reason) VALUES($1::uuid,$2::uuid,$3,$4,$5,$4,'ELIGIBILITY_WINDOW_EXCEEDS_MAXIMUM')`, senderID, contactID, quarantined.ProviderMessageID, quarantined.OccurredAt, quarantined.OccurredAt.Add(25*time.Hour))
	quarantineDeleteTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, quarantineDeleteErr := quarantineDeleteTx.ExecContext(ctx, `DELETE FROM meta_cloud_conversation_window_quarantine WHERE meta_sender_id=$1::uuid AND source_provider_message_id=$2`, senderID, quarantined.ProviderMessageID)
	_ = quarantineDeleteTx.Rollback()
	quarantineDeleteAccepted := quarantineDeleteErr == nil
	quarantineUpdateTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, quarantineUpdateErr := quarantineUpdateTx.ExecContext(ctx, `UPDATE meta_cloud_conversation_window_quarantine SET reason='REWRITTEN' WHERE meta_sender_id=$1::uuid AND source_provider_message_id=$2`, senderID, quarantined.ProviderMessageID)
	_ = quarantineUpdateTx.Rollback()
	quarantineUpdateAccepted := quarantineUpdateErr == nil

	_, quarantineChanged, quarantineReplayErr := store.ObserveInbound(ctx, quarantined, window)
	var quarantineLive int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM meta_cloud_conversation_windows WHERE meta_sender_id=$1::uuid AND source_provider_message_id=$2`, senderID, quarantined.ProviderMessageID).Scan(&quarantineLive); err != nil {
		t.Fatal(err)
	}

	futureObservation := ConversationWindowObservation{
		MetaSenderID: senderID, ContactID: contactID,
		ProviderMessageID: "wamid.future.clock-skew", OccurredAt: time.Now().UTC().Add(time.Hour),
	}
	if _, _, futureErr := store.ObserveInbound(ctx, futureObservation, window); futureErr == nil {
		t.Fatal("far-future provider timestamp created live conversation-window authority")
	}

	conflict := retained
	conflict.OccurredAt = retained.OccurredAt.Add(time.Minute)
	_, _, conflictErr := store.ObserveInbound(ctx, conflict, window)
	if tombstoneDeleteAccepted || tombstoneUpdateAccepted || quarantineDeleteAccepted || quarantineUpdateAccepted ||
		replayErr != nil || replayChanged || retainedLive != 0 ||
		quarantineReplayErr != nil || quarantineChanged || quarantineLive != 0 ||
		!errors.Is(conflictErr, ErrConversationWindowConflict) {
		t.Fatalf("retention replay boundary unsafe: tombstoneDelete=%v tombstoneUpdate=%v quarantineDelete=%v quarantineUpdate=%v replay=%#v replayChanged=%v replayErr=%v retainedLive=%d quarantineChanged=%v quarantineReplayErr=%v quarantineLive=%d conflictErr=%v",
			tombstoneDeleteAccepted, tombstoneUpdateAccepted, quarantineDeleteAccepted, quarantineUpdateAccepted,
			replayed, replayChanged, replayErr, retainedLive, quarantineChanged, quarantineReplayErr, quarantineLive, conflictErr)
	}
}
