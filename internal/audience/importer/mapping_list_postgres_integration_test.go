package importer

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLMappingDefinitionsPaginationContinuesWithoutSkipping(t *testing.T) {
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
	var actor string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Mapping Pagination','DISABLED',false)`, actor, "mapping-pagination-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	source := "PAGINATION_" + strings.ToUpper(strings.ReplaceAll(actor, "-", ""))
	store := &PostgreSQLMappingStore{DB: db}
	admin := &MappingAdministration{Store: store}
	base := time.Date(2099, 1, 1, 14, 0, 0, 0, time.UTC)
	created := make([]MappingDefinition, 0, 3)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		admin.Clock = func() time.Time { return at }
		v, createErr := admin.Create(ctx, MappingDefinition{Name: "Pagination Mapping", SourceSystem: source, TemplateVersion: "v1", Mapping: ColumnMapping{MSISDN: "msisdn"}}, actor, "pagination integration", "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		created = append(created, v)
	}
	first, err := admin.ListPage(ctx, "", source, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != created[2].ID || first.Items[1].ID != created[1].ID {
		t.Fatalf("unexpected first mapping page: %#v", first)
	}
	second, err := admin.ListPage(ctx, "", source, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != created[0].ID {
		t.Fatalf("unexpected second mapping page: %#v", second)
	}
}
