package reconciliation

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLMetricReconciliationRecordsJSONEvidence(t *testing.T) {
	dsn := os.Getenv("POSTGRES_METRICS_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_METRICS_DATABASE_URL is not set")
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
	var orgID, purposeID, campaignID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&orgID, &purposeID, &campaignID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Task 6 metrics "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Task 6 metrics','WHATSAPP','v1')`, purposeID, orgID, "METRICS_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Task 6 metrics',$3::uuid,'DRAFT',10)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 3, 2, 16, 0, 0, 0, time.UTC)
	leaseExpiry := now.Add(time.Minute)
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_metric_reconciliations(campaign_id,status,next_run_at,lease_owner,lease_expires_at,lease_version,created_at,updated_at) VALUES($1::uuid,'PROCESSING',$2,'task6-worker',$3,7,$2,$2)`, campaignID, now, leaseExpiry); err != nil {
		t.Fatal(err)
	}
	work := Work{CampaignID: campaignID, Lease: Lease{Owner: "task6-worker", Version: 7, ExpiresAt: leaseExpiry}}
	observation := Observation{
		CampaignID: campaignID,
		Canonical:  delivery.Metrics{AuthorisedTotal: 10, QueuedTotal: 3, DeliveredTotal: 5},
		Stored:     delivery.Metrics{AuthorisedTotal: 10, QueuedTotal: 3, DeliveredTotal: 5},
		Matched:    true, ObservedAt: now, CanonicalTotal: 18, StoredTotal: 18,
	}
	repo := &PostgreSQLRepository{DB: db}
	if err := repo.Record(ctx, work, observation, now, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	var status string
	var canonicalJSON, storedJSON string
	var leaseOwner sql.NullString
	var driftCount int
	if err := db.QueryRowContext(ctx, `SELECT status,last_canonical::text,last_stored::text,lease_owner,consecutive_drift_count FROM campaign_metric_reconciliations WHERE campaign_id=$1::uuid`, campaignID).Scan(&status, &canonicalJSON, &storedJSON, &leaseOwner, &driftCount); err != nil {
		t.Fatal(err)
	}
	if status != "MATCH" || leaseOwner.Valid || driftCount != 0 {
		t.Fatalf("status=%s leaseOwner=%v driftCount=%d", status, leaseOwner, driftCount)
	}
	if canonicalJSON == "" || storedJSON == "" || canonicalJSON == "null" || storedJSON == "null" {
		t.Fatalf("canonical=%s stored=%s", canonicalJSON, storedJSON)
	}
}
