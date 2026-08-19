package importer

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLImportReconciliationPersistsEvidenceIdempotently(t *testing.T) {
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
	ids := make([]string, 6)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, reviewID, purposeID, importID, reconciliationID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	now := time.Date(2099, 3, 2, 15, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 reconciliation','DISABLED',false)`, actorID, "reconciliation-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Task 6 reconciliation "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,status) VALUES($1::uuid,$2::uuid,'Task 6 review','WHATSAPP','DIRECT','v1','DRAFT')`, reviewID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Task 6 purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "RECON_"+purposeID, reviewID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_imports(id,organisation_id,consent_review_id,purpose_id,channel,wording_version,source_name,object_key,original_filename,file_sha256,byte_size,mapping,status,detected_media_type,template_version,update_policy,malware_scan_status,content_signature_valid,client_request_id,uploaded_by,version) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,'WHATSAPP','v1','task6-reconciliation','imports/task6-reconciliation.csv','task6-reconciliation.csv',$5,100,'{}'::jsonb,'COMPLETED','text/csv','v1','NEWEST_SOURCE','CLEAN',true,$6,$7::uuid,1)`, importID, orgID, reviewID, purposeID, strings.Repeat("c", 64), "reconciliation-request-"+importID, actorID); err != nil {
		t.Fatal(err)
	}
	record := ReconciliationRecord{
		ID: reconciliationID, ImportID: importID,
		Evidence: Reconciliation{
			ImportID: importID, Status: ImportCompleted, UploadedRows: 4, ValidRows: 4,
			InsertedContacts: 3, UpdatedContacts: 1, ValidationAccounted: 4,
			ValidationBalanced: true, MergeAccounted: 4, MergeWithinValidRows: true,
			ReadyForClosure: true, CalculatedAt: now,
		},
		EvidenceHash: strings.Repeat("d", 64), Reason: "task six reconciliation closure",
		PerformedBy: actorID, CreatedAt: now,
	}
	repo := &PostgreSQLReconciliationRepository{DB: db}
	stored, created, err := repo.Ensure(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if !created || stored.ID != record.ID {
		t.Fatalf("stored=%+v created=%v", stored, created)
	}
	loaded, err := repo.GetByImport(ctx, importID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Evidence.InsertedContacts != 3 || !loaded.Evidence.ReadyForClosure {
		t.Fatalf("unexpected loaded evidence: %+v", loaded.Evidence)
	}
	replayed, created, err := repo.Ensure(ctx, record)
	if err != nil || created || replayed.ID != record.ID {
		t.Fatalf("replay=%+v created=%v err=%v", replayed, created, err)
	}
	conflicting := record
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&conflicting.ID); err != nil {
		t.Fatal(err)
	}
	conflicting.EvidenceHash = strings.Repeat("e", 64)
	if _, _, err := repo.Ensure(ctx, conflicting); !errors.Is(err, ErrReconciliationConflict) {
		t.Fatalf("conflicting reconciliation err=%v", err)
	}
}
