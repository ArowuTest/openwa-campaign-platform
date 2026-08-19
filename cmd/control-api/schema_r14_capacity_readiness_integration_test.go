package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestControlSchemaReadinessRequiresLiveCapacityNoTruncate(t *testing.T) {
	dsn := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_META_CLOUD_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `ALTER TABLE campaign_pool_capacity_reservations DISABLE TRIGGER trg_capacity_reservation_no_truncate`); err != nil {
		t.Fatal(err)
	}
	restored := false
	defer func() {
		if !restored {
			_, _ = db.ExecContext(context.Background(), `ALTER TABLE campaign_pool_capacity_reservations ENABLE TRIGGER trg_capacity_reservation_no_truncate`)
		}
	}()
	if err := verifyControlSchema(ctx, db); err == nil {
		t.Fatal("control schema reported ready with live capacity TRUNCATE fence disabled")
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE campaign_pool_capacity_reservations ENABLE TRIGGER trg_capacity_reservation_no_truncate`); err != nil {
		t.Fatal(err)
	}
	restored = true
	if err := verifyControlSchema(ctx, db); err != nil {
		t.Fatalf("restored live capacity TRUNCATE fence rejected: %v", err)
	}
}
