package execution

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestCouncilPostgreSQLReservationReleaseUsesCampaignFirstLockOrder(t *testing.T) {
	dsn := os.Getenv("POSTGRES_EXECUTION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_EXECUTION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(8)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ids := make([]string, 7)
	args := make([]any, 7)
	for i := range ids {
		args[i] = &ids[i]
	}
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(args...); err != nil {
		t.Fatal(err)
	}
	orgID, purposeID, campaignID, userID, poolID, planID, reservationID := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6]
	must := func(q string, a ...any) {
		t.Helper()
		if _, e := db.ExecContext(ctx, q, a...); e != nil {
			t.Fatal(e)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Lock order actor','DISABLED',false)`, userID, "lock-order-"+userID+"@internal.invalid")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "lock-order-"+orgID)
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,'LOCK_ORDER','Lock order','WHATSAPP','v1')`, purposeID, orgID)
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Lock order',$3::uuid,'CANCELLED',1)`, campaignID, orgID, purposeID)
	must(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',10,100)`, poolID, "lock-order-"+poolID, orgID)
	must(`INSERT INTO campaign_routing_plans(id,campaign_id,plan_version,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by,idempotency_key,request_hash) VALUES($1::uuid,$2::uuid,1,'routing-v1','capacity-v1','pacing-v1','NONE',now(),$3::uuid,$4,$5)`, planID, campaignID, userID, "lock-order-"+planID, "0123456789abcdef0123456789abcdef")
	must(`INSERT INTO campaign_pool_capacity_reservations(id,campaign_id,routing_plan_id,sender_pool_id,reservation_start,reservation_end,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,status,fencing_version) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,now(),now()+interval '1 hour',10,100,100,'ACTIVE',2)`, reservationID, campaignID, planID, poolID)
	defer func() {
		c := context.Background()
		_, _ = db.ExecContext(c, `DELETE FROM campaign_pool_capacity_reservations WHERE id=$1::uuid`, reservationID)
		_, _ = db.ExecContext(c, `DELETE FROM campaign_routing_plans WHERE id=$1::uuid`, planID)
		_, _ = db.ExecContext(c, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(c, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(c, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(c, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(c, `DELETE FROM internal_users WHERE id=$1::uuid`, userID)
	}()
	txCampaign, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	defer txCampaign.Rollback()
	txRelease, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	defer txRelease.Rollback()
	var campaignPID, releasePID int
	if err := txCampaign.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&campaignPID); err != nil {
		t.Fatal(err)
	}
	if err := txRelease.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&releasePID); err != nil {
		t.Fatal(err)
	}
	var locked string
	if err := txCampaign.QueryRowContext(ctx, `SELECT id::text FROM campaigns WHERE id=$1::uuid FOR UPDATE`, campaignID).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	releaseDone := make(chan error, 1)
	go func() {
		err := releaseRoutingReservations(ctx, txRelease, planID, "", userID, time.Now().UTC())
		if err == nil {
			err = txRelease.Commit()
		}
		releaseDone <- err
	}()
	blocked := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.QueryRowContext(ctx, `SELECT $1::int = ANY(pg_blocking_pids($2::int))`, campaignPID, releasePID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("release did not reach campaign lock while campaign was held")
	}
	planCtx, planCancel := context.WithTimeout(ctx, 3*time.Second)
	defer planCancel()
	if err := txCampaign.QueryRowContext(planCtx, `SELECT id::text FROM campaign_routing_plans WHERE id=$1::uuid FOR UPDATE`, planID).Scan(&locked); err != nil {
		t.Fatalf("campaign-first transaction could not acquire routing plan after holding campaign: %v", err)
	}
	if err := txCampaign.Rollback(); err != nil && err != sql.ErrTxDone {
		t.Fatal(err)
	}
	select {
	case err := <-releaseDone:
		if err != nil {
			t.Fatalf("release transaction failed after campaign lock cleared: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("release transaction did not complete")
	}
}
