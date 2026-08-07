package sender

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/platformpolicy"
)

func TestPostgreSQLTransportRuntimePolicyResolvesApprovedSessionScope(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TRANSPORT_RUNTIME_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TRANSPORT_RUNTIME_DATABASE_URL is not set")
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

	ids := make([]string, 4)
	for i := range ids {
		if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	sessionID, makerID, submitterID, approverID := ids[0], ids[1], ids[2], ids[3]
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM platform_configuration_events WHERE configuration_id IN (SELECT id FROM platform_configurations WHERE configuration_key=$1 AND scope_type='SENDER_SESSION' AND scope_id=$2)`, TransportRuntimeConfigurationKey, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM platform_configurations WHERE configuration_key=$1 AND scope_type='SENDER_SESSION' AND scope_id=$2`, TransportRuntimeConfigurationKey, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id IN ($1::uuid,$2::uuid,$3::uuid)`, makerID, submitterID, approverID)
	}()
	for i, id := range []string{makerID, submitterID, approverID} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,$3,'DISABLED',false)`, id, "transport-policy-"+id+"@internal.invalid", "Transport Policy Test Actor"); err != nil {
			t.Fatalf("insert actor %d: %v", i, err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	store := &platformpolicy.PostgreSQLStore{DB: db}
	admin := &platformpolicy.ConfigurationAdministration{Store: store, Clock: func() time.Time { return now }}
	raw := json.RawMessage(`{"reconnectMode":"BOUNDED","reconnectMaxAttempts":6,"reconnectBaseDelayMs":8000,"reconnectStabilityResetMs":300000,"watchdogProbeTimeoutMs":10000,"watchdogFailureThreshold":3,"engineTeardownTimeoutMs":40000}`)
	created, err := admin.Create(ctx, platformpolicy.Configuration{Key: TransportRuntimeConfigurationKey, ScopeType: platformpolicy.ScopeSenderSession, ScopeID: sessionID, Value: raw}, makerID, "integration transport runtime policy")
	if err != nil {
		t.Fatalf("create policy: %v", err)
	}
	created, err = admin.Submit(ctx, created.ID, created.Version, submitterID, "submit integration transport runtime policy")
	if err != nil {
		t.Fatalf("submit policy: %v", err)
	}
	created, err = admin.Decide(ctx, created.ID, created.Version, true, approverID, "approve integration transport runtime policy")
	if err != nil {
		t.Fatalf("approve policy: %v", err)
	}

	resolver := &PlatformTransportRuntimeResolver{Configurations: admin}
	policy, err := resolver.ResolveTransportRuntime(ctx, GovernedSession{ID: sessionID}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("resolve policy: %v", err)
	}
	if policy == nil {
		t.Fatal("approved PostgreSQL transport policy did not resolve")
	}
	if policy.ConfigurationID != created.ID || policy.ScopeType != string(platformpolicy.ScopeSenderSession) || policy.ScopeID != sessionID || policy.Version != created.Version {
		t.Fatalf("wrong resolved policy evidence: %+v", policy)
	}
	if policy.ReconnectMode != ReconnectBounded || policy.ReconnectMaxAttempts != 6 || policy.WatchdogFailureThreshold != 3 || policy.EngineTeardownTimeoutMs != 40000 {
		t.Fatalf("wrong resolved policy values: %+v", policy)
	}
}
