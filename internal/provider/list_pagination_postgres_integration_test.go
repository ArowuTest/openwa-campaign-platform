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
	first, err := service.ListPage(ctx, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != created[2].ID || first.Items[1].ID != created[1].ID {
		t.Fatalf("unexpected first provider page: %#v", first)
	}
	second, err := service.ListPage(ctx, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID != created[0].ID {
		t.Fatalf("unexpected second provider page: %#v", second)
	}
}
