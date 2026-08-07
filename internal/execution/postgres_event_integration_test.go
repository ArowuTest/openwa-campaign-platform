package execution

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLRecordEventPersistsJSONDetails(t *testing.T) {
	dsn := os.Getenv("POSTGRES_EXECUTION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_EXECUTION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var orgID, purposeID, campaignID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&orgID, &purposeID, &campaignID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_execution_events WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "execution-event-integration-"+orgID); err != nil {
		t.Fatalf("insert organisation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,'EXEC_TEST','Execution integration','WHATSAPP','v1')`, purposeID, orgID); err != nil {
		t.Fatalf("insert purpose: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Execution event integration',$3::uuid,'DISPATCHING',1)`, campaignID, orgID, purposeID); err != nil {
		t.Fatalf("insert campaign: %v", err)
	}
	now := time.Date(2026, 8, 7, 14, 30, 0, 0, time.UTC)
	repo := &PostgreSQLStore{DB: db}
	if err := repo.RecordEvent(ctx, campaignID, "COMPLETION_ASSESSMENT_FAILED", "execution-scheduler", "integration regression", map[string]any{
		"error":     "synthetic failure",
		"retryable": true,
	}, now); err != nil {
		t.Fatalf("record execution event: %v", err)
	}
	var gotError string
	var gotRetryable bool
	if err := db.QueryRowContext(ctx, `SELECT details->>'error',(details->>'retryable')::boolean FROM campaign_execution_events WHERE campaign_id=$1::uuid AND event_type='COMPLETION_ASSESSMENT_FAILED'`, campaignID).Scan(&gotError, &gotRetryable); err != nil {
		t.Fatalf("read execution event details: %v", err)
	}
	if gotError != "synthetic failure" || !gotRetryable {
		t.Fatalf("unexpected execution event details error=%q retryable=%v", gotError, gotRetryable)
	}
}
