package execution

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLReleaseReservationsRejectsActiveCampaignAndIsIdempotent(t *testing.T) {
	dsn := os.Getenv("POSTGRES_EXECUTION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_EXECUTION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var orgID, purposeID, campaignID, userID, poolID, planID, reservationID string
	if err = db.QueryRowContext(ctx, `SELECT
		gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,
		gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,
		gen_random_uuid()::text`).Scan(
		&orgID, &purposeID, &campaignID, &userID, &poolID, &planID, &reservationID,
	); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_pool_capacity_reservations WHERE id=$1::uuid`, reservationID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_routing_plans WHERE id=$1::uuid`, planID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, userID)
	}()
	if _, err = db.ExecContext(ctx, `INSERT INTO internal_users(
		id,email,display_name,status,mfa_required
	) VALUES($1::uuid,$2,'Routing release actor','DISABLED',false)`,
		userID, "routing-release-"+userID+"@internal.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO organisations(
		id,legal_name,status
	) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "routing-release-"+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO consent_purposes(
		id,organisation_id,code,name,channel,wording_version
	) VALUES($1::uuid,$2::uuid,'ROUTE_RELEASE','Routing release','WHATSAPP','v1')`,
		purposeID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO campaigns(
		id,organisation_id,name,purpose_id,status,maximum_unique_recipients
	) VALUES($1::uuid,$2::uuid,'Routing release',$3::uuid,'DISPATCHING',1)`,
		campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO sender_pools(
		id,name,organisation_id,status,max_messages_per_minute,daily_capacity
	) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',10,100)`,
		poolID, "routing-release-"+poolID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO campaign_routing_plans(
		id,campaign_id,plan_version,routing_policy_version,capacity_evidence_version,
		pacing_policy_version,fallback_mode,approved_at,approved_by,idempotency_key,request_hash
	) VALUES($1::uuid,$2::uuid,1,'routing-v1','capacity-v1','pacing-v1','NONE',
		now(),$3::uuid,$4,$5)`, planID, campaignID, userID,
		"routing-release-"+planID, "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO campaign_pool_capacity_reservations(
		id,campaign_id,routing_plan_id,sender_pool_id,reservation_start,reservation_end,
		reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,status,fencing_version
	) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,now(),now()+interval '1 hour',
		10,100,100,'ACTIVE',2)`, reservationID, campaignID, planID, poolID); err != nil {
		t.Fatal(err)
	}
	store := &PostgreSQLRoutingPlanStore{DB: db}
	if err = store.ReleaseReservations(ctx, planID, userID, time.Now().UTC()); !errors.Is(err, ErrRoutingPlanConflict) {
		t.Fatalf("dispatching release error=%v", err)
	}
	var status string
	var fence int64
	if err = db.QueryRowContext(ctx, `SELECT status,fencing_version
		FROM campaign_pool_capacity_reservations WHERE id=$1::uuid`,
		reservationID).Scan(&status, &fence); err != nil {
		t.Fatal(err)
	}
	if status != "ACTIVE" || fence != 2 {
		t.Fatalf("unsafe release changed reservation status=%s fence=%d", status, fence)
	}
	if _, err = db.ExecContext(ctx, `UPDATE campaigns SET status='CANCELLED'
		WHERE id=$1::uuid`, campaignID); err != nil {
		t.Fatal(err)
	}
	if err = store.ReleaseReservations(ctx, planID, "   ", time.Now().UTC()); !errors.Is(err, ErrRoutingPlanInvalid) {
		t.Fatalf("anonymous release error=%v", err)
	}
	if err = db.QueryRowContext(ctx, `SELECT status,fencing_version
		FROM campaign_pool_capacity_reservations WHERE id=$1::uuid`,
		reservationID).Scan(&status, &fence); err != nil {
		t.Fatal(err)
	}
	if status != "ACTIVE" || fence != 2 {
		t.Fatalf("anonymous release changed reservation status=%s fence=%d", status, fence)
	}
	if err = store.ReleaseReservations(ctx, planID, userID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = store.ReleaseReservations(ctx, planID, userID, time.Now().UTC()); err != nil {
		t.Fatalf("idempotent release failed: %v", err)
	}
	if err = db.QueryRowContext(ctx, `SELECT status,fencing_version
		FROM campaign_pool_capacity_reservations WHERE id=$1::uuid`,
		reservationID).Scan(&status, &fence); err != nil {
		t.Fatal(err)
	}
	if status != "RELEASED" || fence != 3 {
		t.Fatalf("released reservation status=%s fence=%d", status, fence)
	}

	var releasedBy string
	var releasedAt time.Time
	if err = db.QueryRowContext(ctx, `SELECT released_by::text,released_at FROM campaign_pool_capacity_reservations WHERE id=$1::uuid`, reservationID).Scan(&releasedBy, &releasedAt); err != nil {
		t.Fatal(err)
	}
	if releasedBy != userID || releasedAt.IsZero() {
		t.Fatalf("release actor evidence releasedBy=%q releasedAt=%s", releasedBy, releasedAt)
	}

	var tamperActor string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&tamperActor); err != nil {
		t.Fatal(err)
	}
	tamperTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, auditTamperErr := tamperTx.ExecContext(ctx, `UPDATE campaign_pool_capacity_reservations SET released_by=$2 WHERE id=$1::uuid`, reservationID, tamperActor)
	_ = tamperTx.Rollback()
	if auditTamperErr == nil || !strings.Contains(auditTamperErr.Error(), "reservation release evidence is immutable") {
		t.Fatalf("released reservation attribution tamper was not rejected by immutability trigger: %v", auditTamperErr)
	}

	stateTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, invalidStateErr := stateTx.ExecContext(ctx, `UPDATE campaign_pool_capacity_reservations SET status='ACTIVE' WHERE id=$1::uuid`, reservationID)
	_ = stateTx.Rollback()
	if invalidStateErr == nil {
		t.Fatal("active reservation retained release timestamp/actor evidence")
	}

	deleteTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deleteTx.ExecContext(ctx, `DELETE FROM campaign_pool_capacity_reservations WHERE id=$1::uuid`, reservationID); err != nil {
		_ = deleteTx.Rollback()
		t.Fatal(err)
	}
	var tombstoneActor string
	var tombstoneAt time.Time
	var tombstoneFence int64
	if err := deleteTx.QueryRowContext(ctx, `SELECT coalesce(released_by,''),released_at,fencing_version FROM campaign_pool_capacity_reservation_release_tombstones WHERE reservation_id=$1::uuid`, reservationID).Scan(&tombstoneActor, &tombstoneAt, &tombstoneFence); err != nil {
		_ = deleteTx.Rollback()
		t.Fatalf("released reservation DELETE lost durable evidence: %v", err)
	}
	if tombstoneActor != userID || tombstoneAt.IsZero() || tombstoneFence != 3 {
		_ = deleteTx.Rollback()
		t.Fatalf("unexpected release tombstone actor=%q at=%s fence=%d", tombstoneActor, tombstoneAt, tombstoneFence)
	}
	_ = deleteTx.Rollback()
}
