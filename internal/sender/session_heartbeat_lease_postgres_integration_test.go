package sender

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/gateway"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLSignedSessionHeartbeatAcquiresAndRenewsBootBoundLease(t *testing.T) {
	dsn := os.Getenv("POSTGRES_SENDER_HEARTBEAT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_SENDER_HEARTBEAT_DATABASE_URL is not set")
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

	var nodeID, sessionID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&nodeID, &sessionID); err != nil {
		t.Fatal(err)
	}
	bootID := "lease-heartbeat-boot-" + nodeID
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sender_nodes(id,name,boot_id,status,capacity,governance_version)
		VALUES($1::uuid,$2,$3,'READY',1,1)`, nodeID, "lease-heartbeat-"+nodeID, bootID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sender_sessions(
			id,node_id,encrypted_msisdn,masked_msisdn,engine_type,engine_version,status,
			safe_messages_per_minute,safe_daily_capacity,in_flight_limit,sent_today,governance_version
		) VALUES($1::uuid,$2::uuid,decode('00','hex'),'***4040','WHATSAPP_WEB_JS','adapter-v1','READY',10,100,1,0,1)`,
		sessionID, nodeID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_nonces WHERE node_id=$1::uuid`, nodeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_session_leases WHERE session_id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id=$1::uuid`, nodeID)
	})

	store := &PostgreSQLGovernanceStore{DB: db}
	fixed := time.Now().UTC().Truncate(time.Second)
	secret := bytes.Repeat([]byte{0x65}, 32)
	runtime := &RuntimeRegistrationService{Store: store, Secret: secret, Clock: func() time.Time { return fixed }}
	service := &SessionHeartbeatService{
		Governance: &GovernanceService{Store: store},
		Runtime:    runtime,
		Leases:     &gateway.PostgreSQLLeaseStore{DB: db},
		LeaseTTL:   90 * time.Second,
	}
	report := SessionHeartbeatReport{
		NodeID: nodeID, SessionID: sessionID, BootID: bootID, Status: StatusReady,
		EngineVersion: "adapter-v1", SentToday: 4,
	}

	raw, timestamp, nonce, signature := signedSessionHeartbeat(t, secret, fixed, "pg-session-lease-heartbeat-0001", report)
	updated, err := service.Heartbeat(ctx, sessionID, timestamp, nonce, signature, raw)
	if err != nil {
		t.Fatal(err)
	}
	if updated.SentToday != 4 || updated.Status != StatusReady {
		t.Fatalf("unexpected first PostgreSQL heartbeat: %+v", updated)
	}
	var worker string
	var version int64
	var expires time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT worker_node_id::text,version,expires_at
		FROM sender_session_leases WHERE session_id=$1::uuid`, sessionID).Scan(&worker, &version, &expires); err != nil {
		t.Fatal(err)
	}
	if worker != nodeID || version != 1 || !expires.Equal(fixed.Add(90*time.Second)) {
		t.Fatalf("unexpected initial PostgreSQL lease worker=%s version=%d expires=%s", worker, version, expires)
	}

	fixed = fixed.Add(30 * time.Second)
	report.SentToday = 5
	raw, timestamp, nonce, signature = signedSessionHeartbeat(t, secret, fixed, "pg-session-lease-heartbeat-0002", report)
	updated, err = service.Heartbeat(ctx, sessionID, timestamp, nonce, signature, raw)
	if err != nil {
		t.Fatal(err)
	}
	if updated.SentToday != 5 {
		t.Fatalf("renewed heartbeat did not advance usage: %+v", updated)
	}
	var renewedVersion int64
	var renewedExpires time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT version,expires_at FROM sender_session_leases WHERE session_id=$1::uuid`,
		sessionID).Scan(&renewedVersion, &renewedExpires); err != nil {
		t.Fatal(err)
	}
	if renewedVersion != version+1 || !renewedExpires.Equal(fixed.Add(90*time.Second)) {
		t.Fatalf("PostgreSQL heartbeat did not renew fence old=%d new=%d expires=%s", version, renewedVersion, renewedExpires)
	}
}
