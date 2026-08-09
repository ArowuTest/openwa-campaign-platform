package dispatch

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLGatewayAuthorityRejectsConflictingOwnerAndAuditsRejection(t *testing.T) {
	dsn := os.Getenv("POSTGRES_GATEWAY_AUTHORITY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_GATEWAY_AUTHORITY_DATABASE_URL is not set")
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
	var poolID, sessionID, nodeA, nodeB string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(
		&poolID, &sessionID, &nodeA, &nodeB,
	); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_session_authority_events WHERE session_id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_session_authorities WHERE session_id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, sessionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id IN ($1::uuid,$2::uuid)`, nodeA, nodeB)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pools WHERE id=$1::uuid`, poolID)
	}()

	if _, err := db.ExecContext(ctx, `INSERT INTO gateway_pools(
		id,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes
	) VALUES($1::uuid,$2,'OPENWA','BAILEYS','0.13.0+platform.1','ACTIVE','["SEND_TEXT"]'::jsonb,1)`,
		poolID, "gw-authority-"+poolID); err != nil {
		t.Fatal(err)
	}
	for _, node := range []struct{ id, name string }{{nodeA, "authority-a"}, {nodeB, "authority-b"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO sender_nodes(id,name,status,capacity,gateway_pool_id)
			VALUES($1::uuid,$2,'READY',1,$3::uuid)`, node.id, node.name+"-"+node.id, poolID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_sessions(
		id,node_id,encrypted_msisdn,masked_msisdn,engine_type,status,safe_messages_per_minute,safe_daily_capacity,gateway_pool_id
	) VALUES($1::uuid,$2::uuid,decode('00','hex'),'***9007','BAILEYS','READY',10,100,$3::uuid)`,
		sessionID, nodeA, poolID); err != nil {
		t.Fatal(err)
	}

	loader := &PostgreSQLMaterialLoader{DB: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	current := governedRoute{
		GatewayPoolID: poolID, GatewayPoolVersion: 1,
		Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "0.13.0+platform.1",
		GatewayNodeID: nodeA, GatewayNodeVersion: 1,
		SessionLeaseVersion: 2, SessionConfigurationVersion: 3,
		AuthorityExpiresAt: now.Add(5 * time.Minute),
	}
	if err := loader.persistGatewayAuthority(ctx, sessionID, "campaign-1:recipient-1", current, now); err != nil {
		t.Fatalf("persist current authority: %v", err)
	}
	conflict := current
	conflict.GatewayNodeID = nodeB
	conflict.GatewayNodeVersion = 2
	conflictErr := loader.persistGatewayAuthority(ctx, sessionID, "campaign-1:recipient-1", conflict, now.Add(time.Second))
	if conflictErr == nil {
		t.Fatal("conflicting owner unexpectedly replaced the current gateway authority")
	}
	t.Logf("conflicting authority rejected with: %v", conflictErr)

	var owner string
	if err := db.QueryRowContext(ctx, `SELECT owner_node_id::text FROM gateway_session_authorities WHERE session_id=$1::uuid`, sessionID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != nodeA {
		t.Fatalf("live authority owner changed after rejected conflict: got %s want %s", owner, nodeA)
	}

	var issued, rejected int
	if err := db.QueryRowContext(ctx, `SELECT
		count(*) FILTER (WHERE event_type='ISSUED'),
		count(*) FILTER (WHERE event_type='REJECTED')
		FROM gateway_session_authority_events WHERE session_id=$1::uuid`, sessionID).Scan(&issued, &rejected); err != nil {
		t.Fatal(err)
	}
	if issued != 1 || rejected != 1 {
		t.Fatalf("authority audit events issued=%d rejected=%d, want 1/1", issued, rejected)
	}
}
