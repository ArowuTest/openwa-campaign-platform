package importer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/security/malware"
	sharedcrypto "campaign-platform/internal/shared/crypto"
	"campaign-platform/internal/storage"
)

func TestPostgreSQLResumableUploadComposedFinaliseAndValidate(t *testing.T) {
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

	ids := make([]string, 4)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	makerID, orgID, reviewID, purposeID := ids[0], ids[1], ids[2], ids[3]
	now := time.Date(2099, 12, 1, 12, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Resumable Maker','DISABLED',false)`, makerID, "resumable-"+makerID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Resumable "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Resumable review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, makerID, now.Add(-time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Resumable purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "RESUMABLE_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}

	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	uploads := &PostgreSQLUploadSessionRepository{DB: db}
	uploadService := &UploadSessionService{
		Repository: uploads, Store: store,
		PartSize: DefaultUploadPartSize, MaxFileSize: 2 << 30,
		SessionTTL: 24 * time.Hour, Clock: func() time.Time { return now },
	}
	payload := []byte("msisdn,country\n08012345678,NG\n08087654321,NG\n")
	input := validUploadSessionInput()
	input.OrganisationID, input.ConsentReviewID, input.PurposeID, input.UploadedBy = orgID, reviewID, purposeID, makerID
	input.ClientRequestID = "postgres-composed-upload-0001"
	input.ExpectedBytes = int64(len(payload))
	session, created, err := uploadService.Create(ctx, input)
	if err != nil || !created {
		t.Fatalf("create upload: created=%v err=%v", created, err)
	}
	session, changed, err := uploadService.PutPart(ctx, session.ID, 1, checksumHex(payload), bytes.NewReader(payload))
	if err != nil || !changed {
		t.Fatalf("put part: changed=%v err=%v", changed, err)
	}
	session, err = uploadService.Complete(ctx, session.ID, session.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Stat(ctx, session.Parts[0].ObjectKey); err != nil {
		t.Fatalf("stored part not readable immediately after upload: %v", err)
	}
	reloaded, err := uploads.Get(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Parts[0].ObjectKey != session.Parts[0].ObjectKey {
		t.Fatalf("persisted object key drift: got=%q want=%q", reloaded.Parts[0].ObjectKey, session.Parts[0].ObjectKey)
	}
	if _, err := store.Stat(ctx, reloaded.Parts[0].ObjectKey); err != nil {
		t.Fatalf("persisted part not readable before finalisation: %v", err)
	}

	finaliser := &UploadFinalisationWorker{
		Repository:      uploads,
		Source:          &UploadCompositeSource{Store: store},
		Scanner:         &fakeMalwareScanner{result: malware.Result{Clean: true}},
		Imports:         &ImportService{Repository: &PostgreSQLImportRepository{DB: db}},
		WorkerID:        "postgres-composed-finaliser",
		LeaseDuration:   5 * time.Minute,
		ClaimBatch:      1,
		SourceRetention: 30 * 24 * time.Hour,
		Clock:           func() time.Time { return now.Add(3 * time.Minute) },
	}
	processed, err := finaliser.Process(ctx)
	if err != nil || processed != 1 {
		t.Fatalf("finalise: processed=%d err=%v", processed, err)
	}
	session, err = uploads.Get(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.State != UploadSessionImportCreated || session.LinkedImportID == "" {
		t.Fatalf("final session=%+v", session)
	}
	importRepo := &PostgreSQLImportRepository{DB: db}
	batch, err := importRepo.Get(ctx, session.LinkedImportID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != ImportValidating || batch.UploadSessionID != session.ID || batch.FileSHA256 != session.FinalSHA256 {
		t.Fatalf("import linkage/status=%+v", batch)
	}

	staging := &PostgreSQLStagingRepository{
		DB: db, WorkerID: "postgres-composed-validator",
		LeaseDuration: 5 * time.Minute, Clock: func() time.Time { return now.Add(4 * time.Minute) },
	}
	work, err := staging.ClaimReady(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var claimed ValidationWork
	found := false
	for _, item := range work {
		if item.ImportID == batch.ID {
			claimed = item
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("resumable import %s was not claimable after terminal upload link", batch.ID)
	}
	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{51}, 32), bytes.Repeat([]byte{52}, 32))
	if err != nil {
		t.Fatal(err)
	}
	validator := &ValidationWorker{
		Staging: staging, Store: store, UploadSessions: uploads,
		Ingest: &IngestService{Repository: staging, BatchSize: 1},
		Options: PreviewOptions{
			DefaultCountryISO2: "NG", MaxRows: 2_000_000, MaxIssues: 1000,
			MaxCandidateSample: 1, Protector: protector,
		},
	}
	if err := validator.process(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	persisted, err := importRepo.Get(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != ImportPreviewReady || persisted.UploadedRows != 2 || persisted.ValidRows != 2 || persisted.InvalidRows != 0 {
		t.Fatalf("validated import=%+v", persisted)
	}
	var staged int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_import_staging WHERE audience_import_id=$1::uuid`, batch.ID).Scan(&staged); err != nil {
		t.Fatal(err)
	}
	if staged != 2 {
		t.Fatalf("staged rows=%d want=2", staged)
	}

	// Mapping is persisted as governed JSON and remains tied to the original
	// session/import evidence across the whole composed path.
	var storedMapping []byte
	if err := db.QueryRowContext(ctx, `SELECT mapping FROM audience_imports WHERE id=$1::uuid`, batch.ID).Scan(&storedMapping); err != nil {
		t.Fatal(err)
	}
	var decoded ColumnMapping
	if err := json.Unmarshal(storedMapping, &decoded); err != nil || decoded.MSISDN != "msisdn" {
		t.Fatalf("mapping=%s err=%v", string(storedMapping), err)
	}
}
