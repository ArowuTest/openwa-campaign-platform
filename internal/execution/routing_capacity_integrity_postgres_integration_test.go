package execution

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLRoutingCreateRejectsMismatchedReservationAuthority(t *testing.T) {
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
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	actorID := newDatabaseUUID(t, db, ctx)
	orgID := newDatabaseUUID(t, db, ctx)
	purposeID := newDatabaseUUID(t, db, ctx)
	campaignID := newDatabaseUUID(t, db, ctx)
	poolID := newDatabaseUUID(t, db, ctx)
	gatewayID := newDatabaseUUID(t, db, ctx)
	planID := newDatabaseUUID(t, db, ctx)
	reservationID := newDatabaseUUID(t, db, ctx)
	now := time.Date(2026, 8, 16, 6, 0, 0, 0, time.UTC)
	start := now.Add(time.Hour)
	end := now.Add(3 * time.Hour)

	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Capacity integrity actor','ACTIVE',false)`, actorID, "capacity-integrity-"+actorID[:8]+"@example.test")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Capacity integrity "+orgID[:8])
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Capacity integrity','WHATSAPP','v1')`, purposeID, orgID, "CAP_"+purposeID[:8])
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,requested_start_at,completion_deadline_at) VALUES($1::uuid,$2::uuid,'Capacity integrity',$3::uuid,'DRAFT',5,$4,$5)`, campaignID, orgID, purposeID, start, end)
	must(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',30,3000)`, poolID, "capacity-"+poolID[:8], orgID)
	must(`INSERT INTO gateway_pools(id,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version) VALUES($1::uuid,$2,'OPENWA','BAILEYS','0.13.0','ACTIVE','["SEND_TEXT"]'::jsonb,1,1)`, gatewayID, "capacity-gw-"+gatewayID[:8])
	defer func() {
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_pool_capacity_reservations WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_routing_plan_pools WHERE routing_plan_id=$1::uuid`, planID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_routing_plans WHERE id=$1::uuid`, planID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pools WHERE id=$1::uuid`, gatewayID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()

	plan := RoutingPlan{
		ID: planID, CampaignID: campaignID, DistributionMode: DistributionWeighted,
		RoutingPolicyVersion: "rp-capacity-integrity", CapacityEvidenceVersion: "cap-capacity-integrity",
		PacingPolicyVersion: "pace-capacity-integrity", FallbackMode: "NONE", ApprovedAt: now,
		ApprovedBy: actorID, IdempotencyKey: "capacity-integrity-" + campaignID,
		RequestHash: "capacity-integrity-hash-" + campaignID,
	}
	plan.Routes = []PoolRoute{{
		SenderPoolID: poolID, GatewayPoolID: gatewayID, Provider: "OPENWA", Engine: "BAILEYS",
		AllocationWeight: 100, MaximumRecipients: 5, ReservedMessagesPerMinute: 10,
		ReservedHourlyUnits: 100, ReservedDailyUnits: 500,
	}}
	reservations := []CapacityReservation{{
		ID: reservationID, CampaignID: campaignID, RoutingPlanID: planID, SenderPoolID: poolID,
		ReservationStart: start, ReservationEnd: end,
		// Deliberately larger than both the frozen route authority (10) and pool capacity (30).
		ReservedMessagesPerMinute: 31, ReservedHourlyUnits: 100, ReservedDailyUnits: 500,
		Status: "HELD", FencingVersion: 1, CreatedAt: now, UpdatedAt: now,
	}}

	_, err = (&PostgreSQLRoutingPlanStore{DB: db}).Create(ctx, plan, reservations)
	if !errors.Is(err, ErrRoutingPlanInvalid) {
		t.Fatalf("mismatched reservation authority was accepted; error=%v", err)
	}
	plan.Routes[0].ReservedMessagesPerMinute = -1
	reservations[0].ReservedMessagesPerMinute = -1
	if _, err := (&PostgreSQLRoutingPlanStore{DB: db}).Create(ctx, plan, reservations); !errors.Is(err, ErrRoutingPlanInvalid) {
		t.Fatalf("negative capacity authority was not rejected at store boundary: %v", err)
	}
	maximumInt := int(^uint(0) >> 1)
	plan.Routes[0].ReservedMessagesPerMinute = maximumInt
	reservations[0].ReservedMessagesPerMinute = maximumInt
	if _, err := (&PostgreSQLRoutingPlanStore{DB: db}).Create(ctx, plan, reservations); !errors.Is(err, ErrCapacityOverbooked) {
		t.Fatalf("overflow-sized capacity authority did not fail closed as overbooked: %v", err)
	}
}
