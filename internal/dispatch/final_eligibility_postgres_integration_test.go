package dispatch

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	_ "campaign-platform/internal/persistence/database"
)

type finalEligibilityFixture struct {
	Recipient      delivery.Recipient
	OrganisationID string
	PurposeID      string
	ContactID      string
	CampaignID     string
	ActorID        string
	GrantID        string
	AsOf           time.Time
}

func TestPostgreSQLFinalEligibilityHoldsQueuedWorkAtEverySafetyBoundary(t *testing.T) {
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
	f := seedFinalEligibilityFixture(t, ctx, db)
	checker := &PostgreSQLFinalEligibility{DB: db}

	assertDecision := func(wantEligible bool, wantReason string) {
		t.Helper()
		decision, checkErr := checker.Check(ctx, f.Recipient, f.AsOf)
		if checkErr != nil {
			t.Fatalf("final eligibility error: %v", checkErr)
		}
		if decision.Eligible != wantEligible || decision.Reason != wantReason {
			t.Fatalf("decision=%+v want eligible=%v reason=%q", decision, wantEligible, wantReason)
		}
	}
	assertDecision(true, "")
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET status='PAUSED' WHERE id=$1::uuid`, f.CampaignID); err != nil {
		t.Fatal(err)
	}
	assertDecision(false, "CAMPAIGN_NOT_DISPATCHABLE")
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET status='DISPATCHING',requested_start_at=$2 WHERE id=$1::uuid`, f.CampaignID, f.AsOf.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertDecision(false, "CAMPAIGN_NOT_STARTED")
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET requested_start_at=$2,completion_deadline_at=$3 WHERE id=$1::uuid`, f.CampaignID, f.AsOf.Add(-time.Hour), f.AsOf); err != nil {
		t.Fatal(err)
	}
	assertDecision(false, "CAMPAIGN_DEADLINE_PASSED")
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET completion_deadline_at=$2,campaign_timezone='UTC',quiet_hours_start='10:00',quiet_hours_end='11:00' WHERE id=$1::uuid`, f.CampaignID, f.AsOf.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertDecision(false, "CAMPAIGN_QUIET_HOURS")
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET quiet_hours_start=NULL,quiet_hours_end=NULL WHERE id=$1::uuid`, f.CampaignID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE contacts SET status='INACTIVE' WHERE id=$1::uuid`, f.ContactID); err != nil {
		t.Fatal(err)
	}
	assertDecision(false, "CONTACT_INACTIVE")
	if _, err := db.ExecContext(ctx, `UPDATE contacts SET status='ACTIVE' WHERE id=$1::uuid`, f.ContactID); err != nil {
		t.Fatal(err)
	}
	var suppressionID string
	if err := db.QueryRowContext(ctx, `INSERT INTO suppressions(contact_id,scope,reason,effective_at,created_by,client_request_id,request_fingerprint) VALUES($1::uuid,'GLOBAL','final eligibility evidence',$2,$3::uuid,$4,$5) RETURNING id::text`, f.ContactID, f.AsOf.Add(-time.Minute), f.ActorID, "suppression-"+f.ContactID, "fingerprint-"+f.ContactID).Scan(&suppressionID); err != nil {
		t.Fatal(err)
	}
	assertDecision(false, "SUPPRESSED")
	if _, err := db.ExecContext(ctx, `UPDATE suppressions SET active=false WHERE id=$1::uuid`, suppressionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE consent_grants SET status='WITHDRAWN' WHERE id=$1::uuid`, f.GrantID); err != nil {
		t.Fatal(err)
	}
	assertDecision(false, "CONSENT_WITHDRAWN")
}

func seedFinalEligibilityFixture(t *testing.T, ctx context.Context, db *sql.DB) finalEligibilityFixture {
	t.Helper()
	ids := make([]string, 9)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	orgID, purposeID, contactID, campaignID := ids[0], ids[1], ids[2], ids[3]
	reviewID, messageID, snapshotID, actorID, recipientID := ids[4], ids[5], ids[6], ids[7], ids[8]
	asOf := time.Date(2026, 8, 8, 10, 30, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Eligibility Evidence Actor','DISABLED',false)`, actorID, "eligibility-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Eligibility Evidence "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Dispatch review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, actorID, asOf.Add(-24*time.Hour), asOf.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Dispatch purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "DISP_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,requested_start_at,completion_deadline_at,maximum_unique_recipients,consent_review_id,campaign_timezone) VALUES($1::uuid,$2::uuid,'Final eligibility',$3::uuid,'DISPATCHING',$4,$5,1,$6::uuid,'UTC')`, campaignID, orgID, purposeID, asOf.Add(-time.Hour), asOf.Add(2*time.Hour), reviewID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***8010','ACTIVE',$3)`, contactID, "eligibility-"+contactID, asOf.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO message_versions(id,campaign_id,version,message_type,body,content_hash,status,client_request_id) VALUES($1::uuid,$2::uuid,1,'TEXT','final eligibility',repeat('d',64),'APPROVED',$3)`, messageID, campaignID, "eligibility-"+messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_snapshots(id,campaign_id,segment_definition,consent_policy_version,snapshot_hash,eligible_count) VALUES($1::uuid,$2::uuid,'{}'::jsonb,'v1',$3,1)`, snapshotID, campaignID, "eligibility-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET audience_snapshot_id=$2::uuid,approved_message_version_id=$3::uuid WHERE id=$1::uuid`, campaignID, snapshotID, messageID); err != nil {
		t.Fatal(err)
	}
	var grantID string
	if err := db.QueryRowContext(ctx, `INSERT INTO consent_grants(contact_id,organisation_id,purpose_id,channel,wording_version,source_type,evidence_checksum,granted_at,status,effective_from,created_by,client_request_id,request_fingerprint,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'WHATSAPP','v1','DIRECT',$4,$5,'ACTIVE',$5,$6::uuid,$7,$8,$9::uuid) RETURNING id::text`, contactID, orgID, purposeID, "eligibility-evidence-"+contactID, asOf.Add(-time.Hour), actorID, "grant-"+contactID, "grant-fingerprint-"+contactID, reviewID).Scan(&grantID); err != nil {
		t.Fatal(err)
	}
	key, err := delivery.NewIdempotencyKey(campaignID, contactID, messageID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaign_recipients(id,campaign_id,snapshot_id,contact_id,message_version_id,idempotency_key,status,authorised_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,'QUEUED',$7,$7)`, recipientID, campaignID, snapshotID, contactID, messageID, key, asOf.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	return finalEligibilityFixture{Recipient: delivery.Recipient{ID: recipientID, CampaignID: campaignID, ContactID: contactID, MessageVersionID: messageID, IdempotencyKey: key, Status: delivery.StatusQueued, UpdatedAt: asOf.Add(-time.Minute)}, OrganisationID: orgID, PurposeID: purposeID, ContactID: contactID, CampaignID: campaignID, ActorID: actorID, GrantID: grantID, AsOf: asOf}
}

func TestPostgreSQLFinalEligibilityRequiresMatchingOrganisationPurposeAndChannelBasis(t *testing.T) {
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
	f := seedFinalEligibilityFixture(t, ctx, db)
	checker := &PostgreSQLFinalEligibility{DB: db}
	assertReason := func(want string) {
		t.Helper()
		decision, checkErr := checker.Check(ctx, f.Recipient, f.AsOf)
		if checkErr != nil {
			t.Fatal(checkErr)
		}
		if decision.Eligible || decision.Reason != want {
			t.Fatalf("decision=%+v want=%q", decision, want)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE consent_grants SET purpose_id='different-purpose' WHERE id=$1::uuid`, f.GrantID); err != nil {
		t.Fatal(err)
	}
	assertReason("NO_ACTIVE_CONSENT")
	if _, err := db.ExecContext(ctx, `UPDATE consent_grants SET purpose_id=$2 WHERE id=$1::uuid`, f.GrantID, f.PurposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE consent_grants SET channel='SMS' WHERE id=$1::uuid`, f.GrantID); err != nil {
		t.Fatal(err)
	}
	assertReason("NO_ACTIVE_CONSENT")
	if _, err := db.ExecContext(ctx, `UPDATE consent_grants SET channel='WHATSAPP' WHERE id=$1::uuid`, f.GrantID); err != nil {
		t.Fatal(err)
	}
	var otherOrg string
	if err := db.QueryRowContext(ctx, `INSERT INTO organisations(legal_name,status) VALUES($1,'ACTIVE') RETURNING id::text`, "Other Eligibility Org "+f.ContactID).Scan(&otherOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE consent_grants SET organisation_id=$2::uuid WHERE id=$1::uuid`, f.GrantID, otherOrg); err != nil {
		t.Fatal(err)
	}
	assertReason("NO_ACTIVE_CONSENT")
}
