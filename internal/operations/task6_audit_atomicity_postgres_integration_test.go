package operations

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/audit"
	"campaign-platform/internal/outbox"
	_ "campaign-platform/internal/persistence/database"
)

type task6FailingAuditRepository struct{}

func (task6FailingAuditRepository) Head(context.Context) (uint64, string, error) {
	return 0, "", errors.New("injected audit persistence failure")
}
func (task6FailingAuditRepository) Append(context.Context, audit.Event, uint64, string) (audit.Event, error) {
	return audit.Event{}, errors.New("injected audit persistence failure")
}
func (task6FailingAuditRepository) List(context.Context, uint64, int) ([]audit.Event, error) {
	return nil, errors.New("injected audit persistence failure")
}
func (task6FailingAuditRepository) Search(context.Context, audit.Query) (audit.Page, error) {
	return audit.Page{}, errors.New("injected audit persistence failure")
}
func (task6FailingAuditRepository) Verify(context.Context) error {
	return errors.New("injected audit persistence failure")
}
func TestTask6SensitiveMutationCannotCommitWithoutDurableAuditEvidence(t *testing.T) {
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
	var actorID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Task 6 audit actor','DISABLED',false)`, actorID, "task6-audit-atomic-"+actorID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	svc := &Service{Repo: &PostgreSQLRepository{DB: db}, Audit: audit.NewRecorder(task6FailingAuditRepository{}), Clock: func() time.Time { return now }}
	incident, createErr := svc.CreateIncident(ctx, Incident{Category: "GATEWAY", Severity: SeverityCritical, Summary: "Task 6 audit failure injection"}, actorID, "task6-audit-atomic-"+actorID)
	var mutationCount, incidentEventCount int
	if incident.ID != "" {
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM operations_incidents WHERE id=$1::uuid`, incident.ID).Scan(&mutationCount); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM operational_incident_events WHERE incident_id=$1::uuid`, incident.ID).Scan(&incidentEventCount); err != nil {
			t.Fatal(err)
		}
	}
	correlation := "task6-audit-atomic-" + actorID
	var chainedAudit, durableAudit int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE correlation_id=$1`, correlation).Scan(&chainedAudit); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM transactional_outbox WHERE aggregate_type='AUDIT' AND payload->>'correlationId'=$1`, correlation).Scan(&durableAudit); err != nil {
		t.Fatal(err)
	}
	if mutationCount == 1 && chainedAudit+durableAudit == 0 {
		t.Fatalf("incident committed without durable audit evidence: createErr=%v incidentEvents=%d", createErr, incidentEventCount)
	}
	if mutationCount == 0 && createErr == nil {
		t.Fatal("incident reported success without committing the mutation")
	}
	if mutationCount == 1 {
		if createErr != nil || incidentEventCount != 1 || durableAudit != 1 {
			t.Fatalf("atomic incident evidence incomplete: createErr=%v incidentEvents=%d durableAudit=%d", createErr, incidentEventCount, durableAudit)
		}
		var outboxID string
		if err := db.QueryRowContext(ctx, `SELECT id::text FROM transactional_outbox WHERE aggregate_type='AUDIT' AND payload->>'correlationId'=$1`, correlation).Scan(&outboxID); err != nil {
			t.Fatal(err)
		}
		outboxRepo := &outbox.PostgreSQLRepository{DB: db}
		record, err := outboxRepo.Get(ctx, outboxID)
		if err != nil {
			t.Fatal(err)
		}
		auditRepo := &audit.PostgreSQLRepository{DB: db}
		publisher := &outbox.Publisher{Outbox: outboxRepo, Audit: audit.NewRecorder(auditRepo)}
		if err := publisher.Publish(ctx, record); err != nil {
			t.Fatal(err)
		}
		if err := publisher.Publish(ctx, record); err != nil {
			t.Fatalf("audit outbox replay failed: %v", err)
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE correlation_id=$1`, correlation).Scan(&chainedAudit); err != nil {
			t.Fatal(err)
		}
		if chainedAudit != 1 {
			t.Fatalf("published audit events=%d want=1", chainedAudit)
		}
		if err := auditRepo.Verify(ctx); err != nil {
			t.Fatalf("audit chain invalid after outbox publication: %v", err)
		}
	}
}
