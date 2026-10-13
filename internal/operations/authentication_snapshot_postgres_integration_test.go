package operations

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/identity"
	_ "campaign-platform/internal/persistence/database"
	pg "campaign-platform/internal/persistence/postgres"
)

func TestPostgreSQLAuthenticationExportSnapshotExcludesLateCommitsAndMutation(t *testing.T) {
	dsn := os.Getenv("POSTGRES_PAGINATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_PAGINATION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var maker, checker string
	if err := db.QueryRowContext(ctx, "SELECT gen_random_uuid()::text,gen_random_uuid()::text").Scan(&maker, &checker); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{maker, checker} {
		if _, err := db.ExecContext(ctx, "INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Authentication snapshot','DISABLED',false)", actor, "snapshot-"+actor+"@internal.invalid"); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2099, 10, 13, 3, 0, 0, 0, time.UTC)
	originalID, lateID, backdatedID := maker[:35]+"f", maker[:35]+"0", maker[:35]+"1"
	recorder := &pg.AuthenticationEventRecorder{DB: db}
	if err := recorder.Append(ctx, identity.AuthenticationEvent{
		ID: originalID, UserID: maker, Type: identity.EventLoginSucceeded,
		CorrelationID: "original-evidence", OccurredAt: now,
		EmailHash: "0011", SourceIP: "192.0.2.1", UserAgent: "private-agent",
		Metadata: map[string]string{"secret": "private"},
	}); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO authentication_events(id,user_id,event_type,correlation_id,occurred_at) VALUES($1::uuid,$2::uuid,'LOGIN_FAILED','uncommitted-lower-uuid',$3)", lateID, maker, now); err != nil {
		t.Fatal(err)
	}
	repository := &PostgreSQLRepository{DB: db}
	auditRepository := audit.NewMemoryRepository()
	service := &Service{Repo: repository, AuditRepository: auditRepository, AuthenticationEvents: recorder, Clock: func() time.Time { return now }}
	request, err := service.RequestExportWithOptions(ctx, "AUDIT_LOG", "", "JSON", "freeze scoped evidence", maker, "snapshot-request", ExportOptions{Criteria: map[string]any{"userId": maker}, TemplateVersion: "AUDIT-EXPORT-V2"})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.DecideExport(ctx, request.ID, request.Version, true, "", checker, "snapshot-approval")
	if err != nil {
		t.Fatal(err)
	}
	if approved.TemplateVersion != "AUDIT-EXPORT-V3" {
		t.Fatalf("pending legacy template was not normalized: %s", approved.TemplateVersion)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Append(ctx, identity.AuthenticationEvent{ID: backdatedID, UserID: maker, Type: identity.EventLoginFailed, CorrelationID: "late-backdated", OccurredAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE authentication_events SET event_type='LOGIN_FAILED',correlation_id='mutated-after-approval' WHERE id=$1::uuid", originalID); err != nil {
		t.Fatal(err)
	}
	persisted, err := repository.GetExport(ctx, approved.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker := &ExportWorker{AuditRepository: auditRepository}
	payload, err := worker.loadFrozenAudit(ctx, persisted)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.AuthenticationEvents) != 1 || payload.AuthenticationEvents[0].ID != originalID || payload.AuthenticationEvents[0].EventType != string(identity.EventLoginSucceeded) || payload.AuthenticationEvents[0].CorrelationID != "original-evidence" {
		t.Fatalf("approval snapshot changed after late commit or mutation: %+v", payload.AuthenticationEvents)
	}
}
