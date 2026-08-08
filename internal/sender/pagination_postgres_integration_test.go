package sender

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLSenderAndRuntimeHistoryPaginationContinuesWithoutDuplicates(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
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
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Pagination Test Actor','DISABLED',false)`, actorID, "pagination-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLGovernanceStore{DB: db}
	governance := &GovernanceService{Store: store}
	pool, err := (&GatewayPoolService{Store: store}).Create(ctx, GatewayPool{
		Name:     "pagination-" + actorID,
		Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys,
		AdapterVersion: "pagination-1", Status: GatewayPoolActive,
		Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1,
	}, actorID, "pagination integration")
	if err != nil {
		t.Fatal(err)
	}
	node, err := governance.RegisterNode(ctx, Node{
		Name: "pagination-node-" + actorID, Status: "OFFLINE",
		GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS",
		AdapterVersion: "pagination-1", InternalURL: "https://gateway.pagination.invalid",
	}, actorID, "pagination integration")
	if err != nil {
		t.Fatal(err)
	}

	sessions := make([]GovernedSession, 0, 3)
	maskedPrefix := "zzzz-pagination-" + actorID + "-"
	for index, masked := range []string{maskedPrefix + "01", maskedPrefix + "02", maskedPrefix + "03"} {
		session, createErr := governance.RegisterSession(ctx, GovernedSession{
			NodeID: node.ID, GatewayPoolID: pool.ID, MaskedMSISDN: masked,
			OwnerReference: "team-pagination", RegistrationCountryISO2: "NG",
			ProfileDisplayName: "Pagination Sender", RecoveryReference: "vault://pagination/recovery",
			EngineType: "BAILEYS", SafeMessagesPerMinute: 10 + index,
			SafeDailyCapacity: 1000, InFlightLimit: 1,
		}, []byte{1, 2, 3, byte(index + 1)}, actorID, "pagination integration")
		if createErr != nil {
			t.Fatal(createErr)
		}
		sessions = append(sessions, session)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_nonces WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_runtime_events WHERE node_id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_governance_events WHERE object_id IN ($1::uuid,$2::uuid,$3::uuid,$4::uuid)`, node.ID, sessions[0].ID, sessions[1].ID, sessions[2].ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id IN ($1::uuid,$2::uuid,$3::uuid)`, sessions[0].ID, sessions[1].ID, sessions[2].ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_nodes WHERE id=$1::uuid`, node.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pool_events WHERE gateway_pool_id=$1::uuid`, pool.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pools WHERE id=$1::uuid`, pool.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()

	startCursor, err := encodeOpaqueCursor(sessionPageCursor{MaskedMSISDN: maskedPrefix + "00", ID: "00000000-0000-4000-8000-000000000000"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := governance.ListSessionsPage(ctx, 2, startCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("unexpected first sender page: %#v", first)
	}
	second, err := governance.ListSessionsPage(ctx, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatalf("unexpected second sender page: %#v", second)
	}
	seenSessions := map[string]bool{}
	for _, item := range append(append([]GovernedSession{}, first.Items...), second.Items...) {
		if seenSessions[item.ID] {
			t.Fatalf("sender pagination duplicated %s", item.ID)
		}
		seenSessions[item.ID] = true
	}
	for _, session := range sessions {
		if !seenSessions[session.ID] {
			t.Fatalf("sender pagination skipped %s", session.ID)
		}
	}

	eventTime := time.Now().UTC().Truncate(time.Second)
	eventIDs := make([]string, 3)
	for index := range eventIDs {
		if err := db.QueryRowContext(ctx, `INSERT INTO gateway_runtime_events(node_id,gateway_pool_id,event_type,node_version,boot_id,runtime_identity,reason,occurred_at) VALUES($1::uuid,$2::uuid,'HEARTBEAT',$3,$4,'{}'::jsonb,'pagination integration',$5) RETURNING id::text`, node.ID, pool.ID, node.Version, "pagination-boot", eventTime).Scan(&eventIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	runtimeService := &RuntimeRegistrationService{Store: store}
	firstEvents, err := runtimeService.EventsPage(ctx, node.ID, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(firstEvents.Items) != 2 || firstEvents.NextCursor == "" {
		t.Fatalf("unexpected first runtime-event page: %#v", firstEvents)
	}
	secondEvents, err := runtimeService.EventsPage(ctx, node.ID, 2, firstEvents.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondEvents.Items) != 1 || secondEvents.NextCursor != "" {
		t.Fatalf("unexpected second runtime-event page: %#v", secondEvents)
	}
	seenEvents := map[string]bool{}
	for _, item := range append(append([]RuntimeEvent{}, firstEvents.Items...), secondEvents.Items...) {
		if seenEvents[item.ID] {
			t.Fatalf("runtime-event pagination duplicated %s", item.ID)
		}
		seenEvents[item.ID] = true
	}
	for _, eventID := range eventIDs {
		if !seenEvents[eventID] {
			t.Fatalf("runtime-event pagination skipped %s", eventID)
		}
	}

	poolEventTime := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	poolEventIDs := make([]string, 3)
	for index := range poolEventIDs {
		if err := db.QueryRowContext(ctx, `INSERT INTO gateway_pool_events(gateway_pool_id,event_type,version,actor_id,reason,evidence,occurred_at) VALUES($1::uuid,'CREATED',$2,$3::uuid,'pagination integration','{}'::jsonb,$4) RETURNING id::text`, pool.ID, int64(index+100), actorID, poolEventTime).Scan(&poolEventIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	poolAdmin := &GatewayPoolAdministration{Store: store}
	poolPage, err := poolAdmin.EventsPage(ctx, pool.ID, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	seenPoolEvents := map[string]bool{}
	for {
		for _, item := range poolPage.Items {
			seenPoolEvents[item.ID] = true
		}
		if poolPage.NextCursor == "" {
			break
		}
		poolPage, err = poolAdmin.EventsPage(ctx, pool.ID, 2, poolPage.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, eventID := range poolEventIDs {
		if !seenPoolEvents[eventID] {
			t.Fatalf("gateway-pool event pagination skipped %s", eventID)
		}
	}
}
