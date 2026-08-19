package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
	"campaign-platform/internal/sender"
)

func TestCouncilMetaEndpointValidatorRejectsAmbiguousMetaSender(t *testing.T) {
	err := validateAllocationProviderEndpoint("", "", "meta-sender-1", sender.AllocationRoute{SenderPoolID: "pool-1"})
	var permanent PermanentMaterialError
	if !errors.As(err, &permanent) {
		t.Fatalf("ambiguous Meta sender accepted: %v", err)
	}
}
func TestCouncilAllocationEndpointRejectsUnfrozenProvider(t *testing.T) {
	err := validateAllocationProviderEndpoint("", "", "", sender.AllocationRoute{SenderPoolID: "pool-1"})
	var permanent PermanentMaterialError
	if !errors.As(err, &permanent) {
		t.Fatalf("unfrozen provider was accepted as OpenWA authority: %v", err)
	}
}

func TestCouncilPostgreSQLRejectsMetaSenderWithoutExplicitMetaRoute(t *testing.T) {
	dsn := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_META_CLOUD_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ids := make([]string, 6)
	args := make([]any, 6)
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, purposeID, campaignID, poolID, senderID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	suffix := orgID[:8]
	must := func(q string, a ...any) {
		t.Helper()
		if _, e := db.ExecContext(ctx, q, a...); e != nil {
			t.Fatal(e)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Meta coherence actor','ACTIVE',false)`, actorID, "meta-coherence-"+suffix+"@example.test")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Meta coherence "+suffix)
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Meta coherence','WHATSAPP','v1')`, purposeID, orgID, "MC_"+suffix)
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Meta coherence',$3::uuid,'SCHEDULED',1)`, campaignID, orgID, purposeID)
	must(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',10,100)`, poolID, "meta-coherence-"+suffix, orgID)
	must(`INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,version,created_by,reason) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,'Meta coherence','+234 ***','meta-coherence-key','v23.0','DRAFT','HEALTH_UNKNOWN',1,$6::uuid,'create coherence sender')`, senderID, orgID, poolID, "waba-"+suffix, "phone-"+suffix, actorID)
	defer func() {
		c := context.Background()
		_, _ = db.ExecContext(c, `DELETE FROM meta_cloud_senders WHERE id=$1::uuid`, senderID)
		_, _ = db.ExecContext(c, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(c, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(c, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(c, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(c, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	if _, err := db.ExecContext(ctx, `UPDATE campaigns SET transport_sender_pool_id=$2::uuid,meta_sender_id=$3::uuid WHERE id=$1::uuid`, campaignID, poolID, senderID); err == nil {
		t.Fatal("database accepted Meta sender without explicit META/CLOUD_API route")
	}
}
