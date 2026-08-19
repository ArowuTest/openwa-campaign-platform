package sender

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLHeartbeatSessionCannotRewriteGovernedCapacityOrReduceUsage(t *testing.T) {
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
	var actorID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 heartbeat','DISABLED',false)`, actorID, "heartbeat-"+actorID+"@internal.invalid"); err != nil {
		t.Fatalf("insert audit actor: %v", err)
	}
	store := &PostgreSQLGovernanceStore{DB: db}
	created, err := store.RegisterSession(ctx, GovernedSession{
		MaskedMSISDN: "+234 *** 1000", OwnerReference: "task6-heartbeat", RegistrationCountryISO2: "NG",
		ProfileDisplayName: "Task 6 heartbeat", RecoveryReference: "vault://task6/heartbeat",
		EngineType: "WHATSAPP_WEB_JS", Status: StatusReady,
		SafeMessagesPerMinute: 25, SafeDailyCapacity: 500, InFlightLimit: 2,
	}, []byte{0x01, 0x02}, actorID, "register heartbeat regression sender")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_governance_events WHERE object_type='SESSION' AND object_id=$1::uuid`, created.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, created.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	now := time.Date(2026, 8, 10, 0, 30, 0, 0, time.UTC)
	first, err := store.HeartbeatSession(ctx, created.ID, created.Version, GovernedSession{
		Status: StatusReady, EngineVersion: "engine-1",
		SafeMessagesPerMinute: 999, SafeDailyCapacity: 9999, InFlightLimit: 99, SentToday: 120,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.SafeMessagesPerMinute != 25 || first.SafeDailyCapacity != 500 || first.InFlightLimit != 2 {
		t.Fatalf("heartbeat rewrote governed capacity: %+v", first)
	}
	if first.SentToday != 120 {
		t.Fatalf("heartbeat did not advance observed usage: %d", first.SentToday)
	}
	second, err := store.HeartbeatSession(ctx, created.ID, first.Version, GovernedSession{
		Status: StatusPairing, EngineVersion: "engine-2",
		SafeMessagesPerMinute: 1, SafeDailyCapacity: 1, InFlightLimit: 1, SentToday: 10,
	}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != StatusReady || second.SafeMessagesPerMinute != 25 || second.SafeDailyCapacity != 500 || second.InFlightLimit != 2 {
		t.Fatalf("later heartbeat rewrote governed capacity: %+v", second)
	}
	if second.SentToday != 120 {
		t.Fatalf("heartbeat reduced observed usage from 120 to %d", second.SentToday)
	}
	third, err := store.HeartbeatSession(ctx, created.ID, second.Version, GovernedSession{
		Status: StatusReady, EngineVersion: "engine-3", SentToday: 5,
	}, now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if third.SentToday != 5 {
		t.Fatalf("new UTC day did not reset observed usage: %d", third.SentToday)
	}
}
