package execution

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLCampaignFrozenEvidenceRejectsNullProviderAndAdapterOnlyShapes(t *testing.T) {
	dsn := os.Getenv("POSTGRES_EXECUTION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_EXECUTION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var actorID, orgID, purposeID, campaignID, definitionID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&actorID, &orgID, &purposeID, &campaignID, &definitionID); err != nil {
		t.Fatal(err)
	}
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'R11 schema actor','DISABLED',false)`, actorID, "r11-schema-"+actorID+"@internal.invalid")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "R11 schema "+orgID)
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'R11 schema','WHATSAPP','v1')`, purposeID, orgID, "R11_"+purposeID[:8])
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'R11 frozen evidence',$3::uuid,'DRAFT',1)`, campaignID, orgID, purposeID)
	must(`INSERT INTO provider_capability_definitions(id,provider,channel,engine,adapter_version,capabilities,status,effective_from,version,created_by,reason,created_at,updated_at) VALUES($1::uuid,'OPENWA','WHATSAPP','BAILEYS','0.13.0',ARRAY['SEND_TEXT'],'DRAFT',now(),1,$2::uuid,'R11 frozen evidence',now(),now())`, definitionID, actorID)
	defer func() {
		cleanup := context.Background()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM provider_capability_definitions WHERE id=$1::uuid`, definitionID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	nullProviderAccepted := false
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET provider_capability_definition_id=$2::uuid,provider_capability_definition_version=1,provider_adapter_version='0.13.0',transport_provider=NULL,gateway_pool_version=NULL WHERE id=$1::uuid`, campaignID, definitionID); err == nil {
		nullProviderAccepted = true
	}
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET provider_capability_definition_id=NULL,provider_capability_definition_version=NULL,gateway_pool_version=NULL,provider_adapter_version=NULL,transport_provider=NULL WHERE id=$1::uuid`, campaignID); err != nil {
		t.Fatal(err)
	}
	adapterOnlyAccepted := false
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET provider_adapter_version='0.13.0' WHERE id=$1::uuid`, campaignID); err == nil {
		adapterOnlyAccepted = true
	}
	if nullProviderAccepted || adapterOnlyAccepted {
		t.Fatalf("invalid frozen campaign evidence accepted: nullProvider=%v adapterOnly=%v", nullProviderAccepted, adapterOnlyAccepted)
	}
}
