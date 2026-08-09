package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "campaign-platform/internal/persistence/database"
)

func TestPostgreSQLAdmissionPersistsDecisionAndAssumptions(t *testing.T) {
	dsn := os.Getenv("POSTGRES_EXECUTION_ADMISSION_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_EXECUTION_ADMISSION_DATABASE_URL is not set")
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
	var orgID, purposeID, campaignID, referenceID string
	if err := db.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&orgID, &purposeID, &campaignID, &referenceID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaign_capacity_assessments WHERE campaign_id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM campaigns WHERE id=$1::uuid`, campaignID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM consent_purposes WHERE id=$1::uuid`, purposeID)
		_, _ = db.ExecContext(cleanup, `DELETE FROM organisations WHERE id=$1::uuid`, orgID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO organisations(id,legal_name,status) VALUES($1::uuid,$2,'ACTIVE')`, orgID, "Admission Evidence "+orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO consent_purposes(id,organisation_id,code,name,channel,wording_version) VALUES($1::uuid,$2::uuid,$3,'Admission Evidence','WHATSAPP','v1')`, purposeID, orgID, "ADM_"+purposeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO campaigns(id,organisation_id,name,purpose_id,status,maximum_unique_recipients) VALUES($1::uuid,$2::uuid,'Admission Evidence',$3::uuid,'SCHEDULED',1000)`, campaignID, orgID, purposeID); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 8, 12, 30, 0, 0, time.UTC)
	forecast := now.Add(75 * time.Minute)
	evidence := CapacityEvidence{
		CampaignID: campaignID, PoolID: referenceID, EvidenceVersion: "capacity-v7",
		RemainingRecipients: 900, AvailableMessagesPerMinute: 20, AvailableDailyCapacity: 800,
		SafetyMarginPercent: 10, RequiredMessagesPerMinute: 15, EffectiveMessagesPerMinute: 18,
		ForecastCompletionAt: &forecast, DeadlineAt: now.Add(time.Hour), Decision: DecisionReject,
		Reasons: []string{"INSUFFICIENT_DAILY_CAPACITY", "DEADLINE_CAPACITY_SHORTFALL"}, EvaluatedAt: now,
	}
	if err := (&PostgreSQLStore{DB: db}).RecordAdmission(ctx, evidence); err != nil {
		t.Fatal(err)
	}
	var got CapacityEvidence
	var decision string
	var reasonsRaw []byte
	if err := db.QueryRowContext(ctx, `SELECT capacity_reference_id::text,evidence_version,remaining_recipients,available_messages_per_minute,available_daily_capacity,safety_margin_percent,required_messages_per_minute,effective_messages_per_minute,forecast_completion_at,deadline_at,decision,reasons,evaluated_at FROM campaign_capacity_assessments WHERE campaign_id=$1::uuid ORDER BY evaluated_at DESC LIMIT 1`, campaignID).Scan(
		&got.PoolID, &got.EvidenceVersion, &got.RemainingRecipients, &got.AvailableMessagesPerMinute, &got.AvailableDailyCapacity,
		&got.SafetyMarginPercent, &got.RequiredMessagesPerMinute, &got.EffectiveMessagesPerMinute, &got.ForecastCompletionAt,
		&got.DeadlineAt, &decision, &reasonsRaw, &got.EvaluatedAt,
	); err != nil {
		t.Fatal(err)
	}
	got.Decision = AdmissionDecision(decision)
	if err := json.Unmarshal(reasonsRaw, &got.Reasons); err != nil {
		t.Fatal(err)
	}
	if got.PoolID != evidence.PoolID || got.EvidenceVersion != evidence.EvidenceVersion ||
		got.RemainingRecipients != evidence.RemainingRecipients || got.AvailableMessagesPerMinute != evidence.AvailableMessagesPerMinute ||
		got.AvailableDailyCapacity != evidence.AvailableDailyCapacity || got.SafetyMarginPercent != evidence.SafetyMarginPercent ||
		got.RequiredMessagesPerMinute != evidence.RequiredMessagesPerMinute || got.EffectiveMessagesPerMinute != evidence.EffectiveMessagesPerMinute ||
		got.Decision != evidence.Decision || len(got.Reasons) != 2 {
		t.Fatalf("persisted admission evidence mismatch: got=%+v want=%+v", got, evidence)
	}
}
