package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestControlSchemaReadinessRejectsDisabledRouteOrganisationTrigger(t *testing.T) {
	dsn := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_META_CLOUD_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `ALTER TABLE campaign_routing_plan_pools DISABLE TRIGGER trg_campaign_route_pool_organisation`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer restoreCancel()
		if _, restoreErr := db.ExecContext(restoreCtx, `ALTER TABLE campaign_routing_plan_pools ENABLE TRIGGER trg_campaign_route_pool_organisation`); restoreErr != nil {
			t.Errorf("restore route organisation trigger: %v", restoreErr)
			return
		}
		var enabled string
		if restoreErr := db.QueryRowContext(restoreCtx, `SELECT tgenabled::text FROM pg_trigger WHERE tgname='trg_campaign_route_pool_organisation' AND tgrelid='campaign_routing_plan_pools'::regclass`).Scan(&enabled); restoreErr != nil {
			t.Errorf("verify restored route organisation trigger: %v", restoreErr)
		} else if enabled != "O" {
			t.Errorf("route organisation trigger restored with tgenabled=%q want O", enabled)
		}
	}()
	if err := verifyControlSchema(ctx, db); err == nil {
		t.Fatal("control schema reported ready with route organisation trigger disabled")
	}
}
