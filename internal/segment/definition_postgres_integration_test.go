package segment

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	audiencefilter "campaign-platform/internal/audience/filter"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLSegmentDefinitionPersistsJSONAndVersionHistory(t *testing.T) {
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
	var orgID, actorID, segmentID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&orgID, &actorID, &segmentID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Task 6 segment "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 segment','DISABLED',false)`, actorID, "segment-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	definition := audiencefilter.Group{
		Join: audiencefilter.JoinAnd,
		Rules: []audiencefilter.Rule{{
			DefinitionCode: "COUNTRY",
			Operator:       audiencefilter.OperatorIn,
			Values:         []any{"NG"},
		}},
	}
	now := time.Date(2099, 3, 2, 14, 0, 0, 0, time.UTC)
	repo := &PostgreSQLDefinitionRepository{DB: db}
	created := Definition{
		ID: segmentID, OrganisationID: orgID, Name: "Nigeria audience",
		Definition: definition, Status: StatusActive, Version: 1,
		CreatedBy: actorID, UpdatedBy: actorID, CreatedAt: now, UpdatedAt: now,
	}
	stored, err := repo.Create(ctx, created, "task six segment create")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.Get(ctx, stored.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Definition.Rules[0].DefinitionCode != "COUNTRY" {
		t.Fatalf("unexpected loaded segment: %#v", loaded)
	}
	loaded.Name = "Nigeria audience updated"
	loaded.Version++
	loaded.UpdatedAt = now.Add(time.Minute)
	updated, err := repo.Update(ctx, loaded, 1, "task six segment update")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 {
		t.Fatalf("updated version=%d", updated.Version)
	}
	versions, err := repo.Versions(ctx, updated.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("unexpected segment versions: %#v", versions)
	}
}
