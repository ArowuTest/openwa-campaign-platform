package importer

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func TestPostgreSQLMergeDeduplicatesProtectedContactAndPreservesLineage(t *testing.T) {
	dsn := os.Getenv("POSTGRES_CONTACT_MERGE_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_CONTACT_MERGE_DATABASE_URL is not set")
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

	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{21}, 32), bytes.Repeat([]byte{22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	canonical := "+2348012345678"
	hmacValue := protector.LookupHMAC(canonical)
	now := time.Date(2099, 9, 1, 12, 0, 0, 0, time.UTC)
	ids := make([]string, 7)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	makerID, checkerID, orgID, reviewID, purposeID := ids[0], ids[1], ids[2], ids[3], ids[4]
	import1, import2 := ids[5], ids[6]

	for _, actor := range []struct{ id, name string }{{makerID, "Merge Maker"}, {checkerID, "Merge Checker"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,$3,'DISABLED',false)`, actor.id, "merge-"+actor.id+"@internal.invalid", actor.name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Merge Evidence "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO countries(iso2,iso3,name) VALUES('XZ','XZZ','Merge Testland') ON CONFLICT(iso2) DO UPDATE SET active=true`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,submitted_by,reviewed_by,reviewed_at,expires_at,decision_reason,outcome) VALUES($1::uuid,$2::uuid,'Merge evidence','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4::uuid,$5,$6,'approved merge evidence','APPROVED')`, reviewID, orgID, makerID, checkerID, now.Add(-2*time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Merge purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "MERGE_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_review_evidence(consent_review_id,object_key,original_filename,media_type,byte_size,sha256_checksum,malware_scan_status,uploaded_by,uploaded_at) VALUES($1::uuid,'reviews/merge-evidence.pdf','merge-evidence.pdf','application/pdf',10,$2,'CLEAN',$3::uuid,$4)`, reviewID, "merge-evidence-"+reviewID, makerID, now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	seedImport := func(importID, source string, observed time.Time, age int, gender string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO audience_imports(id,organisation_id,consent_review_id,purpose_id,channel,wording_version,source_name,source_system,default_country_iso2,object_key,original_filename,file_sha256,byte_size,mapping,status,detected_media_type,template_version,update_policy,malware_scan_status,content_signature_valid,client_request_id,uploaded_by,approved_by,approved_at,granted_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,'WHATSAPP','v1',$5,$5,'XZ',$6,$7,encode(digest($1::text,'sha256'),'hex'),100,'{}'::jsonb,'APPROVED','text/csv','v1','NEWEST_SOURCE','CLEAN',true,$8,$9::uuid,$10::uuid,$11,$11)`, importID, orgID, reviewID, purposeID, source, "imports/"+importID+".csv", importID+".csv", "merge-request-"+importID, makerID, checkerID, observed); err != nil {
			t.Fatal(err)
		}
		ciphertext, encErr := protector.Encrypt(canonical)
		if encErr != nil {
			t.Fatal(encErr)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO audience_import_staging(audience_import_id,row_number,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,country_iso2,reported_age,age_recorded_at,profile_recorded_at,gender_code,source_record_hash) VALUES($1::uuid,2,$2,$3,$4,'XZ',$5,$6::date,$7,$8,$9)`, importID, ciphertext, hmacValue, sharedcrypto.Mask(canonical), age, observed, observed, gender, "source-"+importID); err != nil {
			t.Fatal(err)
		}
	}
	firstObserved := now.Add(-time.Hour)
	secondObserved := now
	seedImport(import1, "CRM_A", firstObserved, 27, "FEMALE")
	seedImport(import2, "CRM_B", secondObserved, 31, "MALE")

	repo := &PostgreSQLMergeRepository{DB: db}
	first, err := repo.Merge(ctx, import1, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Merge(ctx, import2, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if first.InsertedContacts != 1 || second.InsertedContacts != 0 || second.UpdatedContacts != 1 {
		t.Fatalf("unexpected merge results first=%+v second=%+v", first, second)
	}

	var contactID, masked, ageSource, sourceSystem, sourceRecord string
	var encrypted, storedHMAC []byte
	var reportedAge int
	var ageRecorded time.Time
	if err := db.QueryRowContext(ctx, `SELECT id::text,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,reported_age,age_recorded_at,coalesce(age_source,''),coalesce(source_system,''),coalesce(source_record_id,'') FROM contacts WHERE msisdn_lookup_hmac=$1`, hmacValue).Scan(&contactID, &encrypted, &storedHMAC, &masked, &reportedAge, &ageRecorded, &ageSource, &sourceSystem, &sourceRecord); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(canonical)) {
		t.Fatal("plaintext MSISDN found in encrypted database value")
	}
	plain, err := protector.Decrypt(encrypted)
	if err != nil || plain != canonical {
		t.Fatalf("stored ciphertext did not decrypt to canonical MSISDN: plain=%q err=%v", plain, err)
	}
	if !bytes.Equal(storedHMAC, hmacValue) || !bytes.Equal(storedHMAC, protector.LookupHMAC(canonical)) {
		t.Fatal("stored lookup HMAC is not deterministic for canonical MSISDN")
	}
	if masked == canonical || reportedAge != 31 || ageSource != "SELF_DECLARED_IMPORT" || sourceSystem != "CRM_B" || sourceRecord != import2 {
		t.Fatalf("unexpected canonical contact fields: masked=%q age=%d source=%q system=%q record=%q", masked, reportedAge, ageSource, sourceSystem, sourceRecord)
	}
	if ageRecorded.Format("2006-01-02") != secondObserved.Format("2006-01-02") {
		t.Fatalf("age recorded date=%s want=%s", ageRecorded, secondObserved)
	}

	var contacts, sources, history int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM contacts WHERE msisdn_lookup_hmac=$1`, hmacValue).Scan(&contacts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM contact_sources WHERE contact_id=$1::uuid`, contactID).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM contact_profile_history WHERE contact_id=$1::uuid`, contactID).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if contacts != 1 || sources != 2 || history != 2 {
		t.Fatalf("lineage counts contacts=%d sources=%d history=%d", contacts, sources, history)
	}

	rows, err := db.QueryContext(ctx, `SELECT reported_age,source_record_hash FROM contact_profile_history WHERE contact_id=$1::uuid ORDER BY profile_recorded_at`, contactID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ages []int
	var hashes []string
	for rows.Next() {
		var age int
		var hash string
		if err := rows.Scan(&age, &hash); err != nil {
			t.Fatal(err)
		}
		ages = append(ages, age)
		hashes = append(hashes, hash)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ages) != 2 || ages[0] != 27 || ages[1] != 31 || hashes[0] != "source-"+import1 || hashes[1] != "source-"+import2 {
		t.Fatalf("profile history ages=%v hashes=%v", ages, hashes)
	}
	var import1Version, import2Version int64
	if err := db.QueryRowContext(ctx, `SELECT version FROM audience_imports WHERE id=$1::uuid`, import1).Scan(&import1Version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT version FROM audience_imports WHERE id=$1::uuid`, import2).Scan(&import2Version); err != nil {
		t.Fatal(err)
	}
	rollback := &PostgreSQLRollbackRepository{DB: db}
	if _, err := rollback.Rollback(ctx, import1, import1Version, checkerID, "reject rollback after a later independent source", now.Add(3*time.Minute)); err != ErrRollbackSuperseded {
		t.Fatalf("older import rollback err=%v want=%v", err, ErrRollbackSuperseded)
	}

	rolledBack, err := rollback.Rollback(ctx, import2, import2Version, checkerID, "restore the prior independent source state", now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.RestoredContacts != 1 || rolledBack.DeletedContacts != 0 || rolledBack.RevokedConsents != 1 {
		t.Fatalf("unexpected rollback result: %+v", rolledBack)
	}

	var restoredAge int
	var restoredGender, restoredSystem, restoredRecord, import2Status string
	if err := db.QueryRowContext(ctx, `SELECT reported_age,coalesce(gender_code,''),coalesce(source_system,''),coalesce(source_record_id,'') FROM contacts WHERE id=$1::uuid`, contactID).Scan(&restoredAge, &restoredGender, &restoredSystem, &restoredRecord); err != nil {
		t.Fatal(err)
	}
	if restoredAge != 27 || restoredGender != "FEMALE" || restoredSystem != "CRM_A" || restoredRecord != import1 {
		t.Fatalf("rollback did not restore prior source state: age=%d gender=%q system=%q record=%q", restoredAge, restoredGender, restoredSystem, restoredRecord)
	}
	var activeGrants, import2Revoked, rollbackEvents int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE status='ACTIVE'),count(*) FILTER(WHERE source_import_id=$2::uuid AND status='REVOKED') FROM consent_grants WHERE contact_id=$1::uuid`, contactID, import2).Scan(&activeGrants, &import2Revoked); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM contact_sources WHERE contact_id=$1::uuid`, contactID).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM contact_profile_history WHERE contact_id=$1::uuid`, contactID).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status FROM audience_imports WHERE id=$1::uuid`, import2).Scan(&import2Status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_import_rollback_events WHERE audience_import_id=$1::uuid`, import2).Scan(&rollbackEvents); err != nil {
		t.Fatal(err)
	}
	if sources != 1 || history != 1 || activeGrants != 1 || import2Revoked != 1 || import2Status != "ROLLED_BACK" || rollbackEvents != 1 {
		t.Fatalf("rollback lineage sources=%d history=%d activeGrants=%d import2Revoked=%d status=%q events=%d", sources, history, activeGrants, import2Revoked, import2Status, rollbackEvents)
	}
}

func TestPostgreSQLMergeRejectsImportWithUnapprovedConsentReview(t *testing.T) {
	dsn := os.Getenv("POSTGRES_CONTACT_MERGE_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_CONTACT_MERGE_DATABASE_URL is not set")
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
	ids := make([]string, 6)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	makerID, checkerID, orgID, reviewID, purposeID, importID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	now := time.Date(2099, 9, 2, 12, 0, 0, 0, time.UTC)
	for _, actor := range []struct{ id, name string }{{makerID, "Unapproved Maker"}, {checkerID, "Unapproved Checker"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,$3,'DISABLED',false)`, actor.id, "unapproved-"+actor.id+"@internal.invalid", actor.name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Unapproved Merge "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,status) VALUES($1::uuid,$2::uuid,'Unapproved review','WHATSAPP','DIRECT','v1','DRAFT')`, reviewID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Unapproved purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "UNAPPROVED_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_imports(id,organisation_id,consent_review_id,purpose_id,channel,wording_version,source_name,object_key,original_filename,file_sha256,byte_size,mapping,status,detected_media_type,template_version,update_policy,malware_scan_status,content_signature_valid,client_request_id,uploaded_by,approved_by,approved_at,granted_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,'WHATSAPP','v1','unapproved-source','imports/unapproved.csv','unapproved.csv',$5,100,'{}'::jsonb,'IMPORTING','text/csv','v1','NEWEST_SOURCE','CLEAN',true,$6,$7::uuid,$8::uuid,$9,$9)`, importID, orgID, reviewID, purposeID, strings.Repeat("d", 64), "unapproved-"+importID, makerID, checkerID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := (&PostgreSQLMergeRepository{DB: db}).Merge(ctx, importID, now.Add(time.Minute)); err != ErrConsentBasis {
		t.Fatalf("unapproved consent review merge err=%v want=%v", err, ErrConsentBasis)
	}
	var mergeResults int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_import_merge_results WHERE audience_import_id=$1::uuid`, importID).Scan(&mergeResults); err != nil {
		t.Fatal(err)
	}
	if mergeResults != 0 {
		t.Fatalf("unapproved import produced canonical merge evidence: %d", mergeResults)
	}
}
