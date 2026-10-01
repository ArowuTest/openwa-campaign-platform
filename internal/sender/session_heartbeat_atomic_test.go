package sender

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/gateway"
	_ "campaign-platform/internal/persistence/database"
)

// These stores interleave a committed mutation AFTER the service reads the old
// node. No production hooks, timing sleeps, overlays, or modified assertions.
type atomicMemoryReadHook struct {
	*MemoryGovernanceStore
	hook func() error
}

func (s *atomicMemoryReadHook) GetNode(ctx context.Context, id string) (Node, error) {
	n, err := s.MemoryGovernanceStore.GetNode(ctx, id)
	if err == nil && s.hook != nil {
		h := s.hook
		s.hook = nil
		err = h()
	}
	return n, err
}

type atomicPostgresReadHook struct {
	*PostgreSQLGovernanceStore
	hook func() error
}

func (s *atomicPostgresReadHook) GetNode(ctx context.Context, id string) (Node, error) {
	n, err := s.PostgreSQLGovernanceStore.GetNode(ctx, id)
	if err == nil && s.hook != nil {
		h := s.hook
		s.hook = nil
		err = h()
	}
	return n, err
}

type atomicHeartbeatFixture struct {
	service       *SessionHeartbeatService
	report        SessionHeartbeatReport
	now           time.Time
	secret        []byte
	nonce         int
	setHook       func(func() error)
	replaceBoot   func(string) error
	changeSession func() error
}

func (f *atomicHeartbeatFixture) send(t *testing.T) (GovernedSession, error) {
	t.Helper()
	f.nonce++
	raw, ts, nonce, sig := signedSessionHeartbeat(t, f.secret, f.now, fmt.Sprintf("atomic-heartbeat-%04d", f.nonce), f.report)
	return f.service.Heartbeat(context.Background(), f.report.SessionID, ts, nonce, sig, raw)
}
func newAtomicMemoryFixture(t *testing.T) *atomicHeartbeatFixture {
	t.Helper()
	ctx := context.Background()
	base := NewMemoryGovernanceStore()
	hook := &atomicMemoryReadHook{MemoryGovernanceStore: base}
	governance := &GovernanceService{Store: hook}
	node, session := readyHeartbeatSession(t, ctx, governance)
	f := &atomicHeartbeatFixture{now: time.Now().UTC().Truncate(time.Second), secret: bytes.Repeat([]byte{0x69}, 32)}
	f.report = SessionHeartbeatReport{NodeID: node.ID, SessionID: session.ID, BootID: node.BootID, Status: StatusReady, EngineVersion: "atomic-v1", SentToday: 7}
	f.service = &SessionHeartbeatService{Governance: governance, Runtime: &RuntimeRegistrationService{Store: hook, Secret: f.secret, Clock: func() time.Time { return f.now }}, Leases: gateway.NewMemoryLeaseStore(), LeaseTTL: 90 * time.Second}
	f.setHook = func(h func() error) { hook.hook = h }
	f.replaceBoot = func(boot string) error {
		n, err := base.GetNode(ctx, node.ID)
		if err != nil {
			return err
		}
		n.BootID = boot
		_, err = base.HeartbeatNode(ctx, n.ID, n.Version, n, f.now)
		return err
	}
	f.changeSession = func() error {
		s, err := base.GetSession(ctx, session.ID)
		if err != nil {
			return err
		}
		_, err = base.HeartbeatSession(ctx, s.ID, s.Version, s, f.now.Add(-time.Second))
		return err
	}
	return f
}
func newAtomicPostgresFixture(t *testing.T) *atomicHeartbeatFixture {
	t.Helper()
	dsn := os.Getenv("POSTGRES_SENDER_HEARTBEAT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_SENDER_HEARTBEAT_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var node, session string
	if err = db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&node, &session); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		for _, q := range []string{`DELETE FROM gateway_runtime_nonces WHERE node_id=$1::uuid`, `DELETE FROM sender_session_leases WHERE worker_node_id=$1::uuid`, `DELETE FROM sender_sessions WHERE node_id=$1::uuid`, `DELETE FROM sender_nodes WHERE id=$1::uuid`} {
			if _, e := db.ExecContext(cleanup, q, node); e != nil {
				t.Errorf("fixture cleanup: %v", e)
			}
		}
	})
	if _, err = db.ExecContext(ctx, `INSERT INTO sender_nodes(id,name,boot_id,status,capacity,governance_version) VALUES($1::uuid,$2,'atomic-boot-a','READY',1,1)`, node, "atomic-"+node); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO sender_sessions(id,node_id,encrypted_msisdn,masked_msisdn,engine_type,engine_version,status,safe_messages_per_minute,safe_daily_capacity,in_flight_limit,sent_today,governance_version) VALUES($1::uuid,$2::uuid,decode('00','hex'),'***4041','WHATSAPP_WEB_JS','atomic-v1','READY',10,100,1,0,1)`, session, node); err != nil {
		t.Fatal(err)
	}
	base := &PostgreSQLGovernanceStore{DB: db}
	hook := &atomicPostgresReadHook{PostgreSQLGovernanceStore: base}
	f := &atomicHeartbeatFixture{now: time.Now().UTC().Truncate(time.Second), secret: bytes.Repeat([]byte{0x68}, 32)}
	f.report = SessionHeartbeatReport{NodeID: node, SessionID: session, BootID: "atomic-boot-a", Status: StatusReady, EngineVersion: "atomic-v1", SentToday: 7}
	f.service = &SessionHeartbeatService{Governance: &GovernanceService{Store: hook}, Runtime: &RuntimeRegistrationService{Store: hook, Secret: f.secret, Clock: func() time.Time { return f.now }}, Leases: &gateway.PostgreSQLLeaseStore{DB: db}, LeaseTTL: 90 * time.Second}
	f.setHook = func(h func() error) { hook.hook = h }
	f.replaceBoot = func(boot string) error {
		_, e := db.Exec(`UPDATE sender_nodes SET boot_id=$2 WHERE id=$1::uuid`, node, boot)
		return e
	}
	f.changeSession = func() error {
		_, e := db.Exec(`UPDATE sender_sessions SET governance_version=governance_version+1 WHERE id=$1::uuid`, session)
		return e
	}
	return f
}
func runAtomicHeartbeatCases(t *testing.T, fixture func(*testing.T) *atomicHeartbeatFixture) {
	t.Run("stale_boot_cannot_renew_after_replacement", func(t *testing.T) {
		f := fixture(t)
		first, err := f.send(t)
		if err != nil {
			t.Fatal(err)
		}
		before, ok, err := f.service.Leases.Get(context.Background(), f.report.SessionID)
		if err != nil || !ok {
			t.Fatal("initial lease missing", err)
		}
		f.now = f.now.Add(time.Second)
		f.report.SentToday = 19
		f.setHook(func() error { return f.replaceBoot("atomic-boot-b") })
		_, err = f.send(t)
		if !errors.Is(err, ErrSessionHeartbeatIdentity) {
			t.Errorf("old boot accepted after replacement committed: %v", err)
		}
		after, ok, err := f.service.Leases.Get(context.Background(), f.report.SessionID)
		if err != nil || !ok || after.Version != before.Version || !after.ExpiresAt.Equal(before.ExpiresAt) {
			t.Errorf("rejected old boot mutated lease: before=%+v after=%+v exists=%v err=%v", before, after, ok, err)
		}
		state, err := f.service.Governance.Store.GetSession(context.Background(), f.report.SessionID)
		if err != nil || state.Version != first.Version || state.SentToday != first.SentToday {
			t.Errorf("rejected old boot mutated session: before=%+v after=%+v err=%v", first, state, err)
		}
	})
	t.Run("conflict_preserves_expired_high_water_and_retry", func(t *testing.T) {
		f := fixture(t)
		f.report.Status = StatusDisconnected
		for i := 0; i < 12; i++ {
			f.now = f.now.Add(time.Second)
			if _, err := f.send(t); err != nil {
				t.Fatal(err)
			}
		}
		before, ok, err := f.service.Leases.Get(context.Background(), f.report.SessionID)
		if err != nil || !ok {
			t.Fatal("initial lease missing", err)
		}
		f.now = before.ExpiresAt.Add(time.Second)
		f.setHook(f.changeSession)
		if _, err = f.send(t); !errors.Is(err, ErrSenderConflict) {
			t.Fatalf("expected governance conflict, got %v", err)
		}
		after, ok, err := f.service.Leases.Get(context.Background(), f.report.SessionID)
		if err != nil || !ok || after.Version != before.Version || !after.ExpiresAt.Equal(before.ExpiresAt) {
			t.Errorf("failed heartbeat deleted or advanced durable high-water: before=%+v after=%+v exists=%v err=%v", before, after, ok, err)
		}
		f.now = f.now.Add(time.Second)
		if _, err = f.send(t); err != nil {
			t.Fatal(err)
		}
		retry, ok, err := f.service.Leases.Get(context.Background(), f.report.SessionID)
		if err != nil || !ok || retry.Version <= before.Version {
			t.Errorf("retry regressed fence: previous=%+v retry=%+v err=%v", before, retry, err)
		}
	})
	t.Run("replacement_requires_expiry_then_nonsending_recovery", func(t *testing.T) {
		f := fixture(t)
		if _, err := f.send(t); err != nil {
			t.Fatal(err)
		}
		old, _, _ := f.service.Leases.Get(context.Background(), f.report.SessionID)
		if err := f.replaceBoot("atomic-boot-b"); err != nil {
			t.Fatal(err)
		}
		f.report.BootID = "atomic-boot-b"
		f.now = f.now.Add(time.Second)
		if _, err := f.send(t); !errors.Is(err, gateway.ErrLeaseHeld) {
			t.Fatalf("replacement took live lease: %v", err)
		}
		f.now = old.ExpiresAt.Add(time.Second)
		if _, err := f.send(t); !errors.Is(err, ErrSessionHeartbeatRecoveryRequired) {
			t.Fatalf("replacement became READY before recovery: %v", err)
		}
		for _, status := range []Status{StatusDisconnected, StatusConnecting, StatusReady} {
			f.now = f.now.Add(time.Second)
			f.report.Status = status
			state, err := f.send(t)
			if err != nil || state.Status != status {
				t.Fatalf("recovery state %s: state=%+v err=%v", status, state, err)
			}
		}
		current, ok, err := f.service.Leases.Get(context.Background(), f.report.SessionID)
		if err != nil || !ok || current.Version <= old.Version {
			t.Fatal("recovery did not advance fence", err)
		}
		current.Token = f.report.BootID
		if err := f.service.Leases.Validate(context.Background(), current, f.now); err != nil {
			t.Fatal("new boot does not own recovered lease", err)
		}
	})
}
func TestMemoryAtomicSessionHeartbeat(t *testing.T) {
	runAtomicHeartbeatCases(t, newAtomicMemoryFixture)
}
func TestPostgreSQLAtomicSessionHeartbeat(t *testing.T) {
	runAtomicHeartbeatCases(t, newAtomicPostgresFixture)
}

// The gateway reports routine ownership retirement as DRAINING until teardown
// completes. Prove that this existing non-sending path remains recoverable,
// unlike a genuine RESTRICTED failure that requires administrative intervention.
func runOwnershipRetirementRecovery(t *testing.T, fixture func(*testing.T) *atomicHeartbeatFixture) {
	t.Helper()
	f := fixture(t)
	var previousFence int64
	for _, step := range []struct{ reported, expected Status }{
		{StatusReady, StatusReady},
		{StatusDraining, StatusDraining},
		{StatusDraining, StatusDraining},
		{StatusDisconnected, StatusDisconnected},
		{StatusReady, StatusDisconnected}, // no direct reactivation before connection
		{StatusConnecting, StatusConnecting},
		{StatusReady, StatusReady},
	} {
		f.now = f.now.Add(time.Second)
		f.report.Status = step.reported
		state, err := f.send(t)
		if err != nil || state.Status != step.expected || state.Ownership == nil {
			t.Fatalf("retirement report %s: state=%+v err=%v", step.reported, state, err)
		}
		if state.Ownership.BootID != f.report.BootID || state.Ownership.LeaseVersion <= previousFence {
			t.Fatalf("retirement receipt lost boot/fence continuity: %+v", state.Ownership)
		}
		previousFence = state.Ownership.LeaseVersion
	}
}

func TestMemoryOwnershipRetirementSupportsControlledRecovery(t *testing.T) {
	runOwnershipRetirementRecovery(t, newAtomicMemoryFixture)
}

func TestPostgreSQLOwnershipRetirementSupportsControlledRecovery(t *testing.T) {
	runOwnershipRetirementRecovery(t, newAtomicPostgresFixture)
}

type heartbeatDuringCreateGateway struct {
	fakeSessionGateway
	onCreate func()
}

func (g *heartbeatDuringCreateGateway) Create(context.Context, Node, GovernedSession) (SessionGatewayResult, error) {
	g.onCreate()
	return g.hit("create")
}

// Creating the gateway record makes it visible to the heartbeat scanner before
// the operator's NEW -> PAIRING governance transition has committed. Telemetry
// must not consume that version or install ownership for an uncreated session.
func TestMemorySessionCreateHeartbeatDoesNotStealGovernanceVersion(t *testing.T) {
	ctx := context.Background()
	lifecycle, session := lifecycleFixture(t)
	store := lifecycle.Governance.Store.(*MemoryGovernanceStore)
	node, err := store.GetNode(ctx, session.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	node.BootID = "creation-boot"
	if _, err = store.HeartbeatNode(ctx, node.ID, node.Version, node, now); err != nil {
		t.Fatal(err)
	}
	f := &atomicHeartbeatFixture{now: now, secret: bytes.Repeat([]byte{0x71}, 32)}
	f.report = SessionHeartbeatReport{NodeID: node.ID, SessionID: session.ID, BootID: node.BootID, Status: StatusDisconnected, EngineVersion: "creation-v1", SentToday: 0}
	f.service = &SessionHeartbeatService{
		Governance: lifecycle.Governance,
		Runtime:    &RuntimeRegistrationService{Store: store, Secret: f.secret, Clock: func() time.Time { return f.now }},
		Leases:     gateway.NewMemoryLeaseStore(), LeaseTTL: 90 * time.Second,
	}
	var heartbeatError error
	lifecycle.Gateway = &heartbeatDuringCreateGateway{onCreate: func() {
		_, heartbeatError = f.send(t)
		current, readErr := store.GetSession(ctx, session.ID)
		if readErr != nil || current.Version != session.Version || current.Status != StatusNew {
			t.Errorf("early heartbeat changed uncreated session: %+v err=%v", current, readErr)
		}
		if lease, exists, readErr := f.service.Leases.Get(ctx, session.ID); readErr != nil || exists {
			t.Errorf("early heartbeat created session authority: %+v exists=%v err=%v", lease, exists, readErr)
		}
	}}
	created, _, err := lifecycle.Create(ctx, session.ID, session.Version, "actor", "create gateway session safely")
	if !errors.Is(heartbeatError, ErrSenderConflict) {
		t.Errorf("heartbeat before governed creation should defer, got %v", heartbeatError)
	}
	if err != nil || created.Status != StatusPairing {
		t.Fatalf("heartbeat stranded gateway creation: status=%s err=%v", created.Status, err)
	}
	f.now = f.now.Add(time.Second)
	readyForStart, err := f.send(t)
	if err != nil || readyForStart.Status != StatusDisconnected || readyForStart.Ownership == nil {
		t.Fatalf("post-create heartbeat did not establish non-sending ownership: %+v err=%v", readyForStart, err)
	}
}
