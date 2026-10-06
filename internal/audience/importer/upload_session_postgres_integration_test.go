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
	"campaign-platform/internal/storage"
)

func TestPostgreSQLUploadSessionPersistsResumeAndImmutableParts(t *testing.T) {
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
	now := time.Date(2099, 11, 1, 12, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Upload Maker','DISABLED',false)`, makerID, "upload-session-"+makerID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Upload Session "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Upload session review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, reviewID, orgID, makerID, now.Add(-time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Upload purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "UPLOAD_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}

	repository := &PostgreSQLUploadSessionRepository{DB: db}
	store, err := storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := &UploadSessionService{
		Repository:  repository,
		Store:       store,
		PartSize:    5 << 20,
		MaxFileSize: 2 << 30,
		SessionTTL:  24 * time.Hour,
		Clock:       func() time.Time { return now },
	}
	input := validUploadSessionInput()
	input.OrganisationID = orgID
	input.ConsentReviewID = reviewID
	input.PurposeID = purposeID
	input.UploadedBy = makerID
	input.ClientRequestID = "postgres-large-upload-request-0001"
	input.ExpectedBytes = 9

	first, created, err := service.Create(ctx, input)
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v session=%+v", created, err, first)
	}
	replay, created, err := service.Create(ctx, input)
	if err != nil || created || replay.ID != first.ID {
		t.Fatalf("replay: created=%v err=%v session=%+v", created, err, replay)
	}
	conflict := input
	conflict.ExpectedBytes = 10
	if _, _, err := service.Create(ctx, conflict); !errors.Is(err, ErrUploadSessionReplayConflict) {
		t.Fatalf("expected replay conflict, got %v", err)
	}

	data := []byte("123456789")
	progressed, changed, err := service.PutPart(ctx, first.ID, 1, checksumHex(data), bytes.NewReader(data))
	if err != nil || !changed {
		t.Fatalf("put: changed=%v err=%v session=%+v", changed, err, progressed)
	}

	// Simulate process/browser restart by constructing a new service instance.
	resumedService := &UploadSessionService{Repository: &PostgreSQLUploadSessionRepository{DB: db}, Store: store, Clock: func() time.Time { return now }}
	resumed, err := resumedService.Get(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.UploadedParts != 1 || resumed.UploadedBytes != 9 || resumed.Parts[0].SHA256 != checksumHex(data) {
		t.Fatalf("durable progress missing after restart: %+v", resumed)
	}
	completed, err := resumedService.Complete(ctx, resumed.ID, resumed.Version)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != UploadSessionUploaded {
		t.Fatalf("state=%s", completed.State)
	}

	var sessions, parts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_import_upload_sessions WHERE id=$1::uuid`, first.ID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audience_import_upload_parts WHERE session_id=$1::uuid AND state='UPLOADED'`, first.ID).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || parts != 1 {
		t.Fatalf("database accounting sessions=%d parts=%d", sessions, parts)
	}

	if _, err := db.ExecContext(ctx, `UPDATE audience_import_upload_parts SET sha256=$2 WHERE session_id=$1::uuid AND part_number=1`, first.ID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err == nil {
		t.Fatal("immutable uploaded part accepted checksum mutation")
	}
}
