package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestControlSchemaReadinessRequiresAllLiveMetaWindowFences(t *testing.T) {
	dsn := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_META_CLOUD_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tests := []struct{ name, breakSQL, restoreSQL string }{
		{"live-update-immutability", `ALTER TABLE meta_cloud_conversation_windows DISABLE TRIGGER trg_meta_conversation_window_immutable`, `ALTER TABLE meta_cloud_conversation_windows ENABLE TRIGGER trg_meta_conversation_window_immutable`},
		{"delete-tombstone", `ALTER TABLE meta_cloud_conversation_windows DISABLE TRIGGER trg_meta_conversation_window_delete_tombstone`, `ALTER TABLE meta_cloud_conversation_windows ENABLE TRIGGER trg_meta_conversation_window_delete_tombstone`},
		{"replica-only-insert-guard", `ALTER TABLE meta_cloud_conversation_windows ENABLE REPLICA TRIGGER trg_meta_conversation_window_insert_tombstone_guard`, `ALTER TABLE meta_cloud_conversation_windows ENABLE TRIGGER trg_meta_conversation_window_insert_tombstone_guard`},
		{"live-window-no-truncate", `ALTER TABLE meta_cloud_conversation_windows DISABLE TRIGGER trg_meta_conversation_window_no_truncate`, `ALTER TABLE meta_cloud_conversation_windows ENABLE TRIGGER trg_meta_conversation_window_no_truncate`},
		{"release-delete-tombstone", `ALTER TABLE campaign_pool_capacity_reservations DISABLE TRIGGER trg_reservation_release_delete_tombstone`, `ALTER TABLE campaign_pool_capacity_reservations ENABLE TRIGGER trg_reservation_release_delete_tombstone`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.ExecContext(ctx, tc.breakSQL); err != nil {
				t.Fatal(err)
			}
			restored := false
			defer func() {
				if !restored {
					_, _ = db.ExecContext(context.Background(), tc.restoreSQL)
				}
			}()
			if err := verifyControlSchema(ctx, db); err == nil {
				t.Fatalf("control schema reported ready with broken fence %s", tc.name)
			}
			if _, err := db.ExecContext(ctx, tc.restoreSQL); err != nil {
				t.Fatal(err)
			}
			restored = true
			if err := verifyControlSchema(ctx, db); err != nil {
				t.Fatalf("restored schema rejected for %s: %v", tc.name, err)
			}
		})
	}
}
