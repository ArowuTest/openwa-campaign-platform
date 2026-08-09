package importer

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLProfileConflictResolutionDoesNotBroadenWithdrawnConsent(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PROFILE_CONSENT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PROFILE_CONSENT_DATABASE_URL is not set")
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

	ids := make([]string, 7)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, contactID, purposeID := ids[0], ids[1], ids[2], ids[3]
	reviewID, importID, conflictID := ids[4], ids[5], ids[6]
	now := time.Date(2099, 8, 1, 12, 0, 0, 0, time.UTC)

	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Profile Consent Actor','DISABLED',false)`, actorID, "profile-consent-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Profile Consent "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Profile consent review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, actorID, now.Add(-time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Profile purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "PROFILE_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO contacts(id,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,gender_code,status,profile_recorded_at) VALUES($1::uuid,decode('00','hex'),digest($2,'sha256'),'***7016','MALE','ACTIVE',$3)`, contactID, "profile-consent-"+contactID, now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var grantID string
	if err := db.QueryRowContext(ctx, `INSERT INTO consent_grants(contact_id,organisation_id,purpose_id,channel,wording_version,source_type,evidence_checksum,granted_at,status,effective_from,created_by,client_request_id,request_fingerprint,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'WHATSAPP','v1','DIRECT',$4,$5,'ACTIVE',$5,$6::uuid,$7,$8,$9::uuid) RETURNING id::text`, contactID, orgID, purposeID, "profile-consent-evidence-"+contactID, now.Add(-time.Hour), actorID, "profile-grant-"+contactID, "profile-fingerprint-"+contactID, reviewID).Scan(&grantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE consent_grants SET status='WITHDRAWN' WHERE id=$1::uuid`, grantID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO audience_imports(id,organisation_id,consent_review_id,source_name,object_key,original_filename,file_sha256,byte_size,mapping,status,detected_media_type,template_version,update_policy,malware_scan_status,content_signature_valid,client_request_id,uploaded_by) VALUES($1::uuid,$2::uuid,$3::uuid,'profile-update','imports/profile-update.csv','profile-update.csv',$4,100,'{}'::jsonb,'COMPLETED','text/csv','v1','NEWEST_SOURCE','CLEAN',true,$5,$6::uuid)`, importID, orgID, reviewID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "profile-update-"+importID, actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_profile_conflicts(id,audience_import_id,contact_id,masked_msisdn,field_name,existing_value,incoming_value,status,version,created_at) VALUES($1::uuid,$2::uuid,$3::uuid,'***7016','gender_code','MALE','FEMALE','PENDING',1,$4)`, conflictID, importID, contactID, now); err != nil {
		t.Fatal(err)
	}
	resolved, err := (&PostgreSQLConflictRepository{DB: db}).Resolve(ctx, conflictID, ResolutionUseIncoming, "apply reviewed profile value", actorID, 1, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != ConflictResolved || resolved.Resolution != ResolutionUseIncoming {
		t.Fatalf("resolved conflict=%+v", resolved)
	}

	var gender, consentStatus string
	var totalGrants, activeGrants int
	if err := db.QueryRowContext(ctx, `SELECT coalesce(gender_code,'') FROM contacts WHERE id=$1::uuid`, contactID).Scan(&gender); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM consent_grants WHERE id=$1::uuid`, grantID).Scan(&consentStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE status='ACTIVE') FROM consent_grants WHERE contact_id=$1::uuid`, contactID).Scan(&totalGrants, &activeGrants); err != nil {
		t.Fatal(err)
	}
	if gender != "FEMALE" {
		t.Fatalf("profile update not applied: gender=%q", gender)
	}
	if consentStatus != "WITHDRAWN" || totalGrants != 1 || activeGrants != 0 {
		t.Fatalf("profile update broadened consent: status=%q total=%d active=%d", consentStatus, totalGrants, activeGrants)
	}
}
