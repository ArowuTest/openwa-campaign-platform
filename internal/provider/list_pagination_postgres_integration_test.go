package provider

import (
	_ "campaign-platform/internal/persistence/database"
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestPostgreSQLProviderDefinitionsPaginationContinuesWithoutSkipping(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Provider Pagination','DISABLED',false)`, actor, "provider-pagination-"+actor+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLStore{DB: db}
	service := &Service{Store: store}
	base := time.Date(2099, 1, 1, 12, 0, 0, 0, time.UTC)
	created := make([]Definition, 0, 3)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		service.Clock = func() time.Time { return at }
		v, e := service.CreateDraft(ctx, Definition{Provider: "OPENWA", Channel: ChannelWhatsApp, Engine: "BAILEYS", AdapterVersion: "pagination", Capabilities: []Capability{CapabilitySendText}}, actor, "pagination integration")
		if e != nil {
			t.Fatal(e)
		}
		created = append(created, v)
	}
	want := map[string]int{created[2].ID: 0, created[1].ID: 1, created[0].ID: 2}
	seen := make([]string, 0, 3)
	cursor := ""
	visited := map[string]struct{}{}
	for pageNumber := 0; pageNumber < 1000 && len(seen) < len(want); pageNumber++ {
		page, err := service.ListPage(ctx, 2, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if _, duplicate := visited[item.ID]; duplicate {
				t.Fatalf("provider pagination repeated definition %s", item.ID)
			}
			visited[item.ID] = struct{}{}
			if _, expected := want[item.ID]; expected {
				seen = append(seen, item.ID)
			}
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			t.Fatal("provider pagination cursor did not advance")
		}
		cursor = page.NextCursor
	}
	if len(seen) != 3 || seen[0] != created[2].ID || seen[1] != created[1].ID || seen[2] != created[0].ID {
		t.Fatalf("provider pagination skipped or reordered test definitions: seen=%v want=%v", seen, []string{created[2].ID, created[1].ID, created[0].ID})
	}
}
