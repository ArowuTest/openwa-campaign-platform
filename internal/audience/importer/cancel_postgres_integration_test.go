package importer

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLPreviewReadyImportCanCancelBeforeMerge(t *testing.T) {
	dsn := os.Getenv("POSTGRES_IMPORT_GOVERNANCE_DATABASE_URL")
	if dsn == "" { t.Skip("POSTGRES_IMPORT_GOVERNANCE_DATABASE_URL is not set") }
	db, err := sql.Open("postgres", dsn); if err != nil { t.Fatal(err) }; defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second); defer cancel()
	if err := db.PingContext(ctx); err != nil { t.Fatal(err) }
	ids := make([]string, 5); args := make([]any, len(ids)); for i := range ids { args[i] = &ids[i] }
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil { t.Fatal(err) }
	actorID, orgID, reviewID, purposeID, importID := ids[0], ids[1], ids[2], ids[3], ids[4]
	now := time.Date(2099, 11, 1, 12, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Import Cancel Actor','DISABLED',false)`, actorID, "cancel-"+actorID+"@internal.invalid"); err != nil { t.Fatal(err) }
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Cancel Org "+orgID); err != nil { t.Fatal(err) }
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,status) VALUES($1::uuid,$2::uuid,'Cancel review','WHATSAPP','DIRECT','v1','DRAFT')`, reviewID, orgID); err != nil { t.Fatal(err) }
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,'Cancel purpose','WHATSAPP','v1',$4::uuid)`, purposeID, orgID, "CANCEL_"+purposeID, reviewID); err != nil { t.Fatal(err) }
	if _, err := db.ExecContext(ctx, `INSERT INTO audience_imports(id,organisation_id,consent_review_id,purpose_id,channel,wording_version,source_name,object_key,original_filename,file_sha256,byte_size,mapping,status,detected_media_type,template_version,update_policy,malware_scan_status,content_signature_valid,client_request_id,uploaded_by,version) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,'WHATSAPP','v1','cancel-source','imports/cancel.csv','cancel.csv',$5,100,'{}'::jsonb,'PREVIEW_READY','text/csv','v1','NEWEST_SOURCE','CLEAN',true,$6,$7::uuid,4)`, importID, orgID, reviewID, purposeID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "cancel-request-"+importID, actorID); err != nil { t.Fatal(err) }
	svc := &ImportService{Repository: &PostgreSQLImportRepository{DB: db}, Clock: func() time.Time { return now }}
	got, err := svc.Cancel(ctx, importID, actorID, "preview shows the wrong audience source", 4)
	if err != nil { t.Fatal(err) }
	if got.Status != ImportCancelled || got.Version != 5 || got.FailureReason != "preview shows the wrong audience source" { t.Fatalf("unexpected cancelled import: %+v", got) }
	var status, reason string; var version int64
	if err := db.QueryRowContext(ctx, `SELECT status,coalesce(failure_reason,''),version FROM audience_imports WHERE id=$1::uuid`, importID).Scan(&status,&reason,&version); err != nil { t.Fatal(err) }
	if status != string(ImportCancelled) || reason != got.FailureReason || version != 5 { t.Fatalf("persisted cancellation status=%s reason=%q version=%d",status,reason,version) }
}
