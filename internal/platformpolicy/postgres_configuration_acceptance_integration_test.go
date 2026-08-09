package platformpolicy

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLConfigurationLifecycleIsVersionedEffectiveDatedAndRollbackable(t *testing.T) {
	dsn := os.Getenv("POSTGRES_CONFIGURATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_CONFIGURATION_DATABASE_URL is not set")
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
	var maker, submitter, approver string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&maker, &submitter, &approver); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2099, 11, 1, 10, 0, 0, 0, time.UTC)
	admin := &ConfigurationAdministration{Store: &PostgreSQLStore{DB: db}, Clock: func() time.Time { return base }}
	activate := func(input Configuration, createActor string) Configuration {
		t.Helper()
		created, err := admin.Create(ctx, input, createActor, "create governed configuration version")
		if err != nil {
			t.Fatal(err)
		}
		submitted, err := admin.Submit(ctx, created.ID, created.Version, submitter, "submit governed configuration version")
		if err != nil {
			t.Fatal(err)
		}
		active, err := admin.Decide(ctx, submitted.ID, submitted.Version, true, approver, "approve governed configuration version")
		if err != nil {
			t.Fatal(err)
		}
		return active
	}

	first := activate(Configuration{Key: "ADM.TEST.LIMIT", ScopeType: ScopePlatform, Value: json.RawMessage(`{"limit":10}`), EffectiveFrom: base}, maker)
	if first.Status != StatusActive || first.Version != 3 {
		t.Fatalf("unexpected first configuration: %+v", first)
	}
	second := activate(Configuration{Key: "ADM.TEST.LIMIT", ScopeType: ScopePlatform, Value: json.RawMessage(`{"limit":20}`), EffectiveFrom: base.Add(time.Hour)}, maker)
	resolvedBefore, err := admin.Resolve(ctx, "ADM.TEST.LIMIT", nil, base.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if resolvedBefore.ID != first.ID {
		t.Fatalf("before replacement resolved=%s want=%s", resolvedBefore.ID, first.ID)
	}
	resolvedAfter, err := admin.Resolve(ctx, "ADM.TEST.LIMIT", nil, base.Add(90*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if resolvedAfter.ID != second.ID {
		t.Fatalf("after replacement resolved=%s want=%s", resolvedAfter.ID, second.ID)
	}

	rollback, err := admin.Rollback(ctx, first.ID, maker, "restore previously approved configuration deterministically", base.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	rollback, err = admin.Submit(ctx, rollback.ID, rollback.Version, submitter, "submit rollback configuration version")
	if err != nil {
		t.Fatal(err)
	}
	rollback, err = admin.Decide(ctx, rollback.ID, rollback.Version, true, approver, "approve rollback configuration version")
	if err != nil {
		t.Fatal(err)
	}
	if rollback.RollbackOfID != first.ID || rollback.SupersedesID != first.ID || rollback.Status != StatusActive {
		t.Fatalf("rollback evidence missing: %+v", rollback)
	}
	resolvedRollback, err := admin.Resolve(ctx, "ADM.TEST.LIMIT", nil, base.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var rollbackValue struct {
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(resolvedRollback.Value, &rollbackValue); err != nil {
		t.Fatal(err)
	}
	if resolvedRollback.ID != rollback.ID || rollbackValue.Limit != 10 {
		t.Fatalf("rollback resolved incorrectly: %+v value=%+v", resolvedRollback, rollbackValue)
	}
	events, err := admin.Events(ctx, rollback.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 {
		t.Fatalf("rollback lifecycle events=%d want at least 3", len(events))
	}
	if _, err := admin.Create(ctx, Configuration{Key: "ADM.TEST.INVALID", ScopeType: ScopePlatform, Value: json.RawMessage(`not-json`)}, maker, "reject invalid configuration value"); err == nil {
		t.Fatal("invalid configuration JSON was accepted")
	}
}
