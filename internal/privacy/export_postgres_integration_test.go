package privacy

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLPrivacyExportAppendsControlledEvent(t *testing.T) {
	dsn := os.Getenv("POSTGRES_CONTACT_RECTIFICATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_CONTACT_RECTIFICATION_DATABASE_URL is not set")
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

	var creatorID, executorID, exporterID, caseID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&creatorID, &executorID, &exporterID, &caseID); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []struct{ id, name string }{
		{creatorID, "Privacy Creator"}, {executorID, "Privacy Executor"}, {exporterID, "Privacy Exporter"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,$3,'DISABLED',false)`, actor.id, "privacy-export-"+actor.id+"@internal.invalid", actor.name); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM privacy_cases WHERE id=$1::uuid`, caseID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id IN ($1::uuid,$2::uuid,$3::uuid)`, creatorID, executorID, exporterID)
	}()

	now := time.Now().UTC().Truncate(time.Microsecond)
	checksum := strings.Repeat("a", 64)
	if _, err := db.ExecContext(ctx, `INSERT INTO privacy_cases(
		id,case_type,status,subject_lookup_hmac,subject_masked,requested_at,due_at,created_by,executed_by,
		request_reason,execution_reason,result_ciphertext,result_key_version,result_sha256,completed_at,version,created_at,updated_at
	) VALUES($1::uuid,'PORTABILITY','COMPLETED',decode('010203','hex'),'***5678',$2,$3,$4::uuid,$5::uuid,
		'approved portability request','completed portability package',$6,'v1',$7,$8,4,$2,$8)`,
		caseID, now.Add(-2*time.Hour), now.Add(24*time.Hour), creatorID, executorID, []byte{1, 2, 3, 4}, checksum, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	service := &Service{Repository: &PostgreSQLRepository{DB: db}, Clock: func() time.Time { return now }}
	envelope, err := service.ExportEnvelope(ctx, caseID, exporterID, "privacy-export-postgres")
	if err != nil {
		t.Fatal(err)
	}
	if len(envelope) == 0 || !strings.Contains(string(envelope), checksum) {
		t.Fatalf("controlled export envelope missing result evidence: %s", string(envelope))
	}

	var eventType, actorID, eventChecksum string
	if err := db.QueryRowContext(ctx, `SELECT event_type,actor_id::text,evidence->>'resultSha256'
		FROM privacy_case_events WHERE privacy_case_id=$1::uuid ORDER BY occurred_at DESC,id DESC LIMIT 1`, caseID).
		Scan(&eventType, &actorID, &eventChecksum); err != nil {
		t.Fatal(err)
	}
	if eventType != "EXPORT_REQUESTED" || actorID != exporterID || eventChecksum != checksum {
		t.Fatalf("export event type=%q actor=%q checksum=%q", eventType, actorID, eventChecksum)
	}
}
