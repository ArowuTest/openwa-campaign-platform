package sender

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

type isolatedPostgreSQLGovernanceStore struct {
	*PostgreSQLGovernanceStore
	sessionID string
}

func (s *isolatedPostgreSQLGovernanceStore) ListSessions(ctx context.Context) ([]GovernedSession, error) {
	v, err := s.GetSession(ctx, s.sessionID)
	if err != nil {
		return nil, err
	}
	return []GovernedSession{v}, nil
}

func TestPostgreSQLHealthEnforcerDrainsBreachedSenderAndAudits(t *testing.T) {
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
	var senderID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&senderID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_governance_events WHERE object_type='SESSION' AND object_id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, senderID)
	}()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_sessions(
		id,encrypted_msisdn,masked_msisdn,engine_type,status,safe_messages_per_minute,safe_daily_capacity,
		last_heartbeat_at,last_success_at,owner_reference,registration_country_iso2,profile_display_name,recovery_reference)
		VALUES($1::uuid,decode('00','hex'),'***9101','WHATSAPP_WEB_JS','READY',10,100,$2,$2,'ops-owner','GB','Health Test Sender','vault://sender-health-test')`, senderID, now); err != nil {
		t.Fatalf("insert sender: %v", err)
	}
	for i := 0; i < DefaultHealthPolicy().DisconnectThreshold; i++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO sender_governance_events(
			object_type,object_id,action,actor_id,reason,object_version,created_at)
			VALUES('SESSION',$1::uuid,'STATUS_DISCONNECTED',$2::uuid,'integration disconnect evidence',1,$3)`,
			senderID, HealthProtectionServiceActorID, now.Add(-time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("insert disconnect evidence %d: %v", i, err)
		}
	}
	base := &PostgreSQLGovernanceStore{DB: db}
	store := &isolatedPostgreSQLGovernanceStore{PostgreSQLGovernanceStore: base, sessionID: senderID}
	governance := &GovernanceService{Store: store, HealthSignals: &PostgreSQLHealthSignalSource{DB: db}}
	enforcer := &HealthEnforcer{
		Governance: governance,
		ActorID:    HealthProtectionServiceActorID,
		Clock:      func() time.Time { return now },
	}
	drained, err := enforcer.Process(ctx)
	if err != nil {
		t.Fatalf("enforce sender health: %v", err)
	}
	if drained != 1 {
		t.Fatalf("drained=%d want=1", drained)
	}
	var status string
	var version int64
	if err := db.QueryRowContext(ctx, `SELECT status,governance_version FROM sender_sessions WHERE id=$1::uuid`, senderID).Scan(&status, &version); err != nil {
		t.Fatalf("read drained sender: %v", err)
	}
	if status != string(StatusDraining) || version != 2 {
		t.Fatalf("sender status=%s version=%d want DRAINING/2", status, version)
	}
	var actor, reason string
	var eventVersion int64
	if err := db.QueryRowContext(ctx, `SELECT actor_id::text,reason,object_version
		FROM sender_governance_events
		WHERE object_type='SESSION' AND object_id=$1::uuid AND action='STATUS_DRAINING'
		ORDER BY created_at DESC LIMIT 1`, senderID).Scan(&actor, &reason, &eventVersion); err != nil {
		t.Fatalf("read drain audit event: %v", err)
	}
	if actor != HealthProtectionServiceActorID || eventVersion != 2 || !strings.Contains(reason, "DISCONNECT_THRESHOLD_BREACHED") {
		t.Fatalf("unexpected drain audit actor=%s version=%d reason=%q", actor, eventVersion, reason)
	}
}
