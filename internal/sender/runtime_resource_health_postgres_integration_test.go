package sender

import (
	"context"
	"database/sql"
	"os"
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
		BootID: "boot-health-pg", InternalURL: "https://gateway.integration.invalid",
		Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady,
		Capacity: 3, SessionCount: 2, QueueDepth: 4, CPUPercent: 12.25, MemoryBytes: 123456,
		ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now,
	}
	stored, err := store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != node.Version {
		t.Fatalf("runtime report changed governed node version: got %d want %d", stored.Version, node.Version)
	}
	report.ObservedAt = now.Add(time.Second)
	report.QueueDepth = 5
	stored, err = store.ApplyRuntimeReport(ctx, node.ID, node.Version, report, "", report.ObservedAt)
	if err != nil {
		t.Fatalf("second runtime report at governed node version was rejected: %v", err)
	}
	if stored.Version != node.Version || stored.QueueDepth != 5 {
		t.Fatalf("second runtime report did not preserve governance version and telemetry: %#v", stored)
	}
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
}
