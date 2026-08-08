package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/identity"
	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/sender"
)

func openFinalInventoryDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(func() { cancel(); db.Close() })
	if err = db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func finalInventoryUUID(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var id string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedFinalInventoryActor(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	id := finalInventoryUUID(t, ctx, db)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required,created_at,updated_at) VALUES($1::uuid,$2,'Final Inventory Actor','DISABLED',false,now(),now())`, id, "final-inventory-actor-"+id+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPostgreSQLInternalUserPaginationContinuesWithoutSkipping(t *testing.T) {
	db, ctx := openFinalInventoryDB(t)
	base := time.Now().UTC().AddDate(1000, 0, 0).Truncate(time.Microsecond)
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		ids[i] = finalInventoryUUID(t, ctx, db)
		at := base.Add(time.Duration(i) * time.Minute)
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required,version,created_at,updated_at) VALUES($1::uuid,$2,$3,'ACTIVE',true,1,$4,$4)`, ids[i], "page-user-"+ids[i]+"@internal.invalid", "Page User", at); err != nil {
			t.Fatal(err)
		}
	}
	service := identity.NewAdministrationService(&IdentityAdministrationRepository{DB: db}, nil)
	first, err := service.ListPage(ctx, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != ids[2] || first.Items[1].ID != ids[1] {
		t.Fatalf("unexpected first internal-user page: %#v", first)
	}
	second, err := service.ListPage(ctx, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) == 0 || second.Items[0].ID != ids[0] {
		t.Fatalf("internal-user continuation skipped fixture: %#v", second)
	}
}

func TestPostgreSQLPacingPolicyPaginationContinuesWithoutSkipping(t *testing.T) {
	db, ctx := openFinalInventoryDB(t)
	actor := seedFinalInventoryActor(t, ctx, db)
	admin := &sender.PacingAdministration{Store: &PacingPolicyRepository{DB: db}}
	base := time.Now().UTC().AddDate(1000, 0, 0).Truncate(time.Microsecond)
	created := make([]sender.PacingPolicy, 0, 3)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i) * time.Minute)
		admin.Clock = func() time.Time { return at }
		v, err := admin.CreateDraft(ctx, sender.PacingPolicy{Scope: sender.PacingPlatform, MinimumDelayMS: 0, MaximumDelayMS: 1000, JitterMode: sender.JitterNone, MaxInFlight: 1, MessagesPerMinute: 10, HourlyAllowance: 100, DailyAllowance: 1000, BurstSize: 1, Overrides: []sender.MessageTypeOverride{}}, actor, "pagination pacing")
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, v)
	}
	first, err := admin.ListPage(ctx, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].ID != created[2].ID || first.Items[1].ID != created[1].ID {
		t.Fatalf("unexpected first pacing page: %#v", first)
	}
	second, err := admin.ListPage(ctx, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) == 0 || second.Items[0].ID != created[0].ID {
		t.Fatalf("unexpected second pacing page: %#v", second)
	}
}
