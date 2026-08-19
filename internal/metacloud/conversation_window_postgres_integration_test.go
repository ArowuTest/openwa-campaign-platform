package metacloud

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLConversationWindowEvidenceIsExplicitReplaySafeAndMonotonic(t *testing.T) {
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
	mustExec(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Conversation window actor','ACTIVE',false)`, actorID, "meta-window-"+actorID[:8]+"@example.test")
	mustExec(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Conversation Window "+orgID[:8])
	mustExec(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',10,100)`, poolID, "meta-window-"+poolID[:8], orgID)
	mustExec(`INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,version,created_by,approved_by,reason) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,'Conversation Window','+234 ***','meta-window-key','v23.0','ACTIVE','HEALTHY',now(),now()-interval '1 hour',1,$6::uuid,$6::uuid,'approved sender')`, senderID, orgID, poolID, "waba-"+orgID[:8], "phone-"+orgID[:8], actorID)
	mustExec(`INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,'x'::bytea,$2::bytea,'+234******5678','ACTIVE',now())`, contactID, []byte("window-lookup-"+contactID))
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
	if _, ok, err := store.Current(ctx, senderID, contactID); err != nil || ok {
		t.Fatalf("window inferred without authenticated inbound: ok=%v err=%v", ok, err)
	}
	window := 24 * time.Hour
	base := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	first := ConversationWindowObservation{MetaSenderID: senderID, ContactID: contactID, ProviderMessageID: "wamid.window.1", OccurredAt: base}
	current, changed, err := store.ObserveInbound(ctx, first, window)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || current.ProviderMessageID != first.ProviderMessageID || !current.EligibleUntil.Equal(base.Add(window)) {
		t.Fatalf("first observation=%#v changed=%v", current, changed)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta_cloud_conversation_windows SET eligible_until=eligible_until+interval '1 minute' WHERE meta_sender_id=$1::uuid AND source_provider_message_id=$2`, senderID, first.ProviderMessageID); err == nil {
		t.Fatal("retained Meta conversation-window evidence was mutable")
	}
	deleteTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = deleteTx.ExecContext(ctx, `DELETE FROM meta_cloud_conversation_windows WHERE meta_sender_id=$1::uuid AND source_provider_message_id=$2`, senderID, first.ProviderMessageID); err != nil {
		_ = deleteTx.Rollback()
		t.Fatalf("retention delete itself should remain possible: %v", err)
	}
	rewriteAccepted := false
	if _, err = deleteTx.ExecContext(ctx, `INSERT INTO meta_cloud_conversation_windows(meta_sender_id,contact_id,source_provider_message_id,inbound_occurred_at,eligible_until) VALUES($1::uuid,$2::uuid,$3,$4,$5)`, senderID, contactID, first.ProviderMessageID, base.Add(time.Minute), base.Add(23*time.Hour)); err == nil {
		rewriteAccepted = true
	}
	_ = deleteTx.Rollback()

	boundTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	oversizedAccepted := false
	if _, err = boundTx.ExecContext(ctx, `INSERT INTO meta_cloud_conversation_windows(meta_sender_id,contact_id,source_provider_message_id,inbound_occurred_at,eligible_until) VALUES($1::uuid,$2::uuid,'wamid.window.oversized',$3,$4)`, senderID, contactID, base, base.Add(25*time.Hour)); err == nil {
		oversizedAccepted = true
	}
	_ = boundTx.Rollback()
	if rewriteAccepted || oversizedAccepted {
		t.Fatalf("conversation-window durable authority too permissive: rewriteAccepted=%v oversizedAccepted=%v", rewriteAccepted, oversizedAccepted)
	}
	replayed, changed, err := store.ObserveInbound(ctx, first, window)
	if err != nil {
		t.Fatal(err)
	}
	if changed || replayed.ProviderMessageID != first.ProviderMessageID || !replayed.EligibleUntil.Equal(current.EligibleUntil) {
		t.Fatalf("exact replay changed evidence: %#v changed=%v", replayed, changed)
	}

	newer := ConversationWindowObservation{MetaSenderID: senderID, ContactID: contactID, ProviderMessageID: "wamid.window.2", OccurredAt: base.Add(2 * time.Hour)}
	current, changed, err = store.ObserveInbound(ctx, newer, window)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || current.ProviderMessageID != newer.ProviderMessageID || !current.EligibleUntil.Equal(newer.OccurredAt.Add(window)) {
		t.Fatalf("newer observation did not advance current evidence: %#v changed=%v", current, changed)
	}
	older := ConversationWindowObservation{MetaSenderID: senderID, ContactID: contactID, ProviderMessageID: "wamid.window.old", OccurredAt: base.Add(time.Hour)}
	current, changed, err = store.ObserveInbound(ctx, older, window)
	if err != nil {
		t.Fatal(err)
	}
	if changed || current.ProviderMessageID != newer.ProviderMessageID {
		t.Fatalf("older observation replaced current evidence: %#v changed=%v", current, changed)
	}

	conflicting := older
	conflicting.OccurredAt = older.OccurredAt.Add(time.Minute)
	if _, _, err := store.ObserveInbound(ctx, conflicting, window); !errors.Is(err, ErrConversationWindowConflict) {
		t.Fatalf("conflicting replay error=%v want ErrConversationWindowConflict", err)
	}
	current, ok, err := store.Current(ctx, senderID, contactID)
	if err != nil || !ok || current.ProviderMessageID != newer.ProviderMessageID {
		t.Fatalf("conflict changed current evidence: %#v ok=%v err=%v", current, ok, err)
	}
}
