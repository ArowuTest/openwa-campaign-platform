package sender

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func openSenderInventoryDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(func() { cancel(); db.Close() })
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}
func senderInventoryActor(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Sender Inventory Actor','DISABLED',false)`, id, "sender-inventory-"+id+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPostgreSQLSenderInventoriesContinueWithoutSkipping(t *testing.T) {
	db, ctx := openSenderInventoryDB(t)
	actor := senderInventoryActor(t, ctx, db)
	store := &PostgreSQLGovernanceStore{DB: db}
	gov := &GovernanceService{Store: store}
	pools := make([]Pool, 0, 3)
	for _, name := range []string{"zzzz-" + actor + "-pool-a", "zzzz-" + actor + "-pool-b", "zzzz-" + actor + "-pool-c"} {
		v, err := gov.CreatePool(ctx, Pool{Name: name, Status: "ACTIVE", MaxMessagesPerMinute: 10, DailyCapacity: 1000}, actor, "pagination sender pool")
		if err != nil {
			t.Fatal(err)
		}
		pools = append(pools, v)
	}
	poolStart, err := encodeInventoryCursor("zzzz-"+actor+"-pool-0", "00000000-0000-4000-8000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	firstPools, err := gov.ListPoolsPage(ctx, 2, poolStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPools.Items) != 2 || firstPools.NextCursor == "" || firstPools.Items[0].ID != pools[0].ID || firstPools.Items[1].ID != pools[1].ID {
		t.Fatalf("unexpected first sender-pool page: %#v", firstPools)
	}
	secondPools, err := gov.ListPoolsPage(ctx, 2, firstPools.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPools.Items) == 0 || secondPools.Items[0].ID != pools[2].ID {
		t.Fatalf("unexpected second sender-pool page: %#v", secondPools)
	}
	nodes := make([]Node, 0, 3)
	for _, name := range []string{"zzzz-" + actor + "-node-a", "zzzz-" + actor + "-node-b", "zzzz-" + actor + "-node-c"} {
		v, err := gov.RegisterNode(ctx, Node{Name: name, Status: "OFFLINE"}, actor, "pagination sender node")
		if err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, v)
	}
	nodeStart, err := encodeInventoryCursor("zzzz-"+actor+"-node-0", "00000000-0000-4000-8000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	firstNodes, err := gov.ListNodesPage(ctx, 2, nodeStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstNodes.Items) != 2 || firstNodes.NextCursor == "" || firstNodes.Items[0].ID != nodes[0].ID || firstNodes.Items[1].ID != nodes[1].ID {
		t.Fatalf("unexpected first sender-node page: %#v", firstNodes)
	}
	secondNodes, err := gov.ListNodesPage(ctx, 2, firstNodes.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondNodes.Items) == 0 || secondNodes.Items[0].ID != nodes[2].ID {
		t.Fatalf("unexpected second sender-node page: %#v", secondNodes)
	}
	gatewayAdmin := &GatewayPoolAdministration{Store: store}
	gateways := make([]GatewayPool, 0, 3)
	for _, name := range []string{"zzzz-" + actor + "-gateway-a", "zzzz-" + actor + "-gateway-b", "zzzz-" + actor + "-gateway-c"} {
		v, err := gatewayAdmin.Create(ctx, GatewayPool{Name: name, Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "pagination", Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, actor, "pagination gateway pool")
		if err != nil {
			t.Fatal(err)
		}
		gateways = append(gateways, v)
	}
	gatewayStart, err := encodeInventoryCursor("zzzz-"+actor+"-gateway-0", "00000000-0000-4000-8000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	firstGateways, err := gatewayAdmin.ListPage(ctx, 2, gatewayStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstGateways.Items) != 2 || firstGateways.NextCursor == "" || firstGateways.Items[0].ID != gateways[0].ID || firstGateways.Items[1].ID != gateways[1].ID {
		t.Fatalf("unexpected first gateway-pool page: %#v", firstGateways)
	}
	secondGateways, err := gatewayAdmin.ListPage(ctx, 2, firstGateways.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondGateways.Items) == 0 || secondGateways.Items[0].ID != gateways[2].ID {
		t.Fatalf("unexpected second gateway-pool page: %#v", secondGateways)
	}
}
