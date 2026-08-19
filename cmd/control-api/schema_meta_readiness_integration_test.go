package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestCouncilControlSchemaReadinessRequiresMetaHardening(t *testing.T) {
	preDSN := os.Getenv("POSTGRES_CONTROL_PRE_META_DATABASE_URL")
	currentDSN := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if preDSN == "" || currentDSN == "" {
		t.Skip("schema readiness DSNs are not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pre, err := sql.Open("postgres", preDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer pre.Close()
	if err := verifyControlSchema(ctx, pre); err == nil {
		t.Fatal("control schema accepted database without latest Meta hardening migration")
	}
	current, err := sql.Open("postgres", currentDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if err := verifyControlSchema(ctx, current); err != nil {
		t.Fatalf("latest Meta schema rejected: %v", err)
	}
}

func TestControlSchemaReadinessRejectsDisabledMetaEvidenceTrigger(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `ALTER TABLE meta_cloud_conversation_windows DISABLE TRIGGER trg_meta_conversation_window_insert_tombstone_guard`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer restoreCancel()
		if _, restoreErr := db.ExecContext(restoreCtx, `ALTER TABLE meta_cloud_conversation_windows ENABLE TRIGGER trg_meta_conversation_window_insert_tombstone_guard`); restoreErr != nil {
			t.Errorf("restore Meta retained-evidence insert guard: %v", restoreErr)
			return
		}
		var enabled string
		if restoreErr := db.QueryRowContext(restoreCtx, `SELECT tgenabled::text FROM pg_trigger WHERE tgname='trg_meta_conversation_window_insert_tombstone_guard' AND tgrelid='meta_cloud_conversation_windows'::regclass`).Scan(&enabled); restoreErr != nil {
			t.Errorf("verify restored Meta retained-evidence insert guard: %v", restoreErr)
		} else if enabled != "O" {
			t.Errorf("Meta retained-evidence insert guard restored with tgenabled=%q want O", enabled)
		}
	}()
	if err := verifyControlSchema(ctx, db); err == nil {
		t.Fatal("control schema reported ready with Meta retained-evidence insert guard disabled")
	}
}
