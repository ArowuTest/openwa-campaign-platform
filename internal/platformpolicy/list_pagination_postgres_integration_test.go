package platformpolicy

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLGovernedListsContinueWithoutSkipping(t *testing.T) {
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

	var actorID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Platform Policy Pagination','DISABLED',false)`, actorID, "platform-policy-pagination-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}

	store := &PostgreSQLStore{DB: db}
	configAdmin := &ConfigurationAdministration{Store: store}
	maintenanceAdmin := &MaintenanceAdministration{Store: store}
	base := time.Now().UTC().AddDate(1000, 0, 0).Truncate(time.Microsecond)
	key := "TEST.PAGINATION." + strings.ToUpper(strings.ReplaceAll(actorID, "-", ""))
	configs := make([]Configuration, 0, 3)
	windows := make([]MaintenanceWindow, 0, 3)
	for index := 0; index < 3; index++ {
		at := base.Add(time.Duration(index) * time.Minute)
		configAdmin.Clock = func() time.Time { return at }
		created, createErr := configAdmin.Create(ctx, Configuration{Key: key, ScopeType: ScopePlatform, Value: json.RawMessage(`{"enabled":true}`)}, actorID, "pagination integration")
		if createErr != nil {
			t.Fatal(createErr)
		}
		configs = append(configs, created)
		maintenanceAdmin.Clock = func() time.Time { return at }
		window, createErr := maintenanceAdmin.Create(ctx, MaintenanceWindow{Name: "Pagination Window " + created.ID, Mode: MaintenanceReadOnly, ScopeType: ScopePlatform}, actorID, "pagination integration")
		if createErr != nil {
			t.Fatal(createErr)
		}
		windows = append(windows, window)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM platform_configuration_events WHERE configuration_id IN ($1::uuid,$2::uuid,$3::uuid)`, configs[0].ID, configs[1].ID, configs[2].ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM maintenance_window_events WHERE maintenance_window_id IN ($1::uuid,$2::uuid,$3::uuid)`, windows[0].ID, windows[1].ID, windows[2].ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM platform_configurations WHERE id IN ($1::uuid,$2::uuid,$3::uuid)`, configs[0].ID, configs[1].ID, configs[2].ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM maintenance_windows WHERE id IN ($1::uuid,$2::uuid,$3::uuid)`, windows[0].ID, windows[1].ID, windows[2].ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()

	firstConfigurations, err := configAdmin.ListPage(ctx, ConfigurationQuery{Key: key, ScopeType: ScopePlatform, Limit: 2}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(firstConfigurations.Items) != 2 || firstConfigurations.NextCursor == "" || firstConfigurations.Items[0].ID != configs[2].ID || firstConfigurations.Items[1].ID != configs[1].ID {
		t.Fatalf("unexpected first configuration page: %#v", firstConfigurations)
	}
	secondConfigurations, err := configAdmin.ListPage(ctx, ConfigurationQuery{Key: key, ScopeType: ScopePlatform, Limit: 2}, firstConfigurations.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondConfigurations.Items) != 1 || secondConfigurations.NextCursor != "" || secondConfigurations.Items[0].ID != configs[0].ID {
		t.Fatalf("unexpected second configuration page: %#v", secondConfigurations)
	}

	firstMaintenance, err := maintenanceAdmin.ListPage(ctx, MaintenanceDraft, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(firstMaintenance.Items) != 2 || firstMaintenance.NextCursor == "" || firstMaintenance.Items[0].ID != windows[2].ID || firstMaintenance.Items[1].ID != windows[1].ID {
		t.Fatalf("unexpected first maintenance page: %#v", firstMaintenance)
	}
	secondMaintenance, err := maintenanceAdmin.ListPage(ctx, MaintenanceDraft, 2, firstMaintenance.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondMaintenance.Items) == 0 || secondMaintenance.Items[0].ID != windows[0].ID {
		t.Fatalf("maintenance continuation skipped fixture: %#v", secondMaintenance)
	}
}
