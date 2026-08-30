package sender

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLRuntimeResourceHealthPersistsAcrossReread(t *testing.T) {
	dsn := os.Getenv("POSTGRES_GATEWAY_RUNTIME_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_GATEWAY_RUNTIME_DATABASE_URL is not set")
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
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Runtime Health Test Actor','DISABLED',false)`, actorID, "runtime-health-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLGovernanceStore{DB: db}
	pool, err := (&GatewayPoolService{Store: store}).Create(ctx, GatewayPool{
		Name:     "runtime-health-" + actorID,
		Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys,
		AdapterVersion: "integration-1", Status: GatewayPoolActive,
		Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1,
	}, actorID, "runtime resource health integration")
	if err != nil {
		t.Fatal(err)
	}
	node, err := (&GovernanceService{Store: store}).RegisterNode(ctx, Node{
		Name: "runtime-health-node-" + actorID, Status: "OFFLINE",
		GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "integration-1", InternalURL: "https://gateway.integration.invalid",
	}, actorID, "runtime resource health integration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_nonces WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_events WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_governance_events WHERE object_type='NODE' AND object_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pool_events WHERE gateway_pool_id=$1::uuid`, pool.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pools WHERE id=$1::uuid`, pool.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()

	now := time.Now().UTC().Truncate(time.Second)
	report := RuntimeReport{
		NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID,
		Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "integration-1",
		GatewayVersion: "0.8.28", WorkerVersion: "node-22", ConfigurationVersion: "cfg-health",
		BootID: "boot-health-pg", RuntimeSequence: 1, InternalURL: "https://gateway.integration.invalid",
		Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady,
		Capacity: 3, SessionCount: 2, QueueDepth: 4, CPUPercent: 12.25, MemoryBytes: 123456,
		ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now,
	}
	stored, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", "", time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != node.Version {
		t.Fatalf("runtime report changed governed node version: got %d want %d", stored.Version, node.Version)
	}
	report.RuntimeSequence = 2
	report.ObservedAt = now.Add(-500 * time.Millisecond)
	report.QueueDepth = 5
	stored, err = store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", "", time.Time{}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("same-boot clock correction with a newer runtime sequence was rejected: %v", err)
	}
	if stored.Version != node.Version || stored.QueueDepth != 5 {
		t.Fatalf("second runtime report did not preserve governance version and telemetry: %#v", stored)
	}
	report.RuntimeSequence = 3
	report.InternalURL = "https://gateway-drift.integration.invalid"
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", strings.Repeat("9", 64), time.Time{}, now.Add(1500*time.Millisecond)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("PostgreSQL runtime store accepted governed URL drift inside transaction: %v", err)
	}
	report.InternalURL = "https://gateway.integration.invalid"
	if stored.ResourceHealth.DiskFreeBytes == nil || *stored.ResourceHealth.DiskFreeBytes != 400_000 {
		t.Fatalf("runtime resource health missing from update result: %#v", stored.ResourceHealth)
	}
	reread, err := store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.ResourceHealth.Scope != "CONTAINER" || reread.ResourceHealth.FilesystemPath != "/data" ||
		reread.ResourceHealth.DiskAvailableBytes == nil || *reread.ResourceHealth.DiskAvailableBytes != 350_000 ||
		reread.ResourceHealth.NetworkRXBytes == nil || *reread.ResourceHealth.NetworkRXBytes != 12_345 ||
		reread.ResourceHealth.OpenFileDescriptorCount == nil || *reread.ResourceHealth.OpenFileDescriptorCount != 14 {
		t.Fatalf("runtime resource health did not survive PostgreSQL reread: %#v", reread.ResourceHealth)
	}

	// A failed report mutation must roll back its nonce so a corrected retry can
	// be accepted as one atomic PostgreSQL transaction.
	report.BootID = "boot-atomic-pg"
	report.RuntimeSequence = 1
	report.RuntimeState = RuntimeState("INVALID")
	report.ObservedAt = now.Add(2 * time.Second)
	nonce := "nonce-pg-atomic-rollback-1234"
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, nonce, strings.Repeat("a", 64), now.Add(time.Minute), report.ObservedAt); err == nil {
		t.Fatal("invalid runtime state unexpectedly committed")
	}
	report.RuntimeState = RuntimeReady
	report.ObservedAt = now.Add(3 * time.Second)
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, nonce, strings.Repeat("b", 64), now.Add(time.Minute), report.ObservedAt); err != nil {
		t.Fatalf("failed transaction burned runtime nonce: %v", err)
	}

	// DRAINING is terminal within a process boot. A same-boot READY report is
	// rejected and audited; only a new boot can re-enter READY.
	report.RuntimeState = RuntimeDraining
	report.RuntimeSequence = 2
	report.ObservedAt = now.Add(4 * time.Second)
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", strings.Repeat("c", 64), time.Time{}, report.ObservedAt); err != nil {
		t.Fatalf("failed to enter DRAINING: %v", err)
	}
	report.RuntimeState = RuntimeReady
	report.RuntimeSequence = 3
	report.ObservedAt = now.Add(5 * time.Second)
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", strings.Repeat("d", 64), time.Time{}, report.ObservedAt); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("same-boot READY did not remain fenced after DRAINING: %v", err)
	}
	reread, err = store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.RuntimeState != RuntimeDraining || reread.BootID != report.BootID {
		t.Fatalf("same-boot report reopened DRAINING runtime: %#v", reread)
	}
	var rejectionReason string
	if err := db.QueryRowContext(ctx, `SELECT reason FROM gateway_runtime_events WHERE node_id=$1::uuid AND event_type='REJECTED' ORDER BY occurred_at DESC,id DESC LIMIT 1`, node.ID).Scan(&rejectionReason); err != nil {
		t.Fatalf("same-boot DRAINING rejection was not audited: %v", err)
	}
	if rejectionReason != "runtime state cannot leave DRAINING without a new boot ID" {
		t.Fatalf("unexpected DRAINING rejection evidence: %q", rejectionReason)
	}
	report.BootID = "boot-after-drain-pg"
	report.RuntimeSequence = 1
	report.ObservedAt = now.Add(6 * time.Second)
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", strings.Repeat("e", 64), time.Time{}, report.ObservedAt); err != nil {
		t.Fatalf("new boot could not re-enter READY: %v", err)
	}
	report.BootID = "boot-atomic-pg"
	report.RuntimeSequence = 4
	report.RuntimeState = RuntimeDraining
	report.ObservedAt = now.Add(7 * time.Second)
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", strings.Repeat("f", 64), time.Time{}, report.ObservedAt); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("retired boot clobbered replacement boot with DRAINING: %v", err)
	}
	report.BootID = "boot-rejected-drain-pg"
	report.RuntimeSequence = 1
	report.RuntimeState = RuntimeDraining
	report.ObservedAt = now.Add(8 * time.Second)
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", strings.Repeat("2", 64), time.Time{}, report.ObservedAt); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("replacement boot unexpectedly entered DRAINING: %v", err)
	}
	report.RuntimeSequence = 2
	report.RuntimeState = RuntimeReady
	report.ObservedAt = now.Add(9 * time.Second)
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", strings.Repeat("3", 64), time.Time{}, report.ObservedAt); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("boot left authenticated rejected DRAINING state: %v", err)
	}
	report.BootID = "boot-atomic-pg"
	report.RuntimeSequence = 5
	report.RuntimeState = RuntimeReady
	report.ObservedAt = now.Add(5500 * time.Millisecond)
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", strings.Repeat("1", 64), time.Time{}, report.ObservedAt); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("stale retired-boot observation replaced newer runtime state: %v", err)
	}
	reread, err = store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.RuntimeState != RuntimeReady || reread.Draining || reread.BootID != "boot-after-drain-pg" {
		t.Fatalf("retired runtime report changed replacement state: %#v", reread)
	}
	governed, err := (&GovernanceService{Store: store}).TransitionNode(ctx, node.ID, node.Version, "OFFLINE", actorID, "version-conflict DRAINING integration")
	if err != nil {
		t.Fatal(err)
	}
	report.BootID = "boot-version-conflict-drain-pg"
	report.RuntimeSequence = 1
	report.RuntimeState = RuntimeDraining
	report.ExpectedNodeVersion = node.Version
	report.ObservedAt = now.Add(10 * time.Second)
	conflictNonce := "nonce-version-conflict-drain-pg"
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, conflictNonce, strings.Repeat("4", 64), now.Add(time.Minute), report.ObservedAt); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("stale-version DRAINING did not use transactional rejection path: %v", err)
	}
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, conflictNonce, strings.Repeat("4", 64), now.Add(time.Minute), report.ObservedAt); !errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("stale-version DRAINING rejection did not consume nonce: %v", err)
	}
	report.RuntimeSequence = 2
	report.RuntimeState = RuntimeReady
	report.ExpectedNodeVersion = governed.Version
	report.ObservedAt = now.Add(11 * time.Second)
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, governed.Version, report, "nonce-version-conflict-ready-pg", strings.Repeat("5", 64), now.Add(time.Minute), report.ObservedAt); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("same boot escaped stale-version DRAINING terminality: %v", err)
	}
	var conflictReason, conflictState string
	if err := db.QueryRowContext(ctx, `SELECT reason,coalesce(runtime_identity->>'runtimeState','') FROM gateway_runtime_events WHERE node_id=$1::uuid AND boot_id=$2 AND event_type='REJECTED' ORDER BY occurred_at,id LIMIT 1`, node.ID, report.BootID).Scan(&conflictReason, &conflictState); err != nil {
		t.Fatalf("version-conflict DRAINING rejection was not audited: %v", err)
	}
	if conflictReason != "gateway governance version conflict" || conflictState != string(RuntimeDraining) {
		t.Fatalf("unexpected version-conflict DRAINING evidence: reason=%q state=%q", conflictReason, conflictState)
	}
}

func TestPostgreSQLRejectionOnlyBootCannotReclaimReplacement(t *testing.T) {
	dsn := os.Getenv("POSTGRES_GATEWAY_RUNTIME_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_GATEWAY_RUNTIME_DATABASE_URL is not set")
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
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Rejection Only Boot Test Actor','DISABLED',false)`, actorID, "rejection-only-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLGovernanceStore{DB: db}
	pool, err := (&GatewayPoolService{Store: store}).Create(ctx, GatewayPool{Name: "rejection-only-" + actorID, Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "integration-1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, actorID, "rejection-only boot integration")
	if err != nil {
		t.Fatal(err)
	}
	node, err := (&GovernanceService{Store: store, RequireCanonicalRuntimeURL: true, RuntimeAllowedInternalHosts: []string{"gateway.integration.invalid"}}).RegisterNode(ctx, Node{Name: "rejection-only-node-" + actorID, Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "integration-1", InternalURL: "https://gateway.integration.invalid"}, actorID, "rejection-only boot integration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_nonces WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_events WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_governance_events WHERE object_type='NODE' AND object_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pool_events WHERE gateway_pool_id=$1::uuid`, pool.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pools WHERE id=$1::uuid`, pool.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	base := time.Now().UTC().Truncate(time.Second)
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version + 1, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "integration-1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "boot-a-rejected-pg", RuntimeSequence: 1, InternalURL: "https://gateway.integration.invalid", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: base}
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, report.ExpectedNodeVersion, report, "", "", time.Time{}, base); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("boot-a expected initial rejection, got %v", err)
	}
	report.ExpectedNodeVersion = node.Version
	report.BootID = "boot-b-active-pg"
	report.RuntimeSequence = 1
	report.ObservedAt = base.Add(time.Second)
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", "", time.Time{}, base.Add(time.Second)); err != nil {
		t.Fatalf("replacement boot-b failed: %v", err)
	}
	report.BootID = "boot-a-rejected-pg"
	report.RuntimeSequence = 2
	report.ObservedAt = base.Add(2 * time.Second)
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", "", time.Time{}, base.Add(2*time.Second)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("rejection-only retired boot reclaimed active replacement: %v", err)
	}
	reread, err := store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.BootID != "boot-b-active-pg" || reread.RuntimeState != RuntimeReady {
		t.Fatalf("rejection-only retired boot changed active replacement: %#v", reread)
	}
}

func TestPostgreSQLRejectsDelayedOlderBootFirstReportAfterReplacement(t *testing.T) {
	dsn := os.Getenv("POSTGRES_GATEWAY_RUNTIME_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_GATEWAY_RUNTIME_DATABASE_URL is not set")
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
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Delayed Older Boot Test Actor','DISABLED',false)`, actorID, "delayed-older-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLGovernanceStore{DB: db}
	pool, err := (&GatewayPoolService{Store: store}).Create(ctx, GatewayPool{Name: "delayed-older-" + actorID, Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "integration-1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, actorID, "delayed older boot integration")
	if err != nil {
		t.Fatal(err)
	}
	node, err := (&GovernanceService{Store: store, RequireCanonicalRuntimeURL: true, RuntimeAllowedInternalHosts: []string{"gateway.integration.invalid"}}).RegisterNode(ctx, Node{Name: "delayed-older-node-" + actorID, Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "integration-1", InternalURL: "https://gateway.integration.invalid"}, actorID, "delayed older boot integration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_nonces WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_events WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_governance_events WHERE object_type='NODE' AND object_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pool_events WHERE gateway_pool_id=$1::uuid`, pool.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pools WHERE id=$1::uuid`, pool.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	base := time.Now().UTC().Truncate(time.Second)
	active := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "integration-1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "boot-b-active-delayed-pg", RuntimeSequence: 1, InternalURL: "https://gateway.integration.invalid", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: base.Add(time.Second)}
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, active, "", "", time.Time{}, base.Add(time.Second)); err != nil {
		t.Fatalf("replacement boot-b failed: %v", err)
	}
	delayed := active
	delayed.BootID = "boot-a-delayed-pg"
	delayed.RuntimeSequence = 1
	delayed.ObservedAt = base
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, delayed, "", "", time.Time{}, base.Add(2*time.Second)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("delayed older boot first report reclaimed active replacement: %v", err)
	}
	reread, err := store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.BootID != "boot-b-active-delayed-pg" || reread.RuntimeState != RuntimeReady {
		t.Fatalf("delayed older boot changed active replacement: %#v", reread)
	}

	retry := active
	retry.BootID = "boot-a-retry-pg"
	retry.RuntimeSequence = 2
	retry.ObservedAt = base.Add(3 * time.Second)
	retry.ResourceHealth = completeRuntimeResourceHealth()
	retry.ResourceHealth.ProcessUptimeSeconds = 300
	if _, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, retry, "", "", time.Time{}, base.Add(3*time.Second)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("older boot retry with newer observedAt reclaimed active replacement: %v", err)
	}
	reread, err = store.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.BootID != "boot-b-active-delayed-pg" {
		t.Fatalf("older boot retry changed active replacement: %#v", reread)
	}
}

func TestPostgreSQLRuntimePoolMissRejectionIsAuditable(t *testing.T) {
	dsn := os.Getenv("POSTGRES_GATEWAY_RUNTIME_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_GATEWAY_RUNTIME_DATABASE_URL is not set")
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
	var actorID, missingPoolID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actorID, &missingPoolID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Runtime Pool Miss Actor','DISABLED',false)`, actorID, "runtime-pool-miss-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLGovernanceStore{DB: db}
	pool, err := (&GatewayPoolService{Store: store}).Create(ctx, GatewayPool{Name: "runtime-pool-miss-" + actorID, Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "integration-1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, actorID, "runtime pool miss integration")
	if err != nil {
		t.Fatal(err)
	}
	node, err := (&GovernanceService{Store: store}).RegisterNode(ctx, Node{Name: "runtime-pool-miss-node-" + actorID, Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "integration-1", InternalURL: "https://gateway.integration.invalid"}, actorID, "runtime pool miss integration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_nonces WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_events WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_governance_events WHERE object_type='NODE' AND object_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pool_events WHERE gateway_pool_id=$1::uuid`, pool.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pools WHERE id=$1::uuid`, pool.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	now := time.Now().UTC().Truncate(time.Second)
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: missingPoolID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "integration-1", GatewayVersion: "0.8.28", WorkerVersion: "node-22", ConfigurationVersion: "cfg-pool-miss", BootID: "boot-pool-miss", RuntimeSequence: 1, InternalURL: "https://gateway.integration.invalid", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, err := SignRuntimeReport(secret, now, "nonce-pg-pool-miss-123456789", raw)
	if err != nil {
		t.Fatal(err)
	}
	svc := &RuntimeRegistrationService{Store: store, GatewayPools: store, Secret: secret, AllowedInternalHosts: []string{"gateway.integration.invalid"}, MaximumSkew: time.Minute, Clock: func() time.Time { return now }}
	if _, err := svc.Register(ctx, node.ID, ts, nonce, sig, raw); err == nil {
		t.Fatal("nonexistent gateway pool report was accepted")
	}
	if _, err := svc.Register(ctx, node.ID, ts, nonce, sig, raw); !errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("replayed pool-miss report was not fenced: %v", err)
	}

	var storedPoolID, declaredPoolID, reason string
	if err := db.QueryRowContext(ctx, `SELECT gateway_pool_id::text,coalesce(runtime_identity->>'declaredGatewayPoolId',''),reason FROM gateway_runtime_events WHERE node_id=$1::uuid AND event_type='REJECTED' ORDER BY occurred_at DESC,id DESC LIMIT 1`, node.ID).Scan(&storedPoolID, &declaredPoolID, &reason); err != nil {
		t.Fatalf("nonexistent-pool rejection was not durably audited: %v", err)
	}
	if storedPoolID != pool.ID || declaredPoolID != missingPoolID || reason != "gateway pool unavailable" {
		t.Fatalf("unexpected pool-miss rejection evidence: stored=%s declared=%s reason=%q", storedPoolID, declaredPoolID, reason)
	}

	malformed := report
	malformed.GatewayPoolID = "not-a-uuid"
	malformed.RuntimeSequence = 2
	malformed.BootID = "boot-pool-malformed"
	malformed.ObservedAt = now.Add(time.Second)
	malformedRaw, err := json.Marshal(malformed)
	if err != nil {
		t.Fatal(err)
	}
	malformedTS, malformedNonce, malformedSig, err := SignRuntimeReport(secret, now.Add(time.Second), "nonce-pg-pool-malformed-123", malformedRaw)
	if err != nil {
		t.Fatal(err)
	}
	svc.Clock = func() time.Time { return now.Add(time.Second) }
	if _, err := svc.Register(ctx, node.ID, malformedTS, malformedNonce, malformedSig, malformedRaw); err == nil {
		t.Fatal("malformed gateway pool identifier report was accepted")
	}
	if _, err := svc.Register(ctx, node.ID, malformedTS, malformedNonce, malformedSig, malformedRaw); !errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("replayed malformed-pool report was not fenced: %v", err)
	}
	var malformedDeclaredPoolID, malformedReason string
	if err := db.QueryRowContext(ctx, `SELECT coalesce(runtime_identity->>'declaredGatewayPoolId',''),reason FROM gateway_runtime_events WHERE node_id=$1::uuid AND event_type='REJECTED' AND runtime_identity->>'declaredGatewayPoolId'='not-a-uuid' ORDER BY occurred_at DESC,id DESC LIMIT 1`, node.ID).Scan(&malformedDeclaredPoolID, &malformedReason); err != nil {
		t.Fatalf("malformed-pool rejection was not durably audited: %v", err)
	}
	if malformedDeclaredPoolID != "not-a-uuid" || malformedReason != "gateway pool unavailable" {
		t.Fatalf("unexpected malformed-pool rejection evidence: declared=%s reason=%q", malformedDeclaredPoolID, malformedReason)
	}
}

func TestPostgreSQLUnassignedNodePoolMissRejectionIsAuditable(t *testing.T) {
	dsn := os.Getenv("POSTGRES_GATEWAY_RUNTIME_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_GATEWAY_RUNTIME_DATABASE_URL is not set")
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
	var actorID, missingPoolID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actorID, &missingPoolID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Unassigned Runtime Actor','DISABLED',false)`, actorID, "runtime-unassigned-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLGovernanceStore{DB: db}
	node, err := (&GovernanceService{Store: store}).RegisterNode(ctx, Node{Name: "runtime-unassigned-node-" + actorID, Status: "OFFLINE"}, actorID, "unassigned runtime rejection integration")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_nonces WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_events WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_governance_events WHERE object_type='NODE' AND object_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	now := time.Now().UTC().Truncate(time.Second)
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: missingPoolID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "integration-1", GatewayVersion: "0.8.28", WorkerVersion: "node-22", ConfigurationVersion: "cfg-unassigned", BootID: "boot-unassigned", RuntimeSequence: 1, InternalURL: "https://gateway.integration.invalid", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, err := SignRuntimeReport(secret, now, "nonce-pg-unassigned-1234567", raw)
	if err != nil {
		t.Fatal(err)
	}
	svc := &RuntimeRegistrationService{Store: store, GatewayPools: store, Secret: secret, AllowedInternalHosts: []string{"gateway.integration.invalid"}, MaximumSkew: time.Minute, Clock: func() time.Time { return now }}
	if _, err := svc.Register(ctx, node.ID, ts, nonce, sig, raw); err == nil {
		t.Fatal("unassigned node's nonexistent gateway pool report was accepted")
	}
	if _, err := svc.Register(ctx, node.ID, ts, nonce, sig, raw); !errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("replayed unassigned-node rejection was not fenced: %v", err)
	}

	var poolAnchorPresent bool
	var declaredPoolID, reason string
	if err := db.QueryRowContext(ctx, `SELECT gateway_pool_id IS NOT NULL,coalesce(runtime_identity->>'declaredGatewayPoolId',''),reason FROM gateway_runtime_events WHERE node_id=$1::uuid AND event_type='REJECTED' ORDER BY occurred_at DESC,id DESC LIMIT 1`, node.ID).Scan(&poolAnchorPresent, &declaredPoolID, &reason); err != nil {
		t.Fatalf("unassigned-node rejection was not durably audited: %v", err)
	}
	if poolAnchorPresent || declaredPoolID != missingPoolID || reason != "gateway pool unavailable" {
		t.Fatalf("unexpected unassigned-node rejection evidence: anchor=%v declared=%s reason=%q", poolAnchorPresent, declaredPoolID, reason)
	}
}
