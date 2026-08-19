package metacloud

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestCouncilPostgreSQLTemplateSyncRejectsOlderCatalogue(t *testing.T) {
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
	var orgID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Meta sync order "+orgID[:8]); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM meta_cloud_templates WHERE organisation_id=$1::uuid`, orgID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	}()
	store := &PostgreSQLTemplateStore{DB: db}
	waba := "waba-sync-" + orgID[:8]
	newerAt := time.Date(2026, 8, 13, 7, 0, 0, 0, time.UTC)
	olderAt := newerAt.Add(-time.Minute)
	newerComponents := []byte(`[{"type":"BODY","text":"newer"}]`)
	olderComponents := []byte(`[{"type":"BODY","text":"older"}]`)
	newerHash, err := CanonicalComponentHash(newerComponents)
	if err != nil {
		t.Fatal(err)
	}
	olderHash, err := CanonicalComponentHash(olderComponents)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceWABATemplates(ctx, orgID, waba, []Template{{MetaTemplateID: "tpl-1", Name: "hello", Language: "en_US", Category: "MARKETING", Status: "APPROVED", Components: newerComponents, ComponentHash: newerHash}}, newerAt); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceWABATemplates(ctx, orgID, waba, []Template{{MetaTemplateID: "tpl-1", Name: "hello", Language: "en_US", Category: "MARKETING", Status: "APPROVED", Components: olderComponents, ComponentHash: olderHash}}, olderAt); err != nil {
		t.Fatal(err)
	}
	current, err := store.FindApproved(ctx, orgID, waba, "hello", "en_US")
	if err != nil {
		t.Fatal(err)
	}
	if current.ComponentHash != newerHash || !current.LastSyncedAt.Equal(newerAt) {
		t.Fatalf("older catalogue replaced newer evidence: %#v", current)
	}
}
