package metacloud

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLMetaSenderRejectsSharedOrCrossTenantPool(t *testing.T) {
	dsn := os.Getenv("POSTGRES_META_CLOUD_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_META_CLOUD_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ids := make([]string, 5)
	args := make([]any, len(ids))
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	actorID, orgID, otherOrgID, sharedPoolID, otherPoolID := ids[0], ids[1], ids[2], ids[3], ids[4]
	must := func(q string, a ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, a...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Meta ownership actor','DISABLED',false)`, actorID, "meta-owner-"+actorID+"@internal.invalid")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE'),($3::uuid,$4,'ACTIVE')`, orgID, "Meta owner "+orgID, otherOrgID, "Other owner "+otherOrgID)
	must(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,NULL,'ACTIVE',10,100),($3::uuid,$4,$5::uuid,'ACTIVE',10,100)`, sharedPoolID, "shared-"+sharedPoolID, otherPoolID, "other-"+otherPoolID, otherOrgID)
	defer func() {
		c := context.Background()
		_, _ = db.ExecContext(c, `DELETE FROM meta_cloud_senders WHERE credential_key LIKE 'r11-meta-owner-%'`)
		_, _ = db.ExecContext(c, `DELETE FROM sender_pools WHERE id IN ($1::uuid,$2::uuid)`, sharedPoolID, otherPoolID)
		_, _ = db.ExecContext(c, `DELETE FROM organisations WHERE id IN ($1::uuid,$2::uuid)`, orgID, otherOrgID)
		_, _ = db.ExecContext(c, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	insert := `INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,effective_from,version,created_by,reason) VALUES(gen_random_uuid(),$1::uuid,$2::uuid,$3,$4,'R11 owner','+234 ***',$5,'v23.0','DRAFT','UNKNOWN',now(),1,$6::uuid,'R11 ownership test')`
	sharedAccepted := false
	if _, err := db.ExecContext(ctx, insert, orgID, sharedPoolID, "waba-shared-"+orgID[:8], "phone-shared-"+orgID[:8], "r11-meta-owner-shared-"+orgID[:8], actorID); err == nil {
		sharedAccepted = true
	}
	crossTenantAccepted := false
	if _, err := db.ExecContext(ctx, insert, orgID, otherPoolID, "waba-cross-"+orgID[:8], "phone-cross-"+orgID[:8], "r11-meta-owner-cross-"+orgID[:8], actorID); err == nil {
		crossTenantAccepted = true
	}
	if sharedAccepted || crossTenantAccepted {
		t.Fatalf("invalid Meta sender pool ownership accepted: shared=%v crossTenant=%v", sharedAccepted, crossTenantAccepted)
	}
}
