package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLCampaignReportBreakdownsReconcileToRecipientMembership(t *testing.T) {
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

	ids := make([]string, 10)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, reviewID, purposeID := ids[0], ids[1], ids[2], ids[3]
	campaignID, messageID, snapshotID := ids[4], ids[5], ids[6]
	contacts := ids[7:]
	now := time.Date(2099, 7, 1, 12, 0, 0, 0, time.UTC)

	var countryID, lagosID string
	if err := db.QueryRowContext(ctx, `INSERT INTO countries(iso2,iso3,name) VALUES('XZ','XZZ','Metrics Testland') ON CONFLICT(iso2) DO UPDATE SET name=EXCLUDED.name RETURNING id::text`).Scan(&countryID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO administrative_areas(country_id,level,code,name,area_type) VALUES($1::uuid,1,'MET-LAGOS','Lagos','STATE') ON CONFLICT (country_id,parent_id,level,name) DO UPDATE SET active=true RETURNING id::text`, countryID).Scan(&lagosID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Metrics Report Actor','DISABLED',false)`, actorID, "metrics-report-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Metrics Report "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Metrics review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, actorID, now.Add(-time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Metrics purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "MET_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,maximum_messages_per_recipient,consent_review_id,execution_started_at,execution_completed_at) VALUES($1::uuid,$2::uuid,'Metrics breakdown campaign',$3::uuid,'COMPLETED',3,1,$4::uuid,$5,$6)`, campaignID, orgID, purposeID, reviewID, now.Add(-2*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','metrics',repeat('d',64),'APPROVED',$3)`, messageID, campaignID, "metrics-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count,configuration_version) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,3,'metrics-test')`, snapshotID, campaignID, "snapshot-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET audience_snapshot_id=$2::uuid,approved_message_version_id=$3::uuid WHERE id=$1::uuid`, campaignID, snapshotID, messageID); err != nil {
		t.Fatal(err)
	}

	ages := []any{22, 31, nil}
	ageDates := []any{now, now, nil}
	genders := []any{"MALE", "FEMALE", nil}
	states := []any{lagosID, lagosID, nil}
	for i, contactID := range contacts {
		lookup := fmt.Sprintf("metrics-breakdown-%s-%d", campaignID, i)
		if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,country_id,state_id,reported_age,age_recorded_at,gender_code,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),$3,$4::uuid,NULLIF($5,'')::uuid,$6::smallint,$7::date,NULLIF($8,''),'ACTIVE',$9)`, contactID, lookup, fmt.Sprintf("***%04d", i), countryID, states[i], ages[i], ageDates[i], genders[i], now); err != nil {
			t.Fatal(err)
		}
		var recipientID string
		if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&recipientID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'DELIVERED',$7,$7)`, recipientID, campaignID, snapshotID, contactID, messageID, "metrics-recipient-"+recipientID, now); err != nil {
			t.Fatal(err)
		}
	}

	var noteID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&noteID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_internal_notes(id,campaign_id,category,body,created_by,created_at) VALUES($1::uuid,$2::uuid,'TECHNICAL','CLIENT_REPORT_SECRET_NOTE',$3::uuid,$4)`, noteID, campaignID, actorID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE organisations SET internal_notes='ORGANISATION_INTERNAL_SECRET' WHERE id=$1::uuid`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaign_recipients SET last_error_detail='RAW_PROVIDER_SECRET_PAYLOAD' WHERE campaign_id=$1::uuid`, campaignID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE contacts SET masked_msisdn='2348012345678' WHERE id=$1::uuid`, contacts[0]); err != nil {
		t.Fatal(err)
	}

	report, err := (&PostgreSQLRepository{DB: db}).CampaignReport(ctx, campaignID, now)
	if err != nil {
		t.Fatal(err)
	}
	if report.RawBreakdowns["state"]["Lagos"] != 2 || report.RawBreakdowns["state"]["UNKNOWN"] != 1 {
		t.Fatalf("state breakdown=%v", report.RawBreakdowns["state"])
	}
	if report.RawBreakdowns["ageBand"]["18-24"] != 1 || report.RawBreakdowns["ageBand"]["25-34"] != 1 || report.RawBreakdowns["ageBand"]["UNKNOWN"] != 1 {
		t.Fatalf("age breakdown=%v", report.RawBreakdowns["ageBand"])
	}
	if report.RawBreakdowns["gender"]["Male"] != 1 || report.RawBreakdowns["gender"]["Female"] != 1 || report.RawBreakdowns["gender"]["UNKNOWN"] != 1 {
		t.Fatalf("gender breakdown=%v", report.RawBreakdowns["gender"])
	}
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	csvPayload, err := renderCSV(report)
	if err != nil {
		t.Fatal(err)
	}
	clientOutput := string(payload) + string(csvPayload)
	for _, secret := range []string{"CLIENT_REPORT_SECRET_NOTE", "ORGANISATION_INTERNAL_SECRET", "RAW_PROVIDER_SECRET_PAYLOAD", "2348012345678"} {
		if strings.Contains(clientOutput, secret) {
			t.Fatalf("client report leaked %q", secret)
		}
	}

	policy := ReportingPrivacyPolicy{ID: "metrics-threshold", Version: 3, MinimumCohortSize: 2, SuppressionLabel: "SMALL_CELL", ApplyGeography: true, ApplyDemographics: true, ApplyAttributes: true}
	applyCampaignReportingPrivacy(&report, policy, now)
	if report.RawBreakdowns != nil {
		t.Fatal("raw breakdowns escaped reporting privacy boundary")
	}
	if report.Privacy.PolicyID != policy.ID || report.Privacy.PolicyVersion != policy.Version {
		t.Fatalf("privacy evidence=%+v", report.Privacy)
	}
	if report.Privacy.SuppressedCellCount != 7 {
		t.Fatalf("suppressed cells=%d want=7", report.Privacy.SuppressedCellCount)
	}
	state := report.Breakdowns["state"]
	var lagosVisible, unknownSuppressed bool
	for _, cell := range state {
		if cell.Label == "Lagos" && cell.Count != nil && *cell.Count == 2 {
			lagosVisible = true
		}
		if cell.Label == "UNKNOWN" && cell.Suppressed && cell.Count == nil {
			unknownSuppressed = true
		}
	}
	if !lagosVisible || !unknownSuppressed {
		t.Fatalf("protected state breakdown=%+v", state)
	}
}
