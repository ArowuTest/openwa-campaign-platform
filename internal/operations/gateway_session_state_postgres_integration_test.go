package operations

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/gateway"
	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/sender"
)

func TestPostgreSQLDashboardReflectsSignedSenderDisconnectAndReconnect(t *testing.T) {
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
	bootID := "gwy006-boot-" + nodeID
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sender_nodes(id,name,boot_id,status,capacity,governance_version)
		VALUES($1::uuid,$2,$3,'READY',1,1)`, nodeID, "gwy006-"+nodeID, bootID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sender_sessions(
			id,node_id,encrypted_msisdn,masked_msisdn,engine_type,engine_version,status,
			safe_messages_per_minute,safe_daily_capacity,in_flight_limit,sent_today,governance_version
		) VALUES($1::uuid,$2::uuid,decode('00','hex'),'***6060','WHATSAPP_WEB_JS','adapter-v1','READY',10,100,1,0,1)`,
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

	store := &sender.PostgreSQLGovernanceStore{DB: db}
	fixed := time.Now().UTC().Truncate(time.Second)
	secret := bytes.Repeat([]byte{0x66}, 32)
	service := &sender.SessionHeartbeatService{
		Governance: &sender.GovernanceService{Store: store},
		Runtime:    &sender.RuntimeRegistrationService{Store: store, Secret: secret, Clock: func() time.Time { return fixed }},
		Leases:     &gateway.PostgreSQLLeaseStore{DB: db},
		LeaseTTL:   90 * time.Second,
	}
	report := sender.SessionHeartbeatReport{
		NodeID: nodeID, SessionID: sessionID, BootID: bootID, Status: sender.StatusReady,
		EngineVersion: "adapter-v1", SentToday: 1,
	}
	heartbeat := func(nonce string) {
		t.Helper()
		raw, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		timestamp, signedNonce, signature, err := sender.SignRuntimeReport(secret, fixed, nonce, raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Heartbeat(ctx, sessionID, timestamp, signedNonce, signature, raw); err != nil {
			t.Fatal(err)
		}
	}

	heartbeat("gwy006-heartbeat-ready-0001")
	repository := &PostgreSQLRepository{DB: db}
	baseline, err := repository.Dashboard(ctx, fixed)
	if err != nil {
		t.Fatal(err)
	}
	baselineReady := baseline.Senders[string(sender.StatusReady)]
	baselineDisconnected := baseline.Senders[string(sender.StatusDisconnected)]
	baselineConnecting := baseline.Senders[string(sender.StatusConnecting)]
	baselineUnhealthy := baseline.UnhealthySenderSessions
	if baselineReady < 1 {
		t.Fatalf("signed READY heartbeat was not visible in operations dashboard: %+v", baseline.Senders)
	}

	fixed = fixed.Add(30 * time.Second)
	report.Status = sender.StatusDisconnected
	heartbeat("gwy006-heartbeat-disconnected-0002")
	disconnected, err := repository.Dashboard(ctx, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if disconnected.Senders[string(sender.StatusReady)] != baselineReady-1 ||
		disconnected.Senders[string(sender.StatusDisconnected)] != baselineDisconnected+1 ||
		disconnected.UnhealthySenderSessions != baselineUnhealthy+1 {
		t.Fatalf("forced disconnect did not propagate to dashboard: before=%+v after=%+v unhealthy=%d->%d",
			baseline.Senders, disconnected.Senders, baselineUnhealthy, disconnected.UnhealthySenderSessions)
	}

	fixed = fixed.Add(30 * time.Second)
	report.Status = sender.StatusConnecting
	heartbeat("gwy006-heartbeat-connecting-0003")
	connecting, err := repository.Dashboard(ctx, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if connecting.Senders[string(sender.StatusDisconnected)] != baselineDisconnected ||
		connecting.Senders[string(sender.StatusConnecting)] != baselineConnecting+1 {
		t.Fatalf("reconnect did not propagate CONNECTING to dashboard: before=%+v after=%+v", baseline.Senders, connecting.Senders)
	}

	fixed = fixed.Add(30 * time.Second)
	report.Status = sender.StatusReady
	heartbeat("gwy006-heartbeat-ready-0004")
	ready, err := repository.Dashboard(ctx, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Senders[string(sender.StatusReady)] != baselineReady ||
		ready.Senders[string(sender.StatusConnecting)] != baselineConnecting ||
		ready.UnhealthySenderSessions != baselineUnhealthy {
		t.Fatalf("successful reconnect did not restore dashboard state: before=%+v after=%+v unhealthy=%d->%d",
			baseline.Senders, ready.Senders, baselineUnhealthy, ready.UnhealthySenderSessions)
	}
}
