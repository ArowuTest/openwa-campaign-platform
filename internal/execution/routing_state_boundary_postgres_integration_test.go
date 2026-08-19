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

func TestPostgreSQLRoutingCreateRechecksCampaignStateUnderLock(t *testing.T) {
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
	actorID := newDatabaseUUID(t, db, ctx)
	orgID := newDatabaseUUID(t, db, ctx)
	purposeID := newDatabaseUUID(t, db, ctx)
	campaignID := newDatabaseUUID(t, db, ctx)
	poolID := newDatabaseUUID(t, db, ctx)
	gatewayID := newDatabaseUUID(t, db, ctx)
	planID := newDatabaseUUID(t, db, ctx)
	reservationID := newDatabaseUUID(t, db, ctx)
	now := time.Date(2026, 8, 16, 21, 0, 0, 0, time.UTC)
	start := now.Add(time.Hour)
	end := now.Add(3 * time.Hour)

	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Routing state actor','ACTIVE',false)`, actorID, "routing-state-"+actorID[:8]+"@example.test")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Routing state "+orgID[:8])
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Routing state','WHATSAPP','v1')`, purposeID, orgID, "RST_"+purposeID[:8])
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,requested_start_at,completion_deadline_at) VALUES($1::uuid,$2::uuid,'Routing state',$3::uuid,'DISPATCHING',5,$4,$5)`, campaignID, orgID, purposeID, start, end)
	must(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',30,3000)`, poolID, "routing-state-"+poolID[:8], orgID)
	must(`INSERT INTO gateway_pools(id,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version) VALUES($1::uuid,$2,'OPENWA','BAILEYS','0.13.0','ACTIVE','["SEND_TEXT"]'::jsonb,1,1)`, gatewayID, "routing-state-gw-"+gatewayID[:8])
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
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
		RoutingPolicyVersion: "rp-state", CapacityEvidenceVersion: "cap-state",
		PacingPolicyVersion: "pace-state", FallbackMode: "NONE", ApprovedAt: now,
		ApprovedBy: actorID, IdempotencyKey: "routing-state-" + campaignID,
		RequestHash: "routing-state-hash-" + campaignID,
		Routes:      []PoolRoute{{SenderPoolID: poolID, GatewayPoolID: gatewayID, Provider: "OPENWA", Engine: "BAILEYS", AllocationWeight: 100, MaximumRecipients: 5, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500}},
	}
	reservations := []CapacityReservation{{
		ID: reservationID, CampaignID: campaignID, RoutingPlanID: planID, SenderPoolID: poolID,
		ReservationStart: start, ReservationEnd: end,
		ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500,
		Status: "HELD", FencingVersion: 1, CreatedAt: now, UpdatedAt: now,
	}}

	_, err = (&PostgreSQLRoutingPlanStore{DB: db}).Create(ctx, plan, reservations)
	if !errors.Is(err, ErrRoutingPlanConflict) {
		t.Fatalf("routing plan store accepted a DISPATCHING campaign under its locked row: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_routing_plans WHERE id=$1::uuid`, planID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unsafe routing plan persisted despite campaign state conflict: count=%d", count)
	}
}

func TestPostgreSQLRoutingCreateRejectsNewPlanWhilePriorReservationsAreLive(t *testing.T) {
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
	actorID, orgID, purposeID := newDatabaseUUID(t, db, ctx), newDatabaseUUID(t, db, ctx), newDatabaseUUID(t, db, ctx)
	campaignID, poolID, gatewayID := newDatabaseUUID(t, db, ctx), newDatabaseUUID(t, db, ctx), newDatabaseUUID(t, db, ctx)
	plan1ID, plan2ID := newDatabaseUUID(t, db, ctx), newDatabaseUUID(t, db, ctx)
	reservation1ID, reservation2ID := newDatabaseUUID(t, db, ctx), newDatabaseUUID(t, db, ctx)
	now := time.Date(2026, 8, 16, 22, 0, 0, 0, time.UTC)
	start, end := now.Add(time.Hour), now.Add(3*time.Hour)
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Routing replacement actor','ACTIVE',false)`, actorID, "routing-replace-"+actorID[:8]+"@example.test")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Routing replacement "+orgID[:8])
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Routing replacement','WHATSAPP','v1')`, purposeID, orgID, "RRP_"+purposeID[:8])
	must(`INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,requested_start_at,completion_deadline_at) VALUES($1::uuid,$2::uuid,'Routing replacement',$3::uuid,'DRAFT',5,$4,$5)`, campaignID, orgID, purposeID, start, end)
	must(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',100,10000)`, poolID, "routing-replace-"+poolID[:8], orgID)
	must(`INSERT INTO gateway_pools(id,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version) VALUES($1::uuid,$2,'OPENWA','BAILEYS','0.13.0','ACTIVE','["SEND_TEXT"]'::jsonb,1,1)`, gatewayID, "routing-replace-gw-"+gatewayID[:8])
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_pool_capacity_reservations WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_routing_plan_pools WHERE routing_plan_id IN ($1::uuid,$2::uuid)`, plan1ID, plan2ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_routing_plans WHERE id IN ($1::uuid,$2::uuid)`, plan1ID, plan2ID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM gateway_pools WHERE id=$1::uuid`, gatewayID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pools WHERE id=$1::uuid`, poolID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id=$1::uuid`, actorID)
	}()
	makePlan := func(planID, reservationID, suffix string) (RoutingPlan, []CapacityReservation) {
		plan := RoutingPlan{ID: planID, CampaignID: campaignID, DistributionMode: DistributionWeighted,
			RoutingPolicyVersion: "rp-" + suffix, CapacityEvidenceVersion: "cap-" + suffix, PacingPolicyVersion: "pace-" + suffix,
			FallbackMode: "NONE", ApprovedAt: now, ApprovedBy: actorID, IdempotencyKey: "routing-replace-" + suffix + campaignID,
			RequestHash: "routing-replace-hash-" + suffix + campaignID,
			Routes:      []PoolRoute{{SenderPoolID: poolID, GatewayPoolID: gatewayID, Provider: "OPENWA", Engine: "BAILEYS", AllocationWeight: 100, MaximumRecipients: 5, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500}}}
		reservations := []CapacityReservation{{ID: reservationID, CampaignID: campaignID, RoutingPlanID: planID, SenderPoolID: poolID,
			ReservationStart: start, ReservationEnd: end, ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 100, ReservedDailyUnits: 500,
			Status: "HELD", FencingVersion: 1, CreatedAt: now, UpdatedAt: now}}
		return plan, reservations
	}
	store := &PostgreSQLRoutingPlanStore{DB: db}
	plan1, reservations1 := makePlan(plan1ID, reservation1ID, "v1-")
	if _, err := store.Create(ctx, plan1, reservations1); err != nil {
		t.Fatalf("first routing plan failed: %v", err)
	}
	plan2, reservations2 := makePlan(plan2ID, reservation2ID, "v2-")
	if _, err := store.Create(ctx, plan2, reservations2); !errors.Is(err, ErrRoutingPlanConflict) {
		t.Fatalf("new routing plan was accepted while prior campaign reservations were still live: %v", err)
	}
	var live, plans int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_pool_capacity_reservations WHERE campaign_id=$1::uuid AND status IN ('HELD','ACTIVE')`, campaignID).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_routing_plans WHERE campaign_id=$1::uuid`, campaignID).Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if live != 1 || plans != 1 {
		t.Fatalf("replacement attempt changed campaign authority: live_reservations=%d plans=%d", live, plans)
	}
}
