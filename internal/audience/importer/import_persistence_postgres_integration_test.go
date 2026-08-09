package importer

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func TestPostgreSQLImportIdentityReplayAndDurableDuplicateStaging(t *testing.T) {
	dsn := os.Getenv("POSTGRES_IMPORT_GOVERNANCE_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_IMPORT_GOVERNANCE_DATABASE_URL is not set")
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
	ids := make([]string, 5)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	makerID, checkerID, orgID, reviewID, purposeID := ids[0], ids[1], ids[2], ids[3], ids[4]
	now := time.Date(2099, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, actor := range []struct{ id, name string }{{makerID, "Import Maker"}, {checkerID, "Import Checker"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,$3,'DISABLED',false)`, actor.id, "import-governance-"+actor.id+"@internal.invalid", actor.name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Import Governance "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Import governance review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, checkerID, now.Add(-time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Import purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "IMPORT_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	repository := &PostgreSQLImportRepository{DB: db}
	service := &ImportService{Repository: repository, Clock: func() time.Time { return now }}
	input := CreateImportInput{
		OrganisationID: orgID, ConsentReviewID: reviewID, PurposeID: purposeID, Channel: "WHATSAPP", WordingVersion: "v1",
		SourceName: "governance source", SourceSystem: "CRM", DefaultCountryISO2: "NG",
		ObjectKey: "imports/governance.csv", OriginalFilename: "governance.csv", DetectedMediaType: "text/csv",
		FileSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ByteSize: 128,
		TemplateVersion: "audience-v1", Mapping: ColumnMapping{MSISDN: "msisdn", Country: "country"},
		UpdatePolicy: UpdateNewestSource, UploadedBy: makerID, ClientRequestID: "import-governance-request-0001", ContentSignatureValid: true,
	}
	first, created, err := service.Create(ctx, input)
	if err != nil || !created {
		t.Fatalf("create import: created=%v err=%v batch=%+v", created, err, first)
	}
	if first.FileSHA256 != input.FileSHA256 || first.UploadedBy != makerID || first.CreatedAt.IsZero() {
		t.Fatalf("immutable import identity evidence missing: checksum=%q uploader=%q createdAt=%v", first.FileSHA256, first.UploadedBy, first.CreatedAt)
	}
	replay, created, err := service.Create(ctx, input)
	if err != nil || created || replay.ID != first.ID {
		t.Fatalf("exact replay: created=%v err=%v replay=%+v", created, err, replay)
	}
	duplicateInput := input
	duplicateInput.ClientRequestID = "import-governance-request-0002"
	duplicate, _, err := service.Create(ctx, duplicateInput)
	if !errors.Is(err, ErrDuplicateImportFile) || duplicate.ID != first.ID {
		t.Fatalf("duplicate checksum replay: batch=%+v err=%v", duplicate, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE audience_imports SET source_name='tampered' WHERE id=$1::uuid`, first.ID); err == nil {
		t.Fatal("immutable import identity accepted source-name mutation")
	}
	validated, err := service.RecordScan(ctx, first.ID, MalwareClean, true, "", first.Version)
	if err != nil || validated.Status != ImportValidating {
		t.Fatalf("record clean scan: batch=%+v err=%v", validated, err)
	}

	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{31}, 32), bytes.Repeat([]byte{32}, 32))
	if err != nil {
		t.Fatal(err)
	}
	canonical := "+2348012345678"
	ciphertext, err := protector.Encrypt(canonical)
	if err != nil {
		t.Fatal(err)
	}
	baseCandidate := ContactCandidate{
		EncryptedMSISDN: ciphertext, LookupHMAC: protector.LookupHMAC(canonical), MaskedMSISDN: sharedcrypto.Mask(canonical),
		Country: "NG", SourceHash: "durable-duplicate-source", ProfileRecordedAt: now,
	}
	firstRow := baseCandidate
	firstRow.RowNumber = 2
	duplicateRow := baseCandidate
	duplicateRow.RowNumber = 3
	duplicateRow.SourceHash = "durable-duplicate-source-2"
	staging := &PostgreSQLStagingRepository{DB: db, WorkerID: "import-governance-test", Clock: func() time.Time { return now }}
	lease, err := staging.Begin(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := staging.StageBatch(ctx, first.ID, lease, []ContactCandidate{firstRow, duplicateRow})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Inserted != 1 || staged.SourceDuplicates != 1 {
		t.Fatalf("durable staging counts=%+v want inserted=1 duplicate=1", staged)
	}
	summary, err := staging.Summary(ctx, first.ID, lease)
	if err != nil {
		t.Fatal(err)
	}
	if summary.StagedRows != 1 || summary.SourceDuplicates != 1 {
		t.Fatalf("durable summary=%+v", summary)
	}
	if err := staging.CompleteValidation(ctx, first.ID, lease, PreviewResult{UploadedRows: 2}, summary); err != nil {
		t.Fatal(err)
	}
	persisted, err := repository.Get(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != ImportPreviewReady || persisted.UploadedRows != 2 || persisted.ValidRows != 1 || persisted.DuplicateRows != 1 {
		t.Fatalf("persisted preview counts=%+v", persisted)
	}
	var stagingRows, duplicateIssues int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_import_staging WHERE audience_import_id=$1::uuid`, first.ID).Scan(&stagingRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_import_issues WHERE audience_import_id=$1::uuid AND issue_code='DUPLICATE_MSISDN'`, first.ID).Scan(&duplicateIssues); err != nil {
		t.Fatal(err)
	}
	if stagingRows != 1 || duplicateIssues != 1 {
		t.Fatalf("staging rows=%d duplicate issues=%d", stagingRows, duplicateIssues)
	}
	cancelled, err := service.Cancel(ctx, first.ID, checkerID, "cancel preview before canonical merge", persisted.Version)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != ImportCancelled {
		t.Fatalf("cancelled import=%+v", cancelled)
	}
	var canonicalContacts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM contacts WHERE msisdn_lookup_hmac=$1`, baseCandidate.LookupHMAC).Scan(&canonicalContacts); err != nil {
		t.Fatal(err)
	}
	if canonicalContacts != 0 {
		t.Fatalf("cancelled preview mutated canonical contacts: %d", canonicalContacts)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_import_staging WHERE audience_import_id=$1::uuid`, first.ID).Scan(&stagingRows); err != nil {
		t.Fatal(err)
	}
	if stagingRows != 1 {
		t.Fatalf("cancelled preview lost staging evidence: %d", stagingRows)
	}
}
