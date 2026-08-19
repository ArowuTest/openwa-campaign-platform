package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLExportPersistsCriteriaAndFrozenPayload(t *testing.T) {
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
	var actorID, approverID, exportID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actorID, &approverID, &exportID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 3, 5, 12, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 export','DISABLED',false)`, actorID, "export-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 export approver','DISABLED',false)`, approverID, "export-approver-"+approverID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	repo := &PostgreSQLRepository{DB: db}
	request := ExportRequest{
		ID: exportID, Kind: "CAMPAIGN_REPORT", Format: "CSV", Status: ExportDraft,
		RequestedBy: actorID, Reason: "task 6 export persistence", Criteria: json.RawMessage(`{"campaignStatus":"COMPLETED"}`),
		TemplateVersion: "task6-v1", CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	created, err := repo.CreateExport(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.GetExport(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var initialCriteria map[string]any
	if err := json.Unmarshal(loaded.Criteria, &initialCriteria); err != nil {
		t.Fatal(err)
	}
	if initialCriteria["campaignStatus"] != "COMPLETED" {
		t.Fatalf("unexpected persisted criteria: %v", initialCriteria)
	}
	asOf := now.Add(time.Minute)
	expires := now.Add(time.Hour)
	loaded.Status = ExportApproved
	loaded.ApprovedBy = approverID
	loaded.Criteria = json.RawMessage(`{"campaignStatus":"COMPLETED","includeUnknown":true}`)
	loaded.FrozenPayload = json.RawMessage(`{"rows":10,"unknown":2}`)
	loaded.AsOf = &asOf
	loaded.ExpiresAt = &expires
	loaded.UpdatedAt = now.Add(2 * time.Minute)
	updated, err := repo.UpdateExport(ctx, loaded, 1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 {
		t.Fatalf("unexpected export version: %d", updated.Version)
	}
	persisted, err := repo.GetExport(ctx, exportID)
	if err != nil {
		t.Fatal(err)
	}
	var criteria, frozen map[string]any
	if err := json.Unmarshal(persisted.Criteria, &criteria); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(persisted.FrozenPayload, &frozen); err != nil {
		t.Fatal(err)
	}
	if criteria["includeUnknown"] != true || frozen["rows"] != float64(10) || frozen["unknown"] != float64(2) {
		t.Fatalf("unexpected persisted export JSON criteria=%v frozen=%v", criteria, frozen)
	}
}
