package metacloud

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLMetaSenderLifecycleAndSecretFreeSchema(t *testing.T) {
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
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	var orgID, poolID, makerID, submitterID, checkerID, workerID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&orgID, &poolID, &makerID, &submitterID, &checkerID, &workerID); err != nil {
		t.Fatal(err)
	}
	suffix := orgID[:8]
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Meta test "+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',100,10000)`, poolID, "meta-pool-"+suffix, orgID); err != nil {
		t.Fatal(err)
	}
	for i, actor := range []string{makerID, submitterID, checkerID, workerID} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status) VALUES($1::uuid,$2,$3,'ACTIVE')`, actor, "meta-"+actor[:8]+"@example.test", "Meta Actor "+string(rune('A'+i))); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_sender_events WHERE sender_id IN (SELECT id FROM meta_cloud_senders WHERE organisation_id=$1::uuid)`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM meta_cloud_senders WHERE organisation_id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id IN ($1::uuid,$2::uuid,$3::uuid,$4::uuid)`, makerID, submitterID, checkerID, workerID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	}()

	store := &PostgreSQLStore{DB: db}
	now := time.Date(2026, 8, 11, 19, 15, 0, 0, time.UTC)
	svc := &Service{Store: store, Clock: func() time.Time { return now }}
	input := validSender()
	input.OrganisationID, input.SenderPoolID = orgID, poolID
	input.WABAID = "waba-" + suffix
	input.PhoneNumberID = "phone-" + suffix
	input.CredentialKey = "meta-test-" + suffix
	draft, err := svc.CreateDraft(ctx, input, makerID, "create Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := svc.Submit(ctx, draft.ID, draft.Version, submitterID, "submit Meta sender")
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.Decide(ctx, pending.ID, pending.Version, true, checkerID, "approve Meta sender", now)
	if err != nil {
		t.Fatal(err)
	}
	healthy, err := svc.ObserveHealth(ctx, active.ID, active.Version, HealthHealthy, now.Add(time.Minute), workerID, "Meta sender verified")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ObserveHealth(ctx, healthy.ID, active.Version, HealthDegraded, now.Add(2*time.Minute), workerID, "stale version should fail"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale version error=%v want conflict", err)
	}
	mutated := healthy
	mutated.WABAID = "waba-attacker"
	mutated.PhoneNumberID = "phone-attacker"
	mutated.CredentialKey = "meta-attacker"
	mutated.Version++
	mutated.UpdatedAt = now.Add(3 * time.Minute)
	if _, err = store.CompareAndSwap(ctx, mutated, healthy.Version, workerID, "IDENTITY_MUTATION", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("sender identity mutation error=%v want conflict", err)
	}
	unchanged, err := store.Get(ctx, healthy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.WABAID != healthy.WABAID || unchanged.PhoneNumberID != healthy.PhoneNumberID || unchanged.CredentialKey != healthy.CredentialKey || unchanged.Version != healthy.Version {
		t.Fatalf("sender identity mutated despite rejection: %#v", unchanged)
	}
	events, err := svc.ListEvents(ctx, healthy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[len(events)-1].ActorID != workerID {
		t.Fatalf("unexpected lifecycle events: %#v", events)
	}
	var secretColumns int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='meta_cloud_senders' AND column_name IN ('access_token','app_secret','verify_token')`).Scan(&secretColumns); err != nil {
		t.Fatal(err)
	}
	if secretColumns != 0 {
		t.Fatalf("Meta secret columns persisted in PostgreSQL: %d", secretColumns)
	}
	var credentialKey string
	if err := db.QueryRowContext(ctx, `SELECT credential_key FROM meta_cloud_senders WHERE id=$1::uuid`, healthy.ID).Scan(&credentialKey); err != nil {
		t.Fatal(err)
	}
	if credentialKey != input.CredentialKey {
		t.Fatalf("credential key=%q want %q", credentialKey, input.CredentialKey)
	}
}
