package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestControlSchemaReadinessRequires0085CampaignFrozenEvidenceSemantics(t *testing.T) {
	preDSN := os.Getenv("POSTGRES_CONTROL_PRE_0085_DATABASE_URL")
	currentDSN := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if preDSN == "" || currentDSN == "" {
		t.Skip("0085 schema readiness DSNs are not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pre, err := sql.Open("postgres", preDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer pre.Close()
	if err := verifyControlSchema(ctx, pre); err == nil {
		t.Fatal("control schema accepted an 0084 database without 0085 campaign frozen-evidence semantics")
	}
	current, err := sql.Open("postgres", currentDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if err := verifyControlSchema(ctx, current); err != nil {
		t.Fatalf("0085 schema rejected: %v", err)
	}
}
