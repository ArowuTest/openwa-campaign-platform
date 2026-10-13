package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestControlSchemaReadinessRejectsMissingCommercialEvidence(t *testing.T) {
	dsn := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_META_CLOUD_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := verifyControlSchema(ctx, db); err != nil {
		t.Fatalf("baseline commercial schema rejected: %v", err)
	}
	tests := []struct{ name, breakSQL string }{
		{"missing-events-relation", `ALTER TABLE public.campaign_commercial_events RENAME TO campaign_commercial_events_readiness_hidden`},
		{"missing-history-index", `DROP INDEX public.campaign_commercial_events_record_time_idx`},
		{"missing-append-only-trigger", `DROP TRIGGER campaign_commercial_events_append_only ON public.campaign_commercial_events`},
		{"disabled-append-only-trigger", `ALTER TABLE public.campaign_commercial_events DISABLE TRIGGER campaign_commercial_events_append_only`},
		{"replica-only-append-only-trigger", `ALTER TABLE public.campaign_commercial_events ENABLE REPLICA TRIGGER campaign_commercial_events_append_only`},
		{"missing-no-truncate-trigger", `DROP TRIGGER campaign_commercial_events_no_truncate ON public.campaign_commercial_events`},
		{"disabled-no-truncate-trigger", `ALTER TABLE public.campaign_commercial_events DISABLE TRIGGER campaign_commercial_events_no_truncate`},
		{"replica-only-no-truncate-trigger", `ALTER TABLE public.campaign_commercial_events ENABLE REPLICA TRIGGER campaign_commercial_events_no_truncate`},
		{"append-only-trigger-on-wrong-table", `DROP TRIGGER campaign_commercial_events_append_only ON public.campaign_commercial_events; CREATE TEMP TABLE commercial_readiness_decoy (id uuid); CREATE TRIGGER campaign_commercial_events_append_only BEFORE UPDATE OR DELETE ON commercial_readiness_decoy FOR EACH ROW EXECUTE FUNCTION public.prevent_campaign_commercial_event_mutation()`},
		{"no-truncate-trigger-on-wrong-table", `DROP TRIGGER campaign_commercial_events_no_truncate ON public.campaign_commercial_events; CREATE TEMP TABLE commercial_readiness_decoy (id uuid); CREATE TRIGGER campaign_commercial_events_no_truncate BEFORE TRUNCATE ON commercial_readiness_decoy FOR EACH STATEMENT EXECUTE FUNCTION public.prevent_campaign_commercial_event_mutation()`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// PostgreSQL DDL is transactional: always roll back the broken schema.
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(ctx, tc.breakSQL); err != nil {
				t.Fatal(err)
			}
			var ready bool
			if err := tx.QueryRowContext(ctx, controlSchemaReadinessQuery).Scan(&ready); err != nil {
				t.Fatalf("readiness predicate failed rather than returning false: %v", err)
			}
			if ready {
				t.Fatalf("control schema reported ready with broken commercial evidence: %s", tc.name)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err := verifyControlSchema(ctx, db); err != nil {
				t.Fatalf("restored commercial schema rejected: %v", err)
			}
		})
	}
}
