package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"campaign-platform/internal/delivery"
	_ "campaign-platform/internal/persistence/database"
	pgrepo "campaign-platform/internal/persistence/postgres"
	"campaign-platform/internal/sender"
)

func TestPostgreSQLPacingBlocksSecondCampaignOnDefaultSingleCampaignSender(t *testing.T) {
	dsn := os.Getenv("POSTGRES_SENDER_PACING_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_SENDER_PACING_DATABASE_URL is not set")
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
	var orgID, purposeID, senderID, campaignA, campaignB, makerID, checkerID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(
		&orgID, &purposeID, &senderID, &campaignA, &campaignB, &makerID, &checkerID,
	); err != nil {
		t.Fatal(err)
	}

	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_pacing_runtime WHERE sender_session_id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_daily_submission_usage WHERE sender_session_id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_hourly_submission_usage WHERE sender_session_id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_session_campaign_assignments WHERE sender_session_id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM sender_sessions WHERE id=$1::uuid`, senderID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id IN ($1::uuid,$2::uuid)`, campaignA, campaignB)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM internal_users WHERE id IN ($1::uuid,$2::uuid)`, makerID, checkerID)
	}()

	for _, actor := range []struct{ id, email string }{{makerID, "pacing-maker-" + makerID + "@internal.invalid"}, {checkerID, "pacing-checker-" + checkerID + "@internal.invalid"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO internal_users(id,email,display_name,status,mfa_required) VALUES($1::uuid,$2,'Pacing Evidence Actor','DISABLED',false)`, actor.id, actor.email); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "pacing-evidence-"+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,'PACING_EVIDENCE','Pacing evidence','WHATSAPP','v1')`, purposeID, orgID); err != nil {
		t.Fatal(err)
	}
	for _, campaignID := range []string{campaignA, campaignB} {
		if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,$3,$4::uuid,'DISPATCHING',1)`, campaignID, orgID, "Pacing evidence "+campaignID, purposeID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sender_sessions(id,encrypted_msisdn,masked_msisdn,engine_type,status,safe_messages_per_minute,safe_daily_capacity,last_heartbeat_at) VALUES($1::uuid,decode('00','hex'),'***9018','WHATSAPP_WEB_JS','READY',1000,10000,now())`, senderID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	policies := &sender.PacingAdministration{Store: &pgrepo.PacingPolicyRepository{DB: db}, Clock: func() time.Time { return now }}
	policy, err := policies.CreateDraft(ctx, sender.PacingPolicy{
		Scope: sender.PacingSession, ScopeID: senderID,
		MinimumDelayMS: 0, MaximumDelayMS: 0, JitterMode: sender.JitterNone,
		MaxInFlight: 1, MessagesPerMinute: 1000, HourlyAllowance: 1000, DailyAllowance: 10000,
		MaxActiveCampaigns: 0, BurstSize: 1, Overrides: []sender.MessageTypeOverride{},
	}, makerID, "default one campaign evidence")
	if err != nil {
		t.Fatal(err)
	}
	if policy.MaxActiveCampaigns != sender.DefaultMaxActiveCampaigns {
		t.Fatalf("maxActiveCampaigns=%d want=%d", policy.MaxActiveCampaigns, sender.DefaultMaxActiveCampaigns)
	}
	policy, err = policies.Submit(ctx, policy.ID, policy.Version, makerID, "submit one campaign evidence")
	if err != nil {
		t.Fatal(err)
	}
	policy, err = policies.Decide(ctx, policy.ID, policy.Version, true, checkerID, "approve one campaign evidence")
	if err != nil {
		t.Fatal(err)
	}
	if policy.Status != sender.PacingActive || policy.MaxActiveCampaigns != 1 {
		t.Fatalf("unexpected active policy: %+v", policy)
	}

	controller := &PostgreSQLPacingController{DB: db, Policies: policies}
	material := Material{SessionID: senderID, MessageType: "text"}
	first := delivery.Recipient{CampaignID: campaignA}
	if err := controller.Wait(ctx, first, material, now); err != nil {
		t.Fatalf("first campaign should acquire sender: %v", err)
	}
	var appliedPolicyID string
	var appliedPolicyVersion int64
	if err := db.QueryRowContext(ctx, `SELECT policy_id::text,policy_version FROM sender_pacing_runtime WHERE sender_session_id=$1::uuid`, senderID).Scan(&appliedPolicyID, &appliedPolicyVersion); err != nil {
		t.Fatal(err)
	}
	if appliedPolicyID != policy.ID || appliedPolicyVersion != policy.Version {
		t.Fatalf("worker pacing runtime policy=%s v%d want=%s v%d", appliedPolicyID, appliedPolicyVersion, policy.ID, policy.Version)
	}
	second := delivery.Recipient{CampaignID: campaignB}
	if err := controller.Wait(ctx, second, material, now); !errors.Is(err, ErrActiveCampaignLimit) {
		t.Fatalf("second campaign err=%v want=%v", err, ErrActiveCampaignLimit)
	}

	var activeCampaigns int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sender_session_campaign_assignments WHERE sender_session_id=$1::uuid AND status='ACTIVE'`, senderID).Scan(&activeCampaigns); err != nil {
		t.Fatal(err)
	}
	if activeCampaigns != 1 {
		t.Fatalf("active assignments=%d want=1", activeCampaigns)
	}
}
