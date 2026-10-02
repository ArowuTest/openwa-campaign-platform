package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/consent"
	"campaign-platform/internal/delivery"
	"campaign-platform/internal/operations"
	_ "campaign-platform/internal/persistence/database"
	postgresrepo "campaign-platform/internal/persistence/postgres"
)

type optOutAtomicFixture struct {
	actorID, orgID, contactID, purposeID, campaignID, messageID, snapshotID, recipientID string
	now                                                                                  time.Time
}

func newOptOutAtomicFixture(t *testing.T, ctx context.Context, db *sql.DB) optOutAtomicFixture {
	t.Helper()
	var f optOutAtomicFixture
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(
		&f.actorID, &f.orgID, &f.contactID, &f.purposeID, &f.campaignID, &f.messageID, &f.snapshotID, &f.recipientID,
	); err != nil {
		t.Fatal(err)
	}
	f.now = time.Now().UTC().Truncate(time.Microsecond)
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Opt-out atomic actor','DISABLED',false)`, f.actorID, "optout-atomic-"+f.actorID+"@internal.invalid")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, f.orgID, "Opt-out atomic "+f.orgID)
	must(`INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***9191','ACTIVE',$3)`, f.contactID, f.contactID, f.now)
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Opt-out atomic','WHATSAPP','v1')`, f.purposeID, f.orgID, "OA_"+f.purposeID)
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Opt-out atomic',$3::uuid,'DISPATCHING',1)`, f.campaignID, f.orgID, f.purposeID)
	must(`INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','optout atomic',repeat('a',64),'APPROVED',$3)`, f.messageID, f.campaignID, "optout-atomic-"+f.messageID)
	must(`INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1)`, f.snapshotID, f.campaignID, "optout-atomic-"+f.snapshotID)
	key, err := delivery.NewIdempotencyKey(f.campaignID, f.contactID, f.messageID)
	if err != nil {
		t.Fatal(err)
	}
	must(`INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'DELIVERED',$7,$7)`, f.recipientID, f.campaignID, f.snapshotID, f.contactID, f.messageID, key, f.now)
	return f
}

func (f optOutAtomicFixture) cleanup(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = db.ExecContext(ctx, `DELETE FROM campaign_opt_out_metric_events WHERE campaign_id=$1::uuid`, f.campaignID)
	_, _ = db.ExecContext(ctx, `DELETE FROM campaign_metrics WHERE campaign_id=$1::uuid`, f.campaignID)
	_, _ = db.ExecContext(ctx, `DELETE FROM consent_events WHERE contact_id=$1::uuid`, f.contactID)
	_, _ = db.ExecContext(ctx, `DELETE FROM suppressions WHERE contact_id=$1::uuid`, f.contactID)
	_, _ = db.ExecContext(ctx, `DELETE FROM campaign_recipients WHERE campaign_id=$1::uuid`, f.campaignID)
	_, _ = db.ExecContext(ctx, `DELETE FROM audience_snapshots WHERE id=$1::uuid`, f.snapshotID)
	_, _ = db.ExecContext(ctx, `DELETE FROM message_versions WHERE id=$1::uuid`, f.messageID)
	_, _ = db.ExecContext(ctx, `DELETE FROM campaigns WHERE id=$1::uuid`, f.campaignID)
	_, _ = db.ExecContext(ctx, `DELETE FROM consent_purposes WHERE id=$1::uuid`, f.purposeID)
	_, _ = db.ExecContext(ctx, `DELETE FROM contacts WHERE id=$1::uuid`, f.contactID)
	_, _ = db.ExecContext(ctx, `DELETE FROM organisations WHERE id=$1::uuid`, f.orgID)
	_, _ = db.ExecContext(ctx, `DELETE FROM internal_users WHERE id=$1::uuid`, f.actorID)
}

func openOptOutAtomicDB(t *testing.T) (*sql.DB, context.Context, context.CancelFunc) {
	t.Helper()
	dsn := os.Getenv("POSTGRES_FINAL_ELIGIBILITY_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_FINAL_ELIGIBILITY_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	if err := db.PingContext(ctx); err != nil {
		cancel()
		db.Close()
		t.Fatal(err)
	}
	return db, ctx, func() {
		cancel()
		db.Close()
	}
}

func TestPostgreSQLOptOutSuppressionAndMetricCommitAtomically(t *testing.T) {
	db, ctx, done := openOptOutAtomicDB(t)
	defer done()
	f := newOptOutAtomicFixture(t, ctx, db)
	defer f.cleanup(t, db)

	ledger := consent.NewLedgerService(&postgresrepo.ConsentLedgerRepository{DB: db})
	input := consent.SuppressionInput{
		ContactID: f.contactID, Scope: consent.SuppressionGlobal, Reason: "INBOUND_STOP",
		EffectiveAt: &f.now, SourceReference: "gateway:atomic-rollback", CreatedBy: f.actorID,
		ClientRequestID: "gateway-inbound:atomic-rollback",
	}
	var missingCampaign string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&missingCampaign); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ledger.CreateSuppressionWithOptOutMetric(ctx, input, missingCampaign, f.now); err == nil {
		t.Fatal("expected missing campaign metric write to fail atomically")
	}
	var suppressions, consentEvents int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM suppressions WHERE created_by=$1::uuid AND client_request_id=$2`, f.actorID, input.ClientRequestID).Scan(&suppressions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM consent_events WHERE contact_id=$1::uuid AND event_type='SUPPRESSION_CREATED'`, f.contactID).Scan(&consentEvents); err != nil {
		t.Fatal(err)
	}
	if suppressions != 0 || consentEvents != 0 {
		t.Fatalf("metric failure leaked suppression transaction: suppressions=%d events=%d", suppressions, consentEvents)
	}

	processor := &consent.OptOutProcessor{
		Deliveries: delivery.NewService(&delivery.PostgreSQLRepository{DB: db}),
		Ledger:     ledger,
		ActorID:    f.actorID,
		Clock:      func() time.Time { return f.now.Add(time.Minute) },
	}
	first, err := processor.Process(ctx, f.recipientID, "atomic-rollback", "STOP", "gateway:atomic-rollback")
	if err != nil || !first.Recognised || first.Replayed {
		t.Fatalf("post-rollback opt-out=%+v err=%v", first, err)
	}
	replay, err := processor.Process(ctx, f.recipientID, "atomic-rollback", " STOP! ", "gateway:atomic-rollback")
	if err != nil || !replay.Recognised || !replay.Replayed || replay.Suppression.ID != first.Suppression.ID {
		t.Fatalf("post-rollback replay=%+v err=%v", replay, err)
	}
	report, err := (&operations.PostgreSQLRepository{DB: db}).CampaignReport(ctx, f.campaignID, f.now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var metricEvents int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_opt_out_metric_events WHERE campaign_id=$1::uuid`, f.campaignID).Scan(&metricEvents); err != nil {
		t.Fatal(err)
	}
	if report.Engagement["optOuts"] != 1 || metricEvents != 1 {
		t.Fatalf("post-rollback reporting drift: optOuts=%d events=%d", report.Engagement["optOuts"], metricEvents)
	}
}

func TestPostgreSQLOptOutProcessorAppearsOnceInCampaignReport(t *testing.T) {
	db, ctx, done := openOptOutAtomicDB(t)
	defer done()
	f := newOptOutAtomicFixture(t, ctx, db)
	defer f.cleanup(t, db)

	ledger := consent.NewLedgerService(&postgresrepo.ConsentLedgerRepository{DB: db})
	processor := &consent.OptOutProcessor{
		Deliveries: delivery.NewService(&delivery.PostgreSQLRepository{DB: db}),
		Ledger:     ledger,
		ActorID:    f.actorID,
		Clock:      func() time.Time { return f.now.Add(time.Minute) },
	}
	first, err := processor.Process(ctx, f.recipientID, "atomic-stop-1", "STOP", "gateway:atomic-stop-1")
	if err != nil || !first.Recognised || first.Replayed {
		t.Fatalf("first opt-out=%+v err=%v", first, err)
	}
	replay, err := processor.Process(ctx, f.recipientID, "atomic-stop-1", " STOP! ", "gateway:atomic-stop-1")
	if err != nil || !replay.Recognised || !replay.Replayed || replay.Suppression.ID != first.Suppression.ID {
		t.Fatalf("replayed opt-out=%+v err=%v", replay, err)
	}
	report, err := (&operations.PostgreSQLRepository{DB: db}).CampaignReport(ctx, f.campaignID, f.now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if report.Engagement["optOuts"] != 1 {
		t.Fatalf("campaign report optOuts=%d want=1 report=%+v", report.Engagement["optOuts"], report)
	}
	organisationReport, err := (&operations.PostgreSQLRepository{DB: db}).OrganisationPerformanceReport(ctx, f.orgID, f.now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if organisationReport.Recipients["optOuts"] != 1 {
		t.Fatalf("organisation report optOuts=%d want=1 report=%+v", organisationReport.Recipients["optOuts"], organisationReport)
	}
	var metricEvents int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_opt_out_metric_events WHERE campaign_id=$1::uuid`, f.campaignID).Scan(&metricEvents); err != nil {
		t.Fatal(err)
	}
	if metricEvents != 1 {
		t.Fatalf("campaign opt-out metric events=%d want=1", metricEvents)
	}
}

func TestPostgreSQLOptOutDoesNotDependOnCampaignMetricsRowLock(t *testing.T) {
	db, ctx, done := openOptOutAtomicDB(t)
	defer done()
	f := newOptOutAtomicFixture(t, ctx, db)
	defer f.cleanup(t, db)

	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_metrics(campaign_id,opt_out_total,updated_at)
VALUES($1::uuid,0,$2) ON CONFLICT(campaign_id) DO NOTHING`, f.campaignID, f.now); err != nil {
		t.Fatal(err)
	}
	lockTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lockTx.Rollback()
	var lockedCampaignID string
	if err := lockTx.QueryRowContext(ctx, `SELECT campaign_id::text FROM campaign_metrics WHERE campaign_id=$1::uuid FOR UPDATE`, f.campaignID).Scan(&lockedCampaignID); err != nil {
		t.Fatal(err)
	}
	if lockedCampaignID != f.campaignID {
		t.Fatalf("locked campaign=%s want=%s", lockedCampaignID, f.campaignID)
	}

	processor := &consent.OptOutProcessor{
		Deliveries: delivery.NewService(&delivery.PostgreSQLRepository{DB: db}),
		Ledger:     consent.NewLedgerService(&postgresrepo.ConsentLedgerRepository{DB: db}),
		ActorID:    f.actorID,
		Clock:      func() time.Time { return f.now.Add(time.Minute) },
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := processor.Process(callCtx, f.recipientID, "locked-metrics-stop", "STOP", "gateway:locked-metrics-stop")
	if err != nil {
		t.Fatalf("opt-out depended on locked campaign_metrics row: %v", err)
	}
	if !result.Recognised || result.Replayed {
		t.Fatalf("unexpected opt-out result while campaign_metrics locked: %+v", result)
	}
	report, err := (&operations.PostgreSQLRepository{DB: db}).CampaignReport(callCtx, f.campaignID, f.now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("campaign report failed while metrics row locked: %v", err)
	}
	if report.Engagement["optOuts"] != 1 {
		t.Fatalf("campaign report optOuts=%d want=1", report.Engagement["optOuts"])
	}
}
