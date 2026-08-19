package platformpolicy

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestTask6ConcurrentConfigurationActivationHasOneEffectiveVersion(t *testing.T) {
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
	ids := make([]string, 3)
	args := []any{&ids[0], &ids[1], &ids[2]}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	makerA, makerB, checker := ids[0], ids[1], ids[2]
	for _, actor := range ids {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 configuration actor','DISABLED',false)`, actor, "task6-config-"+actor+"@internal.invalid"); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	store := &PostgreSQLStore{DB: db}
	admin := &ConfigurationAdministration{Store: store, Clock: func() time.Time { return now }}
	key := strings.ToUpper("TASK6.CONCURRENT." + ids[0][:8])
	create := func(actor string, enabled bool) Configuration {
		t.Helper()
		value, err := admin.Create(ctx, Configuration{Key: key, ScopeType: ScopePlatform, Value: json.RawMessage([]byte(`{"enabled":` + map[bool]string{true: "true", false: "false"}[enabled] + `}`)), EffectiveFrom: now}, actor, "task 6 concurrent configuration")
		if err != nil {
			t.Fatal(err)
		}
		value, err = admin.Submit(ctx, value.ID, value.Version, actor, "submit configuration for approval")
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	first := create(makerA, true)
	second := create(makerB, false)
	type outcome struct {
		value Configuration
		err   error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, value := range []Configuration{first, second} {
		value := value
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			approved, decideErr := admin.Decide(ctx, value.ID, value.Version, true, checker, "approve one effective configuration")
			results <- outcome{value: approved, err: decideErr}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	var winner Configuration
	for result := range results {
		if result.err == nil {
			successes++
			winner = result.value
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent activation successes=%d, want exactly 1", successes)
	}
	resolved, err := admin.Resolve(ctx, key, nil, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != winner.ID || resolved.Status != StatusActive {
		t.Fatalf("resolved configuration=%+v winner=%+v", resolved, winner)
	}
	if err := admin.ValidateResolvedChecksum(resolved); err != nil {
		t.Fatalf("resolved configuration checksum invalid: %v", err)
	}
	var effective int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM platform_configurations WHERE configuration_key=$1 AND scope_type='PLATFORM' AND scope_id='' AND status IN ('ACTIVE','SUPERSEDED') AND effective_from<=$2 AND (effective_to IS NULL OR effective_to>$2)`, key, now.Add(time.Second)).Scan(&effective); err != nil {
		t.Fatal(err)
	}
	if effective != 1 {
		t.Fatalf("effective configuration versions=%d want=1", effective)
	}
	var eventID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM platform_configuration_events WHERE configuration_id=$1::uuid ORDER BY occurred_at DESC,id DESC LIMIT 1`, winner.ID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE platform_configuration_events SET reason='tampered' WHERE id=$1::uuid`, eventID); err == nil {
		t.Fatal("configuration history mutation was accepted")
	}
}
