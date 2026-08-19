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

type r18RoutingFixture struct {
	db        *sql.DB
	ctx       context.Context
	actorID   string
	orgID     string
	purposeID string
	poolID    string
	gatewayID string
}

func newR18RoutingFixture(t *testing.T) *r18RoutingFixture {
	t.Helper()
	dsn := os.Getenv("POSTGRES_EXECUTION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_EXECUTION_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	f := &r18RoutingFixture{db: db, ctx: ctx}
	f.actorID = newDatabaseUUID(t, db, ctx)
	f.orgID = newDatabaseUUID(t, db, ctx)
	f.purposeID = newDatabaseUUID(t, db, ctx)
	f.poolID = newDatabaseUUID(t, db, ctx)
	f.gatewayID = newDatabaseUUID(t, db, ctx)
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'R18 routing actor','ACTIVE',false)`, f.actorID, "r18-routing-"+f.actorID[:8]+"@example.test")
	must(`INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, f.orgID, "R18 routing "+f.orgID[:8])
	must(`INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'R18 routing','WHATSAPP','v1')`, f.purposeID, f.orgID, "R18_"+f.purposeID[:8])
	must(`INSERT INTO sender_pools(id,name,organisation_id,status,max_messages_per_minute,daily_capacity) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',10,1000)`, f.poolID, "r18-pool-"+f.poolID[:8], f.orgID)
	must(`INSERT INTO gateway_pools(id,name,provider,engine,adapter_version,status,capabilities,minimum_healthy_nodes,version) VALUES($1::uuid,$2,'OPENWA','BAILEYS','0.13.0','ACTIVE','["SEND_TEXT"]'::jsonb,1,1)`, f.gatewayID, "r18-gw-"+f.gatewayID[:8])
	return f
}

func (f *r18RoutingFixture) campaign(t *testing.T, status string, start, end time.Time) (string, string, string) {
	t.Helper()
	campaignID := newDatabaseUUID(t, f.db, f.ctx)
	planID := newDatabaseUUID(t, f.db, f.ctx)
	reservationID := newDatabaseUUID(t, f.db, f.ctx)
	_, err := f.db.ExecContext(f.ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients,requested_start_at,completion_deadline_at) VALUES($1::uuid,$2::uuid,'R18 routing',$3::uuid,$4,700,$5,$6)`, campaignID, f.orgID, f.purposeID, status, start, end)
	if err != nil {
		t.Fatal(err)
	}
	return campaignID, planID, reservationID
}
func (f *r18RoutingFixture) plan(campaignID, planID, reservationID string, start, end, approvedAt time.Time, daily int64) (RoutingPlan, []CapacityReservation) {
	plan := RoutingPlan{
		ID: planID, CampaignID: campaignID, DistributionMode: DistributionWeighted,
		RoutingPolicyVersion: "r18-route-" + planID, CapacityEvidenceVersion: "r18-capacity-" + planID,
		PacingPolicyVersion: "r18-pacing-" + planID, FallbackMode: "NONE",
		ApprovedAt: approvedAt, ApprovedBy: f.actorID,
		IdempotencyKey: "r18-routing-" + planID, RequestHash: "r18-hash-" + planID,
		Routes: []PoolRoute{{
			SenderPoolID: f.poolID, GatewayPoolID: f.gatewayID, Provider: "OPENWA", Engine: "BAILEYS",
			AllocationWeight: 100, MaximumRecipients: 700,
			ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 600, ReservedDailyUnits: daily,
		}},
	}
	reservations := []CapacityReservation{{
		ID: reservationID, CampaignID: campaignID, RoutingPlanID: planID, SenderPoolID: f.poolID,
		ReservationStart: start, ReservationEnd: end,
		ReservedMessagesPerMinute: 10, ReservedHourlyUnits: 600, ReservedDailyUnits: daily,
		Status: "HELD", FencingVersion: 1, CreatedAt: approvedAt, UpdatedAt: approvedAt,
	}}
	return plan, reservations
}

func TestR18PostgreSQLRoutingRejectsSequentialSameDayDailyOverbooking(t *testing.T) {
	f := newR18RoutingFixture(t)
	now := time.Date(2026, 8, 17, 7, 0, 0, 0, time.UTC)
	startA, endA := time.Date(2026, 8, 18, 8, 0, 0, 0, time.UTC), time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	startB, endB := endA, time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	campaignA, planA, reservationA := f.campaign(t, "DRAFT", startA, endA)
	campaignB, planB, reservationB := f.campaign(t, "DRAFT", startB, endB)
	pA, rA := f.plan(campaignA, planA, reservationA, startA, endA, now, 700)
	pB, rB := f.plan(campaignB, planB, reservationB, startB, endB, now.Add(time.Minute), 700)
	store := &PostgreSQLRoutingPlanStore{DB: f.db}
	if _, err := store.Create(f.ctx, pA, rA); err != nil {
		t.Fatalf("first daily reservation: %v", err)
	}
	if _, err := store.Create(f.ctx, pB, rB); !errors.Is(err, ErrCapacityOverbooked) {
		t.Fatalf("sequential same-day reservations exceeded daily capacity without rejection: %v", err)
	}
}

func TestR18PostgreSQLRoutingRejectsTerminalCampaignReservationAuthority(t *testing.T) {
	for _, status := range []string{"COMPLETED", "COMPLETED_WITH_EXCEPTIONS", "CANCELLED"} {
		t.Run(status, func(t *testing.T) {
			f := newR18RoutingFixture(t)
			now := time.Date(2026, 8, 17, 7, 0, 0, 0, time.UTC)
			start, end := now.Add(time.Hour), now.Add(3*time.Hour)
			campaignID, planID, reservationID := f.campaign(t, status, start, end)
			plan, reservations := f.plan(campaignID, planID, reservationID, start, end, now, 700)
			_, err := (&PostgreSQLRoutingPlanStore{DB: f.db}).Create(f.ctx, plan, reservations)
			if !errors.Is(err, ErrRoutingPlanConflict) {
				t.Fatalf("terminal campaign %s minted new HELD capacity authority: %v", status, err)
			}
		})
	}
}
