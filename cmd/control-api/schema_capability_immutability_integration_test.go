package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestSchemaCapabilityEvidenceIsImmutable(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `UPDATE platform_schema_capabilities SET evidence_version=2 WHERE capability='CAMPAIGN_FROZEN_EVIDENCE_NULL_HARDENING'`); err == nil {
		t.Fatal("schema capability evidence was mutable by UPDATE")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM platform_schema_capabilities WHERE capability='CAMPAIGN_FROZEN_EVIDENCE_NULL_HARDENING'`); err == nil {
		t.Fatal("schema capability evidence was mutable by DELETE")
	}
	if _, err := db.ExecContext(ctx, `TRUNCATE platform_schema_capabilities`); err == nil {
		t.Fatal("schema capability evidence was mutable by TRUNCATE")
	}
	var migration, evidence int
	if err := db.QueryRowContext(ctx, `SELECT source_migration,evidence_version FROM platform_schema_capabilities WHERE capability='CAMPAIGN_FROZEN_EVIDENCE_NULL_HARDENING'`).Scan(&migration, &evidence); err != nil {
		t.Fatal(err)
	}
	if migration != 85 || evidence != 1 {
		t.Fatalf("schema capability evidence changed: migration=%d evidence=%d", migration, evidence)
	}
}
