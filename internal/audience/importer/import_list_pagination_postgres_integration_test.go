package importer

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLImportListPageIsOrganisationScopedAndCursorStable(t *testing.T) {
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

	ids := make([]string, 7)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	makerID := ids[0]
	org1, review1, purpose1 := ids[1], ids[2], ids[3]
	org2, review2, purpose2 := ids[4], ids[5], ids[6]
	now := time.Date(2099, 1, 2, 12, 0, 0, 0, time.UTC)

	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Import List Maker','DISABLED',false)`, makerID, "import-list-"+makerID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct{ org, review, purpose, suffix string }{
		{org1, review1, purpose1, "one"},
		{org2, review2, purpose2, "two"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, fixture.org, "Import List "+fixture.suffix); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO consent_reviews(id,organisation_id,name,channel,consent_source,wording_version,privacy_notice_reviewed,opt_out_process_reviewed,sample_records_reviewed,status,reviewed_by,reviewed_at,expires_at,outcome) VALUES($1::uuid,$2::uuid,$3,'WHATSAPP','DIRECT','v1',true,true,true,'APPROVED',$4::uuid,$5,$6,'APPROVED')`, fixture.review, fixture.org, "Review "+fixture.suffix, makerID, now.Add(-time.Hour), now.Add(24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version,consent_review_id) VALUES($1::uuid,$2::uuid,$3,$4,'WHATSAPP','v1',$5::uuid)`, fixture.purpose, fixture.org, "PURPOSE_"+fixture.suffix, "Purpose "+fixture.suffix, fixture.review); err != nil {
			t.Fatal(err)
		}
	}

	repository := &PostgreSQLImportRepository{DB: db}
	create := func(org, review, purpose string, index int, at time.Time) ImportBatch {
		t.Helper()
		batch, err := NewImportBatch(CreateImportInput{
			OrganisationID: org, ConsentReviewID: review, PurposeID: purpose,
			Channel: "WHATSAPP", WordingVersion: "v1",
			SourceName: fmt.Sprintf("Postgres source %d", index), SourceSystem: "CRM",
			ObjectKey:        fmt.Sprintf("private/postgres/%d.csv", index),
			OriginalFilename: fmt.Sprintf("pg-%d.csv", index), DetectedMediaType: "text/csv",
			FileSHA256: fmt.Sprintf("%064x", 9000+index), ByteSize: int64(1000 + index),
			TemplateVersion: "v1", Mapping: ColumnMapping{MSISDN: "msisdn"},
			UpdatePolicy: UpdateNewestSource, UploadedBy: makerID,
			ClientRequestID: fmt.Sprintf("postgres-import-list-%016d", index), ContentSignatureValid: true,
		}, at)
		if err != nil {
			t.Fatal(err)
		}
		value, created, err := repository.Create(ctx, batch)
		if err != nil || !created {
			t.Fatalf("create import %d: created=%v err=%v", index, created, err)
		}
		return value
	}
	create(org1, review1, purpose1, 1, now.Add(time.Minute))
	create(org1, review1, purpose1, 2, now.Add(2*time.Minute))
	create(org2, review2, purpose2, 3, now.Add(3*time.Minute))
	create(org1, review1, purpose1, 4, now.Add(4*time.Minute))

	service := &ImportService{Repository: repository}
	first, err := service.ListPage(ctx, org1, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].SourceName != "Postgres source 4" || first.Items[1].SourceName != "Postgres source 2" || first.NextCursor == "" {
		t.Fatalf("first page=%+v", first)
	}
	second, err := service.ListPage(ctx, org1, 2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].SourceName != "Postgres source 1" || second.NextCursor != "" {
		t.Fatalf("second page=%+v", second)
	}
}
