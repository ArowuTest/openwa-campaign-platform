package sender

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLSessionProxyGovernance(t *testing.T) {
	dsn := os.Getenv("POSTGRES_SENDER_PROXY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_SENDER_PROXY_DATABASE_URL is not set")
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
	var actorID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM internal_users ORDER BY created_at LIMIT 1`).Scan(&actorID); err != nil {
		t.Fatalf("load audit actor: %v", err)
	}
	var sessionID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_sessions(
		id,encrypted_msisdn,masked_msisdn,engine_type,status,
		safe_messages_per_minute,safe_daily_capacity,in_flight_limit,governance_version
	) VALUES($1::uuid,decode('00','hex'),'***0000','WHATSAPP_WEB_JS','DISCONNECTED',10,100,1,1)`, sessionID); err != nil {
		t.Fatalf("insert sender session: %v", err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_governance_events WHERE object_type='SESSION' AND object_id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, sessionID)
	}()

	store := &PostgreSQLGovernanceStore{DB: db}
	ciphertext := []byte(`v1:encrypted-proxy-envelope`)
	configured, err := store.ConfigureSessionProxy(ctx, sessionID, 1, ciphertext, actorID, "approved stable proxy routing")
	if err != nil {
		t.Fatal(err)
	}
	if !configured.Configured || configured.Version != 2 {
		t.Fatalf("configured=%+v", configured)
	}
	loaded, version, err := store.LoadSessionProxy(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != string(ciphertext) || version != 2 {
		t.Fatalf("loaded=%q version=%d", loaded, version)
	}
	var configuredAudit int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sender_governance_events
		WHERE object_type='SESSION' AND object_id=$1::uuid AND action='PROXY_CONFIGURED'`, sessionID).Scan(&configuredAudit); err != nil {
		t.Fatal(err)
	}
	if configuredAudit != 1 {
		t.Fatalf("configured audit count=%d", configuredAudit)
	}

	if _, err := db.ExecContext(ctx, `UPDATE sender_sessions SET status='READY' WHERE id=$1::uuid`, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConfigureSessionProxy(ctx, sessionID, 2, []byte(`replacement`), actorID, "attempt live proxy change"); !errors.Is(err, ErrSenderConflict) {
		t.Fatalf("expected active-session conflict, got %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE sender_sessions SET status='DISCONNECTED' WHERE id=$1::uuid`, sessionID); err != nil {
		t.Fatal(err)
	}
	cleared, err := store.ClearSessionProxy(ctx, sessionID, 2, actorID, "remove approved stable proxy")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Configured || cleared.Version != 3 {
		t.Fatalf("cleared=%+v", cleared)
	}
	loaded, version, err = store.LoadSessionProxy(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil || version != 3 {
		t.Fatalf("loaded after clear=%q version=%d", loaded, version)
	}
	var clearedAudit int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sender_governance_events
		WHERE object_type='SESSION' AND object_id=$1::uuid AND action='PROXY_CLEARED'`, sessionID).Scan(&clearedAudit); err != nil {
		t.Fatal(err)
	}
	if clearedAudit != 1 {
		t.Fatalf("cleared audit count=%d", clearedAudit)
	}
}
