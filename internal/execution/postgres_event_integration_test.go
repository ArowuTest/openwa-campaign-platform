package execution

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
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
	var gotRetryable, versionIsNull bool
	if err := db.QueryRowContext(ctx, `
SELECT details->>'error',(details->>'retryable')::boolean,campaign_version IS NULL
FROM campaign_execution_events
WHERE campaign_id=$1::uuid AND event_type='COMPLETION_ASSESSMENT_FAILED'`,
		campaignID,
	).Scan(&gotError, &gotRetryable, &versionIsNull); err != nil {
		t.Fatalf("read execution event details: %v", err)
	}
	if gotError != "synthetic failure" || !gotRetryable || !versionIsNull {
		t.Fatalf("unexpected execution event error=%q retryable=%v versionIsNull=%v",
			gotError, gotRetryable, versionIsNull)
	}
}

func TestPostgreSQLRecordLifecycleEventPersistsCampaignVersion(t *testing.T) {
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

	var orgID, purposeID, campaignID string
	if err := db.QueryRowContext(ctx,
		`SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`,
	).Scan(&orgID, &purposeID, &campaignID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM campaign_execution_events WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	}()
	if _, err = db.ExecContext(ctx,
		`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`,
		orgID, "lifecycle-event-"+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx,
		`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version)
		  VALUES($1::uuid,$2::uuid,'LIFECYCLE_EVENT','Lifecycle event','WHATSAPP','v1')`,
		purposeID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx,
		`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients)
		  VALUES($1::uuid,$2::uuid,'Lifecycle event',$3::uuid,'PAUSED',1)`,
		campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	value := validLifecycleCommit()
	value.Campaign = campaign.Campaign{ID: campaignID, Version: 2}
	if err = (&PostgreSQLStore{DB: db}).RecordLifecycleEventInTx(ctx, tx, value); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	duplicate, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	if err = (&PostgreSQLStore{DB: db}).RecordLifecycleEventInTx(
		ctx, duplicate, value,
	); err == nil {
		_ = duplicate.Rollback()
		t.Fatal("duplicate campaign lifecycle version was accepted")
	}
	_ = duplicate.Rollback()
	var version int64
	if err = db.QueryRowContext(ctx,
		`SELECT campaign_version FROM campaign_execution_events
		  WHERE campaign_id=$1::uuid AND event_type='CAMPAIGN_PAUSED'`,
		campaignID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("campaign version=%d", version)
	}
}
