package audit

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLAuditEventIsCompleteVerifiableAndAppendOnly(t *testing.T) {
	dsn := os.Getenv("POSTGRES_AUDIT_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_AUDIT_DATABASE_URL is not set")
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
	var actorID, orgID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actorID, &orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "ADM audit "+orgID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	recorder := NewRecorder(&PostgreSQLRepository{DB: db})
	created, err := recorder.Record(ctx, Input{
		ActorType: "USER", ActorID: actorID, Action: "ADM_ACCEPTANCE_CHANGE", ObjectType: "CONFIGURATION", ObjectID: "adm-acceptance",
		OrganisationID: orgID, Outcome: "SUCCESS", Sensitivity: "INTERNAL", Before: map[string]any{"version": 1}, After: map[string]any{"version": 2},
		ReasonCode: "APPROVED_CHANGE", Reason: "verify complete append-only audit evidence", IPAddress: "192.0.2.10", Device: "adm-test-agent",
		CorrelationID: "adm-audit-" + actorID, OccurredAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Sequence == 0 || created.Hash == "" {
		t.Fatalf("missing audit chain evidence: %+v", created)
	}
	repo := &PostgreSQLRepository{DB: db}
	page, err := repo.Search(ctx, Query{CorrelationID: created.CorrelationID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("audit search returned %d events", len(page.Items))
	}
	got := page.Items[0]
	if got.ActorID != actorID || got.Action != "ADM_ACCEPTANCE_CHANGE" || got.ObjectType != "CONFIGURATION" || got.ObjectID != "adm-acceptance" || got.OrganisationID != orgID || got.Outcome != "SUCCESS" || got.Reason == "" || got.IPAddress == "" || got.Device == "" || got.CorrelationID != created.CorrelationID || len(got.Before) == 0 || len(got.After) == 0 {
		t.Fatalf("audit event is incomplete: %+v", got)
	}
	if err := repo.Verify(ctx); err != nil {
		t.Fatalf("audit chain verification failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE audit_events SET action='TAMPERED' WHERE id=$1::uuid`, created.ID); err == nil {
		t.Fatal("audit UPDATE was accepted")
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM audit_events WHERE id=$1::uuid`, created.ID); err == nil {
		t.Fatal("audit DELETE was accepted")
	}
}
