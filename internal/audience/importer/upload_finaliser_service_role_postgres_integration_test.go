package importer

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/security/malware"
	"campaign-platform/internal/storage"
)

// Removing the upload-session grants must stop real finalisation under the
// deployed worker role, even when the same path succeeds as the schema owner.
func TestPostgreSQLUploadFinaliserAudienceWorkerComposedAndRecovery(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		name := "fresh"
		if recovery {
			name = "existing_scanned_import_after_expired_lease"
		}
		t.Run(name, func(t *testing.T) {
			f := newUploadFinaliserRoleFixture(t)
			session := f.upload(t)
			repo := &PostgreSQLUploadSessionRepository{DB: f.worker}
			imports := &ImportService{Repository: &PostgreSQLImportRepository{DB: f.worker}, Clock: func() time.Time { return f.now }}
			wantImportID := ""
			if recovery {
				work, err := repo.ClaimReadyFinalisations(f.ctx, "crashed-finaliser", time.Minute, 1, f.now)
				if err != nil || len(work) != 1 || work[0].Session.ID != session.ID {
					t.Fatalf("claim under campaign_audience_worker: count=%d err=%v", len(work), err)
				}
				expiry := f.now.Add(30 * 24 * time.Hour)
				input := validCreateImportInput()
				input.OrganisationID, input.ConsentReviewID, input.PurposeID, input.UploadedBy = f.org, f.review, f.purpose, f.maker
				input.SourceName, input.SourceSystem, input.DefaultCountryISO2 = session.SourceName, session.SourceSystem, session.DefaultCountryISO2
				input.ObjectKey, input.UploadSessionID = uploadSessionSourceReference(session.ID), session.ID
				input.OriginalFilename, input.TemplateVersion = session.OriginalFilename, session.TemplateVersion
				input.Mapping, input.UpdatePolicy = session.Mapping, session.UpdatePolicy
				input.FileSHA256, input.ByteSize = checksumHex(f.payload), int64(len(f.payload))
				input.ClientRequestID, input.SourceExpiresAt = uploadSessionImportRequestKey(session.ID), &expiry
				batch, created, err := imports.Create(f.ctx, input)
				if err != nil || !created {
					t.Fatalf("create recovery import: created=%v err=%v", created, err)
				}
				batch, err = imports.RecordScan(f.ctx, batch.ID, MalwareClean, true, "", batch.Version)
				if err != nil {
					t.Fatal(err)
				}
				wantImportID = batch.ID
				f.now = f.now.Add(2 * time.Minute)
			}
			worker := &UploadFinalisationWorker{
				Repository: repo, Source: &UploadCompositeSource{Store: f.store},
				Scanner: &fakeMalwareScanner{result: malware.Result{Clean: true}},
				Imports: imports, WorkerID: "service-role-finaliser",
				LeaseDuration: 5 * time.Minute, ClaimBatch: 1,
				SourceRetention: 30 * 24 * time.Hour, Clock: func() time.Time { return f.now },
			}
			processed, err := worker.Process(f.ctx)
			if err != nil || processed != 1 {
				t.Fatalf("finalise under campaign_audience_worker: processed=%d err=%v", processed, err)
			}
			final, err := repo.Get(f.ctx, session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if final.State != UploadSessionImportCreated || final.LinkedImportID == "" ||
				final.FinalSHA256 != checksumHex(f.payload) || final.DetectedMediaType != "text/csv" ||
				final.FinaliserLeaseOwner != "" || final.FinaliserLeaseExpiresAt != nil {
				t.Fatalf("terminal finalisation/link evidence is incomplete: %+v", final)
			}
			if wantImportID != "" && final.LinkedImportID != wantImportID {
				t.Fatalf("recovery created a different import: got=%s want=%s", final.LinkedImportID, wantImportID)
			}
			batch, err := imports.Get(f.ctx, final.LinkedImportID)
			if err != nil {
				t.Fatal(err)
			}
			if batch.Status != ImportValidating || batch.UploadSessionID != session.ID ||
				batch.FileSHA256 != checksumHex(f.payload) || batch.MalwareStatus != MalwareClean {
				t.Fatalf("linked import not durably ready for validation: %+v", batch)
			}
			var count int
			if err := f.owner.QueryRowContext(f.ctx, "SELECT count(*) FROM audience_imports WHERE upload_session_id=$1::uuid", session.ID).Scan(&count); err != nil || count != 1 {
				t.Fatalf("import count=%d want=1 err=%v", count, err)
			}
			if final.RequestFingerprint() != session.RequestFingerprint() ||
				final.Parts[0].ObjectKey != session.Parts[0].ObjectKey || final.Parts[0].SHA256 != checksumHex(f.payload) {
				t.Fatal("worker rewrote frozen request or part evidence")
			}
			if processed, err := worker.Process(f.ctx); err != nil || processed != 0 {
				t.Fatalf("terminal replay: processed=%d err=%v", processed, err)
			}
		})
	}
}

func TestPostgreSQLUploadFinaliserAudienceWorkerLeaseLifecycle(t *testing.T) {
	f := newUploadFinaliserRoleFixture(t)
	first := f.upload(t)
	repo := &PostgreSQLUploadSessionRepository{DB: f.worker}
	work, err := repo.ClaimReadyFinalisations(f.ctx, "worker-a", 2*time.Minute, 1, f.now)
	if err != nil || len(work) != 1 || work[0].Session.ID != first.ID {
		t.Fatalf("claim under campaign_audience_worker: count=%d err=%v", len(work), err)
	}
	_, renewed, err := repo.RenewUploadFinalisation(f.ctx, first.ID, work[0].Lease, 2*time.Minute, f.now.Add(time.Minute))
	if err != nil || !renewed.ExpiresAt.Equal(f.now.Add(3*time.Minute)) {
		t.Fatalf("renew: expiry=%s err=%v", renewed.ExpiresAt, err)
	}
	if _, err := repo.FailUploadFinalisation(f.ctx, first.ID, "stale failure rejected", work[0].Lease, f.now.Add(time.Minute)); !errors.Is(err, ErrUploadFinalisationLease) {
		t.Fatalf("pre-renewal lease accepted: %v", err)
	}
	failed, err := repo.FailUploadFinalisation(f.ctx, first.ID, "scanner rejected source", renewed, f.now.Add(90*time.Second))
	if err != nil || failed.State != UploadSessionFailed || failed.FailureReason != "scanner rejected source" ||
		failed.FinaliserLeaseOwner != "" || failed.FinaliserLeaseExpiresAt != nil {
		t.Fatalf("fail: session=%+v err=%v", failed, err)
	}

	second := f.upload(t)
	original, err := repo.ClaimReadyFinalisations(f.ctx, "worker-a", time.Minute, 1, f.now)
	if err != nil || len(original) != 1 || original[0].Session.ID != second.ID {
		t.Fatalf("recovery original claim: count=%d err=%v", len(original), err)
	}
	takeover, err := repo.ClaimReadyFinalisations(f.ctx, "worker-b", 2*time.Minute, 1, f.now.Add(time.Minute))
	if err != nil || len(takeover) != 1 || takeover[0].Lease.Version != original[0].Lease.Version+1 {
		t.Fatalf("expired-lease takeover: count=%d err=%v", len(takeover), err)
	}
	late := f.now.Add(90 * time.Second)
	if _, _, err := repo.RenewUploadFinalisation(f.ctx, second.ID, original[0].Lease, 2*time.Minute, late); !errors.Is(err, ErrUploadFinalisationLease) {
		t.Fatalf("old owner renewal accepted: %v", err)
	}
	if _, err := repo.CompleteUploadFinalisation(f.ctx, second.ID, first.ID, checksumHex(f.payload), "text/csv", original[0].Lease, late); !errors.Is(err, ErrUploadFinalisationLease) {
		t.Fatalf("old owner completion accepted: %v", err)
	}
	if _, err := repo.FailUploadFinalisation(f.ctx, second.ID, "late owner failure", original[0].Lease, late); !errors.Is(err, ErrUploadFinalisationLease) {
		t.Fatalf("old owner failure accepted: %v", err)
	}
	if _, err := repo.FailUploadFinalisation(f.ctx, second.ID, "replacement owner rejected source", takeover[0].Lease, late); err != nil {
		t.Fatal(err)
	}
}

func TestPostgreSQLUploadFinaliserAudienceWorkerLeastPrivilege(t *testing.T) {
	f := newUploadFinaliserRoleFixture(t)
	session := f.upload(t)
	// The companion psql regression reconciles broad old grants twice before
	// these actual-role statements prove the resulting authority boundary.
	allowed := map[string]map[string]bool{
		"audience_import_upload_sessions": {
			"state": true, "finaliser_lease_owner": true, "finaliser_lease_version": true,
			"finaliser_lease_expires_at": true, "version": true, "updated_at": true,
			"final_sha256": true, "detected_media_type": true, "linked_import_id": true, "failure_reason": true,
		},
		"audience_import_upload_parts": {"state": true},
	}
	for table, columns := range allowed {
		var read, update, insert, delete, truncate bool
		if err := f.worker.QueryRowContext(f.ctx, `SELECT has_table_privilege(current_user,$1,'SELECT'),
has_table_privilege(current_user,$1,'UPDATE'),has_table_privilege(current_user,$1,'INSERT'),
has_table_privilege(current_user,$1,'DELETE'),has_table_privilege(current_user,$1,'TRUNCATE')`, table).Scan(&read, &update, &insert, &delete, &truncate); err != nil {
			t.Fatal(err)
		}
		if !read || update || insert || delete || truncate {
			t.Fatalf("%s privileges: select=%v table-update=%v insert=%v delete=%v truncate=%v", table, read, update, insert, delete, truncate)
		}
		rows, err := f.worker.QueryContext(f.ctx, `SELECT column_name,has_column_privilege(current_user,table_name,column_name,'UPDATE')
FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 ORDER BY ordinal_position`, table)
		if err != nil {
			t.Fatal(err)
		}
		seen := 0
		for rows.Next() {
			var column string
			var permitted bool
			if err := rows.Scan(&column, &permitted); err != nil {
				t.Fatal(err)
			}
			if permitted != columns[column] {
				t.Errorf("%s.%s update=%v want=%v", table, column, permitted, columns[column])
			}
			seen++
		}
		err = rows.Err()
		rows.Close()
		if err != nil || seen <= len(columns) {
			t.Fatalf("column privilege proof incomplete: table=%s seen=%d err=%v", table, seen, err)
		}
	}
	for _, query := range []string{
		"UPDATE audience_import_upload_sessions SET purpose_id=purpose_id WHERE id=$1::uuid",
		"UPDATE audience_import_upload_sessions SET mapping=mapping WHERE id=$1::uuid",
		"UPDATE audience_import_upload_sessions SET uploaded_bytes=uploaded_bytes WHERE id=$1::uuid",
		"DELETE FROM audience_import_upload_sessions WHERE id=$1::uuid",
		"INSERT INTO audience_import_upload_sessions SELECT * FROM audience_import_upload_sessions WHERE id=$1::uuid",
		"UPDATE audience_import_upload_parts SET sha256=sha256 WHERE session_id=$1::uuid",
		"UPDATE audience_import_upload_parts SET object_key=object_key WHERE session_id=$1::uuid",
		"DELETE FROM audience_import_upload_parts WHERE session_id=$1::uuid",
		"INSERT INTO audience_import_upload_parts SELECT * FROM audience_import_upload_parts WHERE session_id=$1::uuid",
	} {
		_, err := f.worker.ExecContext(f.ctx, query, session.ID)
		var state interface{ SQLState() string }
		if !errors.As(err, &state) || state.SQLState() != "42501" {
			t.Errorf("forbidden statement did not fail with insufficient_privilege: query=%s err=%v", query, err)
		}
	}
	_, err := f.worker.ExecContext(f.ctx, "UPDATE audience_import_upload_parts SET state='PENDING' WHERE session_id=$1::uuid", session.ID)
	var state interface{ SQLState() string }
	if !errors.As(err, &state) || state.SQLState() != "P0001" {
		t.Fatalf("row-lock column grant allowed immutable uploaded part rewrite: %v", err)
	}
}

type uploadFinaliserRoleFixture struct {
	ctx                         context.Context
	owner, worker               *sql.DB
	store                       storage.ObjectStore
	now                         time.Time
	payload                     []byte
	maker, org, review, purpose string
}

func newUploadFinaliserRoleFixture(t *testing.T) *uploadFinaliserRoleFixture {
	t.Helper()
	dsn := os.Getenv("POSTGRES_UPLOAD_FINALISER_ROLE_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_UPLOAD_FINALISER_ROLE_DATABASE_URL is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid dedicated upload-finaliser test DSN")
	}
	if u.Path != "/openwa_upload_finaliser_role_review_20261007" {
		t.Fatal("upload-finaliser role tests require their isolated review database")
	}
	owner, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	f := &uploadFinaliserRoleFixture{ctx: ctx, owner: owner,
		now:     time.Date(2099, 12, 2, 12, 0, 0, 0, time.UTC),
		payload: []byte("msisdn,country\n08012345678,NG\n")}
	query := u.Query()
	query.Set("options", "-crole=campaign_audience_worker")
	u.RawQuery = query.Encode()
	f.worker, err = sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.worker.Close() })
	var actualRole string
	if err := f.worker.QueryRowContext(ctx, "SELECT current_user").Scan(&actualRole); err != nil || actualRole != "campaign_audience_worker" {
		t.Fatalf("actual service role=%q err=%v", actualRole, err)
	}
	if err := owner.QueryRowContext(ctx, "SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text").Scan(&f.maker, &f.org, &f.review, &f.purpose); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Finaliser Maker','DISABLED',false)", []any{f.maker, "finaliser-" + f.maker + "@internal.invalid"}},
		{"INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')", []any{f.org, "Finaliser " + f.org}},
		{`INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,'Finaliser review','WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$3::uuid,$4,$5,'APPROVED')`, []any{f.review, f.org, f.maker, f.now.Add(-time.Hour), f.now.Add(24 * time.Hour)}},
		{"INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Finaliser purpose','WHATSAPP','v1',$4::uuid)", []any{f.purpose, f.org, "FINALISER_" + f.purpose, f.review}},
	} {
		if _, err := owner.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	f.store, err = storage.NewFileSystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A failed RED test must not leave a claimable upload for the next case.
	t.Cleanup(func() {
		_, err := owner.ExecContext(context.Background(), `UPDATE audience_import_upload_sessions
SET state='FAILED',failure_reason='test fixture cleanup',finaliser_lease_owner=NULL,finaliser_lease_expires_at=NULL
WHERE organisation_id=$1::uuid AND state IN ('UPLOADED','FINALISING')`, f.org)
		if err != nil {
			t.Errorf("fixture cleanup: %v", err)
		}
	})
	return f
}

func (f *uploadFinaliserRoleFixture) upload(t *testing.T) UploadSession {
	t.Helper()
	var request string
	if err := f.owner.QueryRowContext(f.ctx, "SELECT gen_random_uuid()::text").Scan(&request); err != nil {
		t.Fatal(err)
	}
	input := validUploadSessionInput()
	input.OrganisationID, input.ConsentReviewID, input.PurposeID, input.UploadedBy = f.org, f.review, f.purpose, f.maker
	input.ClientRequestID, input.ExpectedBytes = request, int64(len(f.payload))
	service := &UploadSessionService{
		Repository: &PostgreSQLUploadSessionRepository{DB: f.owner}, Store: f.store,
		PartSize: DefaultUploadPartSize, MaxFileSize: 2 << 30, SessionTTL: 24 * time.Hour,
		Clock: func() time.Time { return f.now },
	}
	session, created, err := service.Create(f.ctx, input)
	if err != nil || !created {
		t.Fatalf("owner create: created=%v err=%v", created, err)
	}
	session, changed, err := service.PutPart(f.ctx, session.ID, 1, checksumHex(f.payload), bytes.NewReader(f.payload))
	if err != nil || !changed {
		t.Fatalf("owner upload: changed=%v err=%v", changed, err)
	}
	session, err = service.Complete(f.ctx, session.ID, session.Version)
	if err != nil {
		t.Fatal(err)
	}
	return session
}
