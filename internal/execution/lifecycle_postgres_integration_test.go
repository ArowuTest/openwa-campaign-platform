package execution

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"campaign-platform/internal/campaign"
	_ "campaign-platform/internal/persistence/database"
	postgresrepo "campaign-platform/internal/persistence/postgres"
)

type executionLifecycleFixture struct {
	actorID      string
	organisation string
	purposeID    string
	campaignID   string
	poolID       string
	planID       string
	reservation  string
}

func newExecutionLifecycleFixture(
	t *testing.T, db *sql.DB, status string,
) executionLifecycleFixture {
	t.Helper()
	ctx := context.Background()
	var value executionLifecycleFixture
	err := db.QueryRowContext(ctx, `
SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,
       gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,
       gen_random_uuid()::text`).Scan(
		&value.actorID, &value.organisation, &value.purposeID,
		&value.campaignID, &value.poolID, &value.planID, &value.reservation,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `
INSERT INTO internal_users(id,email,display_name,status,mfa_required)
VALUES($1::uuid,$2,'Atomic lifecycle actor','DISABLED',false)`,
		value.actorID, "atomic-"+value.actorID+"@internal.invalid",
	); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `
INSERT INTO organisations(id,legal_name,status)
VALUES($1::uuid,$2,'ACTIVE')`,
		value.organisation, "Atomic lifecycle "+value.organisation,
	); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `
INSERT INTO consent_purposes(
	id,organisation_id,code,name,channel,wording_version
) VALUES($1::uuid,$2::uuid,$3,'Atomic lifecycle','WHATSAPP','v1')`,
		value.purposeID, value.organisation, "ATOMIC_"+value.purposeID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `
INSERT INTO campaigns(
	id,organisation_id,name,purpose_id,status,maximum_unique_recipients,version
) VALUES($1::uuid,$2::uuid,'Atomic lifecycle',$3::uuid,$4,10,1)`,
		value.campaignID, value.organisation, value.purposeID, status,
	); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `
INSERT INTO sender_pools(
	id,name,organisation_id,status,max_messages_per_minute,daily_capacity
) VALUES($1::uuid,$2,$3::uuid,'ACTIVE',20,1000)`,
		value.poolID, "Atomic lifecycle "+value.poolID, value.organisation,
	); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `
INSERT INTO campaign_routing_plans(
	id,campaign_id,plan_version,routing_policy_version,
	capacity_evidence_version,pacing_policy_version,fallback_mode,
	approved_at,approved_by,idempotency_key,request_hash
) VALUES(
	$1::uuid,$2::uuid,1,'routing-v1','capacity-v1','pacing-v1','NONE',
	now(),$3::uuid,$4,$5
)`, value.planID, value.campaignID, value.actorID,
		"atomic-"+value.planID, strings.Repeat("a", 32),
	); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `
INSERT INTO campaign_pool_capacity_reservations(
	id,campaign_id,routing_plan_id,sender_pool_id,
	reservation_start,reservation_end,reserved_messages_per_minute,
	reserved_hourly_units,reserved_daily_units,status,fencing_version
) VALUES(
	$1::uuid,$2::uuid,$3::uuid,$4::uuid,now(),now()+interval '1 hour',
	10,100,100,'ACTIVE',2
)`, value.reservation, value.campaignID, value.planID, value.poolID); err != nil {
		t.Fatal(err)
	}
	return value
}

func (v executionLifecycleFixture) cleanup(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	_, _ = db.ExecContext(ctx,
		`DELETE FROM campaign_execution_events WHERE campaign_id=$1::uuid`, v.campaignID)
	_, _ = db.ExecContext(ctx,
		`DELETE FROM campaign_pool_capacity_reservations WHERE routing_plan_id=$1::uuid`, v.planID)
	_, _ = db.ExecContext(ctx,
		`DELETE FROM campaign_routing_plans WHERE id=$1::uuid`, v.planID)
	_, _ = db.ExecContext(ctx,
		`DELETE FROM sender_pools WHERE id=$1::uuid`, v.poolID)
	_, _ = db.ExecContext(ctx,
		`DELETE FROM campaigns WHERE id=$1::uuid`, v.campaignID)
	_, _ = db.ExecContext(ctx,
		`DELETE FROM consent_purposes WHERE id=$1::uuid`, v.purposeID)
	_, _ = db.ExecContext(ctx,
		`DELETE FROM organisations WHERE id=$1::uuid`, v.organisation)
	_, _ = db.ExecContext(ctx,
		`DELETE FROM internal_users WHERE id=$1::uuid`, v.actorID)
}

func openExecutionLifecycleDatabase(t *testing.T) *sql.DB {
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
	return db
}

func TestPostgreSQLExecutionLifecycleRollsBackWhenEventInsertFails(t *testing.T) {
	db := openExecutionLifecycleDatabase(t)
	fixture := newExecutionLifecycleFixture(t, db, "PAUSED")
	t.Cleanup(func() { fixture.cleanup(t, db) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	suffix := strings.ReplaceAll(fixture.campaignID, "-", "")
	functionName := "task6_fail_event_" + suffix
	triggerName := "task6_fail_event_trigger_" + suffix
	functionDDL := fmt.Sprintf(`
CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.campaign_id = '%s'::uuid AND NEW.campaign_version IS NOT NULL THEN
		RAISE EXCEPTION 'injected lifecycle event failure';
	END IF;
	RETURN NEW;
END
$$`, functionName, fixture.campaignID)
	if _, err := db.ExecContext(ctx, functionDDL); err != nil {
		t.Fatal(err)
	}
	triggerDDL := fmt.Sprintf(`
CREATE TRIGGER %s
BEFORE INSERT ON campaign_execution_events
FOR EACH ROW EXECUTE FUNCTION %s()`, triggerName, functionName)
	if _, err := db.ExecContext(ctx, triggerDDL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON campaign_execution_events", triggerName))
		_, _ = db.ExecContext(context.Background(),
			fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	})

	repository := &postgresrepo.CampaignRepository{DB: db}
	current, err := repository.Get(ctx, fixture.campaignID)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := current.Transition(campaign.TransitionInput{
		Action: campaign.ActionCancel, ActorID: fixture.actorID,
		Reason: "operator cancellation", ExpectedVersion: current.Version,
	}, time.Date(2026, 8, 10, 19, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	committer := &PostgreSQLLifecycleCommitter{DB: db}
	_, err = committer.Commit(ctx, LifecycleCommit{
		Campaign: candidate, ExpectedVersion: current.Version,
		Action: campaign.ActionCancel, EventType: "CAMPAIGN_CANCELLED",
		ActorID: fixture.actorID, Reason: "operator cancellation",
		RoutingPlanID:        fixture.planID,
		ReservationOperation: ReservationRelease,
		OccurredAt:           candidate.UpdatedAt,
	})
	if err == nil {
		t.Fatal("expected injected event failure")
	}
	var status string
	var version int64
	if err = db.QueryRowContext(ctx,
		`SELECT status,version FROM campaigns WHERE id=$1::uuid`,
		fixture.campaignID,
	).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "PAUSED" || version != 1 {
		t.Fatalf("campaign partially committed status=%s version=%d", status, version)
	}
	var reservationStatus string
	var fence int64
	if err = db.QueryRowContext(ctx, `
SELECT status,fencing_version
FROM campaign_pool_capacity_reservations
WHERE id=$1::uuid`, fixture.reservation).Scan(&reservationStatus, &fence); err != nil {
		t.Fatal(err)
	}
	if reservationStatus != "ACTIVE" || fence != 2 {
		t.Fatalf("reservation partially committed status=%s fence=%d",
			reservationStatus, fence)
	}
	var eventCount int
	if err = db.QueryRowContext(ctx, `
SELECT count(*) FROM campaign_execution_events
WHERE campaign_id=$1::uuid AND campaign_version IS NOT NULL`,
		fixture.campaignID,
	).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("rolled-back lifecycle events=%d", eventCount)
	}
}

func TestPostgreSQLExecutionLifecycleCommitsAllActionsAtomically(t *testing.T) {
	db := openExecutionLifecycleDatabase(t)
	cases := []struct {
		name              string
		from              string
		action            campaign.Action
		eventType         string
		operation         ReservationOperation
		initialResStatus  string
		initialFence      int64
		expectedStatus    string
		expectedResStatus string
		expectedFence     int64
	}{
		{"start", "SCHEDULED", campaign.ActionStartDispatch, "DISPATCH_STARTED",
			ReservationActivate, "HELD", 1, "DISPATCHING", "ACTIVE", 2},
		{"start-preactivated", "SCHEDULED", campaign.ActionStartDispatch, "DISPATCH_STARTED",
			ReservationActivate, "ACTIVE", 2, "DISPATCHING", "ACTIVE", 2},
		{"pause", "DISPATCHING", campaign.ActionPause, "CAMPAIGN_PAUSED",
			ReservationNone, "ACTIVE", 2, "PAUSED", "ACTIVE", 2},
		{"resume", "PAUSED", campaign.ActionResume, "CAMPAIGN_RESUMED",
			ReservationNone, "ACTIVE", 2, "DISPATCHING", "ACTIVE", 2},
		{"cancel", "PAUSED", campaign.ActionCancel, "CAMPAIGN_CANCELLED",
			ReservationRelease, "ACTIVE", 2, "CANCELLED", "RELEASED", 3},
		{"cancel-prereleased", "PAUSED", campaign.ActionCancel, "CAMPAIGN_CANCELLED",
			ReservationRelease, "RELEASED", 3, "CANCELLED", "RELEASED", 3},
		{"complete", "DISPATCHING", campaign.ActionComplete, "COMPLETED",
			ReservationRelease, "ACTIVE", 2, "COMPLETED", "RELEASED", 3},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newExecutionLifecycleFixture(t, db, test.from)
			t.Cleanup(func() { fixture.cleanup(t, db) })
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := db.ExecContext(ctx, `
UPDATE campaign_pool_capacity_reservations
SET status=$2, fencing_version=$3,
    released_by=CASE WHEN $2='RELEASED' THEN $4::text ELSE NULL::text END,
    released_at=CASE WHEN $2='RELEASED' THEN $5::timestamptz ELSE NULL::timestamptz END
WHERE id=$1::uuid`, fixture.reservation,
				test.initialResStatus, test.initialFence, fixture.actorID,
				time.Date(2026, 8, 10, 19, 30, 0, 0, time.UTC),
			); err != nil {
				t.Fatal(err)
			}
			repository := &postgresrepo.CampaignRepository{DB: db}
			current, err := repository.Get(ctx, fixture.campaignID)
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := current.Transition(campaign.TransitionInput{
				Action: test.action, ActorID: fixture.actorID,
				Reason: "atomic lifecycle test", ExpectedVersion: current.Version,
			}, time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			planID := ""
			if test.operation != ReservationNone {
				planID = fixture.planID
			}
			committer := &PostgreSQLLifecycleCommitter{DB: db}
			result, err := committer.Commit(ctx, LifecycleCommit{
				Campaign: candidate, ExpectedVersion: current.Version,
				Action: test.action, EventType: test.eventType,
				ActorID: fixture.actorID, Reason: "atomic lifecycle test",
				RoutingPlanID: planID, ReservationOperation: test.operation,
				OccurredAt: candidate.UpdatedAt,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != campaign.Status(test.expectedStatus) || result.Version != 2 {
				t.Fatalf("returned campaign status=%s version=%d",
					result.Status, result.Version)
			}
			var status, reservationStatus string
			var version, fence int64
			if err = db.QueryRowContext(ctx, `
SELECT c.status,c.version,r.status,r.fencing_version
FROM campaigns c
JOIN campaign_pool_capacity_reservations r ON r.campaign_id=c.id
WHERE c.id=$1::uuid`, fixture.campaignID).Scan(
				&status, &version, &reservationStatus, &fence,
			); err != nil {
				t.Fatal(err)
			}
			if status != test.expectedStatus || version != 2 {
				t.Fatalf("persisted campaign status=%s version=%d", status, version)
			}
			if reservationStatus != test.expectedResStatus || fence != test.expectedFence {
				t.Fatalf("reservation status=%s fence=%d", reservationStatus, fence)
			}
			var eventVersion int64
			var eventType string
			if err = db.QueryRowContext(ctx, `
SELECT campaign_version,event_type
FROM campaign_execution_events
WHERE campaign_id=$1::uuid AND campaign_version=2`,
				fixture.campaignID,
			).Scan(&eventVersion, &eventType); err != nil {
				t.Fatal(err)
			}
			if eventVersion != 2 || eventType != test.eventType {
				t.Fatalf("event version=%d type=%s", eventVersion, eventType)
			}
		})
	}
}

func TestPostgreSQLExecutionLifecycleRollsBackWhenReservationMutationFails(t *testing.T) {
	db := openExecutionLifecycleDatabase(t)
	fixture := newExecutionLifecycleFixture(t, db, "PAUSED")
	t.Cleanup(func() { fixture.cleanup(t, db) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `
UPDATE campaign_pool_capacity_reservations
SET status='EXPIRED' WHERE id=$1::uuid`, fixture.reservation); err != nil {
		t.Fatal(err)
	}
	var mixedReservation string
	if err := db.QueryRowContext(ctx,
		`SELECT gen_random_uuid()::text`,
	).Scan(&mixedReservation); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO campaign_pool_capacity_reservations(
	id,campaign_id,routing_plan_id,sender_pool_id,
	reservation_start,reservation_end,reserved_messages_per_minute,
	reserved_hourly_units,reserved_daily_units,status,fencing_version
) VALUES(
	$1::uuid,$2::uuid,$3::uuid,$4::uuid,now(),now()+interval '1 hour',
	10,100,100,'ACTIVE',2
)`, mixedReservation, fixture.campaignID, fixture.planID, fixture.poolID); err != nil {
		t.Fatal(err)
	}
	repository := &postgresrepo.CampaignRepository{DB: db}
	current, err := repository.Get(ctx, fixture.campaignID)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := current.Transition(campaign.TransitionInput{
		Action: campaign.ActionCancel, ActorID: fixture.actorID,
		Reason: "expired reservation test", ExpectedVersion: current.Version,
	}, time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	committer := &PostgreSQLLifecycleCommitter{DB: db}
	_, err = committer.Commit(ctx, LifecycleCommit{
		Campaign: candidate, ExpectedVersion: current.Version,
		Action: campaign.ActionCancel, EventType: "CAMPAIGN_CANCELLED",
		ActorID: fixture.actorID, Reason: "expired reservation test",
		RoutingPlanID: fixture.planID, ReservationOperation: ReservationRelease,
		OccurredAt: candidate.UpdatedAt,
	})
	if err == nil {
		t.Fatal("expected expired reservation mutation failure")
	}
	var status string
	var version, expired, active, maxFence, events int64
	if err = db.QueryRowContext(ctx, `
SELECT c.status,c.version,
       count(*) FILTER (WHERE r.status='EXPIRED'),
       count(*) FILTER (WHERE r.status='ACTIVE'),
       max(r.fencing_version),
       (SELECT count(*) FROM campaign_execution_events e
        WHERE e.campaign_id=c.id AND e.campaign_version IS NOT NULL)
FROM campaigns c
JOIN campaign_pool_capacity_reservations r ON r.campaign_id=c.id
WHERE c.id=$1::uuid
GROUP BY c.id,c.status,c.version`, fixture.campaignID).Scan(
		&status, &version, &expired, &active, &maxFence, &events,
	); err != nil {
		t.Fatal(err)
	}
	if status != "PAUSED" || version != 1 {
		t.Fatalf("campaign partially committed status=%s version=%d", status, version)
	}
	if expired != 1 || active != 1 || maxFence != 2 {
		t.Fatalf("mixed reservations changed expired=%d active=%d maxFence=%d",
			expired, active, maxFence)
	}
	if events != 0 {
		t.Fatalf("unexpected version-linked lifecycle events=%d", events)
	}
}

func TestPostgreSQLExecutionLifecycleConcurrentStartCommitsOnce(t *testing.T) {
	db := openExecutionLifecycleDatabase(t)
	fixture := newExecutionLifecycleFixture(t, db, "SCHEDULED")
	t.Cleanup(func() { fixture.cleanup(t, db) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `
UPDATE campaign_pool_capacity_reservations
SET status='HELD',fencing_version=1 WHERE id=$1::uuid`,
		fixture.reservation,
	); err != nil {
		t.Fatal(err)
	}
	repository := &postgresrepo.CampaignRepository{DB: db}
	current, err := repository.Get(ctx, fixture.campaignID)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := current.Transition(campaign.TransitionInput{
		Action: campaign.ActionStartDispatch, ActorID: fixture.actorID,
		Reason: "concurrent start test", ExpectedVersion: current.Version,
	}, time.Date(2026, 8, 10, 22, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	value := LifecycleCommit{
		Campaign: candidate, ExpectedVersion: current.Version,
		Action: campaign.ActionStartDispatch, EventType: "DISPATCH_STARTED",
		ActorID: fixture.actorID, Reason: "concurrent start test",
		RoutingPlanID: fixture.planID, ReservationOperation: ReservationActivate,
		OccurredAt: candidate.UpdatedAt,
	}
	committer := &PostgreSQLLifecycleCommitter{DB: db}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, commitErr := committer.Commit(ctx, value)
			results <- commitErr
		}()
	}
	var successes, conflicts int
	for range 2 {
		commitErr := <-results
		switch {
		case commitErr == nil:
			successes++
		case errors.Is(commitErr, campaign.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent commit error: %v", commitErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	var status, reservationStatus string
	var version, fence, events int64
	if err = db.QueryRowContext(ctx, `
SELECT c.status,c.version,r.status,r.fencing_version,
       (SELECT count(*) FROM campaign_execution_events e
        WHERE e.campaign_id=c.id AND e.campaign_version=2)
FROM campaigns c
JOIN campaign_pool_capacity_reservations r ON r.campaign_id=c.id
WHERE c.id=$1::uuid`, fixture.campaignID).Scan(
		&status, &version, &reservationStatus, &fence, &events,
	); err != nil {
		t.Fatal(err)
	}
	if status != "DISPATCHING" || version != 2 {
		t.Fatalf("campaign status=%s version=%d", status, version)
	}
	if reservationStatus != "ACTIVE" || fence != 2 {
		t.Fatalf("reservation status=%s fence=%d", reservationStatus, fence)
	}
	if events != 1 {
		t.Fatalf("version-linked lifecycle events=%d", events)
	}
}
