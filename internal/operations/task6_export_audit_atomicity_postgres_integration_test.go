package operations

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/audit"
	_ "campaign-platform/internal/persistence/database"
)

func TestTask6ExportGovernanceCannotCommitWithoutDurableAuditEvidence(t *testing.T) {
	dsn := os.Getenv("POSTGRES_OPERATIONS_AUDIT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_OPERATIONS_AUDIT_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var makerID, checkerID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&makerID, &checkerID); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{makerID, checkerID} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 export audit actor','DISABLED',false)`, actor, "task6-export-audit-"+actor+"@internal.invalid"); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	svc := &Service{Repo: &PostgreSQLRepository{DB: db}, Audit: audit.NewRecorder(task6FailingAuditRepository{}), Clock: func() time.Time { return now }}
	requestCorrelation := "task6-export-request-" + makerID
	export, requestErr := svc.RequestExport(ctx, "AUDIT_LOG", "", "JSON", "task 6 governed export request", makerID, requestCorrelation)
	if export.ID == "" {
		t.Fatalf("export request did not return an identifier: %v", requestErr)
	}
	var requestRow, requestAudit int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM export_requests WHERE id=$1::uuid`, export.ID).Scan(&requestRow); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM transactional_outbox WHERE aggregate_type='AUDIT' AND payload->>'correlationId'=$1`, requestCorrelation).Scan(&requestAudit); err != nil {
		t.Fatal(err)
	}
	if requestRow == 1 && requestAudit == 0 {
		t.Fatalf("export request committed without durable audit evidence: %v", requestErr)
	}
	decisionCorrelation := "task6-export-reject-" + checkerID
	rejected, decisionErr := svc.DecideExport(ctx, export.ID, export.Version, false, "independent governed rejection", checkerID, decisionCorrelation)
	var decisionAudit int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM transactional_outbox WHERE aggregate_type='AUDIT' AND payload->>'correlationId'=$1`, decisionCorrelation).Scan(&decisionAudit); err != nil {
		t.Fatal(err)
	}
	var storedStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM export_requests WHERE id=$1::uuid`, export.ID).Scan(&storedStatus); err != nil {
		t.Fatal(err)
	}
	if storedStatus == string(ExportRejected) && decisionAudit == 0 {
		t.Fatalf("export rejection committed without durable audit evidence: decision=%+v err=%v", rejected, decisionErr)
	}
}
