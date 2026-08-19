package execution

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLCapacityReservationsRejectDeleteWhileLive(t *testing.T) {
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
	actorID := newDatabaseUUID(t, db, ctx)
	orgID := newDatabaseUUID(t, db, ctx)
	purposeID := newDatabaseUUID(t, db, ctx)
	campaignID := newDatabaseUUID(t, db, ctx)
	poolID := newDatabaseUUID(t, db, ctx)
	planID := newDatabaseUUID(t, db, ctx)
	reservationID := newDatabaseUUID(t, db, ctx)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	must := func(q string, args ...any) {
		t.Helper()
		if _, e := tx.ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'R17 capacity actor','ACTIVE',false)`, actorID, "r17-capacity-"+actorID[:8]+"@example.test")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "R17 capacity "+orgID[:8])
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'R17 capacity','WHATSAPP','v1')`, purposeID, orgID, "R17_"+purposeID[:8])
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,requested_start_at,completion_deadline_at) VALUES($1::uuid,$2::uuid,'R17 capacity',$3::uuid,'DRAFT',1,now()+interval '1 hour',now()+interval '2 hours')`, campaignID, orgID, purposeID)
	must(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',10,100)`, poolID, "r17-capacity-"+poolID[:8], orgID)
	must(`INSERT INTO campaign_routing_plans(id,campaign_id,plan_version,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by,idempotency_key,request_hash) VALUES($1::uuid,$2::uuid,1,'r17-route','r17-capacity','r17-pacing','NONE',now(),$3::uuid,$4,$5)`, planID, campaignID, actorID, "r17-live-delete-"+planID, "r17-hash-"+planID)
	must(`INSERT INTO campaign_pool_capacity_reservations(id,campaign_id,routing_plan_id,sender_pool_id,reservation_start,reservation_end,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,status,fencing_version) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,now()+interval '1 hour',now()+interval '2 hours',10,100,100,'HELD',1)`, reservationID, campaignID, planID, poolID)
	if _, err := tx.ExecContext(ctx, `DELETE FROM campaign_pool_capacity_reservations WHERE id=$1::uuid`, reservationID); err == nil {
		t.Fatal("live HELD capacity reservation could be deleted without release")
	}
}
