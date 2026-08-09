package operations

import (
	_ "campaign-platform/internal/persistence/database"
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestPostgreSQLDashboardMarksOnlyNodesBeyondResolvedStaleThresholdUnavailable(t *testing.T) {
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

	now := time.Date(2099, 6, 1, 12, 0, 0, 0, time.UTC)
	repo := &PostgreSQLRepository{DB: db, RuntimeHealth: staticGatewayRuntimeHealthResolver{policy: GatewayRuntimeHealthPolicy{
		StaleAfter: 3 * time.Minute, Source: "GOVERNED_CONFIGURATION", ConfigurationID: "integration-policy", Version: 9,
	}}}
	before, err := repo.Dashboard(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	var freshID, staleID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&freshID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&staleID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id IN ($1::uuid,$2::uuid)`, freshID, staleID)
	}()

	if _, err := db.ExecContext(ctx, `INSERT INTO sender_nodes(id,name,status,last_heartbeat_at) VALUES
		($1::uuid,$2,'READY',$3),($4::uuid,$5,'READY',$6)`,
		freshID, "runtime-threshold-fresh-"+freshID, now.Add(-179*time.Second),
		staleID, "runtime-threshold-stale-"+staleID, now.Add(-181*time.Second)); err != nil {
		t.Fatal(err)
	}
	after, err := repo.Dashboard(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if after.GatewayStaleAfterSeconds != 180 || after.GatewayRuntimeHealthConfigurationID != "integration-policy" || after.GatewayRuntimeHealthConfigurationVersion != 9 {
		t.Fatalf("resolved runtime-health evidence missing from dashboard: %+v", after)
	}
	if after.StaleWorkerNodes != before.StaleWorkerNodes+1 {
		t.Fatalf("stale count changed by %d, want 1 (before=%d after=%d)", after.StaleWorkerNodes-before.StaleWorkerNodes, before.StaleWorkerNodes, after.StaleWorkerNodes)
	}
	if after.UnavailableGatewayNodes != before.UnavailableGatewayNodes+1 {
		t.Fatalf("unavailable count changed by %d, want 1 (before=%d after=%d)", after.UnavailableGatewayNodes-before.UnavailableGatewayNodes, before.UnavailableGatewayNodes, after.UnavailableGatewayNodes)
	}
}
