package consent

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLOptOutMetricRecorderIsReplaySafe(t *testing.T) {
	dsn := os.Getenv("POSTGRES_FINAL_ELIGIBILITY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_FINAL_ELIGIBILITY_DATABASE_URL is not set")
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

	var orgID, contactID, purposeID, campaignID, suppression1, suppression2 string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(
		&orgID, &contactID, &purposeID, &campaignID, &suppression1, &suppression2,
	); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_opt_out_metric_events WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_metrics WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM suppressions WHERE id IN ($1::uuid,$2::uuid)`, suppression1, suppression2)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM contacts WHERE id=$1::uuid`, contactID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	}()

	now := time.Now().UTC().Truncate(time.Microsecond)
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "optout-metric-"+orgID)
	must(`INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***3131','ACTIVE',$3)`, contactID, contactID, now)
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Opt-out metric','WHATSAPP','v1')`, purposeID, orgID, "OPTOUT_"+purposeID)
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Opt-out metric evidence',$3::uuid,'DRAFT',1)`, campaignID, orgID, purposeID)
	for _, id := range []string{suppression1, suppression2} {
		must(`INSERT INTO suppressions(id,contact_id,scope,reason,effective_at,created_by,client_request_id,request_fingerprint) VALUES($1::uuid,$2::uuid,'GLOBAL','INBOUND_STOP',$3,'00000000-0000-0000-0000-000000000000'::uuid,$4,$4)`, id, contactID, now, "optout-metric-"+id)
	}

	recorder := &PostgreSQLOptOutMetricRecorder{DB: db}
	if err := recorder.RecordOptOut(ctx, campaignID, suppression1, now); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordOptOut(ctx, campaignID, suppression1, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var events, metricRows int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_opt_out_metric_events WHERE campaign_id=$1::uuid`, campaignID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_metrics WHERE campaign_id=$1::uuid`, campaignID).Scan(&metricRows); err != nil {
		t.Fatal(err)
	}
	if events != 1 || metricRows != 0 {
		t.Fatalf("replayed suppression changed durable opt-out events: events=%d campaignMetricRows=%d", events, metricRows)
	}

	if err := recorder.RecordOptOut(ctx, campaignID, suppression2, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_opt_out_metric_events WHERE campaign_id=$1::uuid`, campaignID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_metrics WHERE campaign_id=$1::uuid`, campaignID).Scan(&metricRows); err != nil {
		t.Fatal(err)
	}
	if events != 2 || metricRows != 0 {
		t.Fatalf("distinct suppression missing from durable opt-out events: events=%d campaignMetricRows=%d", events, metricRows)
	}
}
