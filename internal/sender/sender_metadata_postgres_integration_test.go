package sender

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLSenderOperationalMetadataGovernance(t *testing.T) {
	dsn := os.Getenv("POSTGRES_SENDER_METADATA_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_SENDER_METADATA_DATABASE_URL is not set")
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
	var actorID string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM internal_users ORDER BY created_at LIMIT 1`).Scan(&actorID); err != nil {
		t.Fatalf("load audit actor: %v", err)
	}
	store := &PostgreSQLGovernanceStore{DB: db}
	governance := &GovernanceService{Store: store}
	created, err := governance.RegisterSession(ctx, GovernedSession{
		MaskedMSISDN: "+234 *** 9001", OwnerReference: "operations-team", RegistrationCountryISO2: "NG",
		ProfileDisplayName: "Nigeria primary", RecoveryReference: "vault://sender/recovery/integration-old",
		EngineType: "WHATSAPP_WEB_JS", Status: StatusNew,
		SafeMessagesPerMinute: 10, SafeDailyCapacity: 100, InFlightLimit: 1,
	}, []byte{0x01, 0x02}, actorID, "register governed sender metadata")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_governance_events WHERE object_type='SESSION' AND object_id=$1::uuid`, created.ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, created.ID)
	}()
	if created.Version != 1 || created.OwnerReference != "operations-team" || created.RegistrationCountryISO2 != "NG" || created.ProfileDisplayName != "Nigeria primary" || !created.RecoveryReferenceConfigured {
		t.Fatalf("unexpected registered sender: %+v", created)
	}
	var rawRecovery string
	if err := db.QueryRowContext(ctx, `SELECT recovery_reference FROM sender_sessions WHERE id=$1::uuid`, created.ID).Scan(&rawRecovery); err != nil {
		t.Fatal(err)
	}
	if rawRecovery != "vault://sender/recovery/integration-old" {
		t.Fatalf("recovery reference not persisted: %q", rawRecovery)
	}
	var registerAudit int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sender_governance_events WHERE object_type='SESSION' AND object_id=$1::uuid AND action='REGISTERED'`, created.ID).Scan(&registerAudit); err != nil {
		t.Fatal(err)
	}
	if registerAudit != 1 {
		t.Fatalf("registered audit count=%d", registerAudit)
	}
	updated, err := governance.UpdateSessionMetadata(ctx, created.ID, created.Version, SessionOperationalMetadata{
		OwnerReference: "ghana-operations", RegistrationCountryISO2: "GH", ProfileDisplayName: "Ghana primary",
		RecoveryReference: "vault://sender/recovery/integration-new",
	}, actorID, "approved sender metadata ownership change")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.OwnerReference != "ghana-operations" || updated.RegistrationCountryISO2 != "GH" || !updated.RecoveryReferenceConfigured {
		t.Fatalf("unexpected updated sender: %+v", updated)
	}
	if _, err := governance.UpdateSessionMetadata(ctx, created.ID, 1, SessionOperationalMetadata{
		OwnerReference: "stale-team", RegistrationCountryISO2: "NG", ProfileDisplayName: "Stale",
		RecoveryReference: "vault://sender/recovery/stale",
	}, actorID, "stale sender metadata update"); !errors.Is(err, ErrSenderConflict) {
		t.Fatalf("expected stale-version conflict, got %v", err)
	}
	var metadataAudit int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sender_governance_events WHERE object_type='SESSION' AND object_id=$1::uuid AND action='METADATA_UPDATED'`, created.ID).Scan(&metadataAudit); err != nil {
		t.Fatal(err)
	}
	if metadataAudit != 1 {
		t.Fatalf("metadata audit count=%d", metadataAudit)
	}
	rawJSON, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rawJSON, []byte("vault://sender/recovery/integration-new")) {
		t.Fatalf("recovery reference leaked in sender JSON: %s", rawJSON)
	}
	var storedRecovery string
	if err := db.QueryRowContext(ctx, `SELECT recovery_reference FROM sender_sessions WHERE id=$1::uuid`, created.ID).Scan(&storedRecovery); err != nil {
		t.Fatal(err)
	}
	if storedRecovery != "vault://sender/recovery/integration-new" {
		t.Fatalf("updated recovery reference=%q", storedRecovery)
	}
}
