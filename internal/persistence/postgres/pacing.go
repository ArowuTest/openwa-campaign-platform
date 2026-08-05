package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"campaign-platform/internal/sender"
)

type PacingPolicyRepository struct{ DB *sql.DB }

const pacingSelect = `SELECT id::text,scope,coalesce(scope_id,''),coalesce(provider,''),coalesce(engine,''),minimum_delay_ms,maximum_delay_ms,jitter_mode,max_in_flight,messages_per_minute,hourly_allowance,daily_allowance,max_active_campaigns,burst_size,cooldown_seconds,recovery_ramp_minutes,failure_threshold_bps,disconnect_threshold,auto_quarantine,message_type_overrides::text,status,effective_from,effective_to,version,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,created_at,updated_at FROM sender_pacing_policies`

type pacingScanner interface{ Scan(...any) error }

func scanPacing(row pacingScanner) (sender.PacingPolicy, error) {
	var p sender.PacingPolicy
	var scope, jitter, status, overrides string
	err := row.Scan(&p.ID, &scope, &p.ScopeID, &p.Provider, &p.Engine, &p.MinimumDelayMS, &p.MaximumDelayMS, &jitter, &p.MaxInFlight, &p.MessagesPerMinute, &p.HourlyAllowance, &p.DailyAllowance, &p.MaxActiveCampaigns, &p.BurstSize, &p.CooldownSeconds, &p.RecoveryRampMinutes, &p.FailureThresholdBPS, &p.DisconnectThreshold, &p.AutoQuarantine, &overrides, &status, &p.EffectiveFrom, &p.EffectiveTo, &p.Version, &p.CreatedBy, &p.SubmittedBy, &p.ApprovedBy, &p.Reason, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, err
	}
	p.Scope = sender.PacingScope(scope)
	p.JitterMode = sender.JitterMode(jitter)
	p.Status = sender.PacingStatus(status)
	if err := json.Unmarshal([]byte(overrides), &p.Overrides); err != nil {
		return p, err
	}
	return p, nil
}

func (r *PacingPolicyRepository) Create(ctx context.Context, p sender.PacingPolicy) (sender.PacingPolicy, error) {
	overrides, err := json.Marshal(p.Overrides)
	if err != nil {
		return sender.PacingPolicy{}, err
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return sender.PacingPolicy{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO sender_pacing_policies(id,scope,scope_id,provider,engine,minimum_delay_ms,maximum_delay_ms,jitter_mode,max_in_flight,messages_per_minute,hourly_allowance,daily_allowance,max_active_campaigns,burst_size,cooldown_seconds,recovery_ramp_minutes,failure_threshold_bps,disconnect_threshold,auto_quarantine,message_type_overrides,status,effective_from,effective_to,version,created_by,submitted_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20::jsonb,$21,$22,$23,$24,$25::uuid,NULLIF($26,'')::uuid,NULLIF($27,'')::uuid,$28,$29,$30)`, p.ID, p.Scope, p.ScopeID, p.Provider, p.Engine, p.MinimumDelayMS, p.MaximumDelayMS, p.JitterMode, p.MaxInFlight, p.MessagesPerMinute, p.HourlyAllowance, p.DailyAllowance, p.MaxActiveCampaigns, p.BurstSize, p.CooldownSeconds, p.RecoveryRampMinutes, p.FailureThresholdBPS, p.DisconnectThreshold, p.AutoQuarantine, string(overrides), p.Status, p.EffectiveFrom, p.EffectiveTo, p.Version, p.CreatedBy, p.SubmittedBy, p.ApprovedBy, p.Reason, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return sender.PacingPolicy{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sender_pacing_policy_events(id,policy_id,action,actor_id,reason,policy_version,occurred_at) VALUES(gen_random_uuid(),$1::uuid,'CREATED',$2::uuid,$3,$4,$5)`, p.ID, p.CreatedBy, p.Reason, p.Version, p.CreatedAt)
	if err != nil {
		return sender.PacingPolicy{}, err
	}
	if err = tx.Commit(); err != nil {
		return sender.PacingPolicy{}, err
	}
	return p, nil
}
func (r *PacingPolicyRepository) Get(ctx context.Context, id string) (sender.PacingPolicy, error) {
	p, err := scanPacing(r.DB.QueryRowContext(ctx, pacingSelect+` WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return sender.PacingPolicy{}, sender.ErrPacingNotFound
	}
	return p, err
}
func (r *PacingPolicyRepository) List(ctx context.Context) ([]sender.PacingPolicy, error) {
	rows, err := r.DB.QueryContext(ctx, pacingSelect+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sender.PacingPolicy{}
	for rows.Next() {
		p, e := scanPacing(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *PacingPolicyRepository) CompareAndSwap(ctx context.Context, p sender.PacingPolicy, expected int64) (sender.PacingPolicy, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return sender.PacingPolicy{}, err
	}
	defer tx.Rollback()
	if p.Status == sender.PacingActive {
		_, err = tx.ExecContext(ctx, `UPDATE sender_pacing_policies SET status='RETIRED',effective_to=$3,version=version+1,updated_at=$3 WHERE scope=$1 AND coalesce(scope_id,'')=coalesce(NULLIF($2,''),'') AND status='ACTIVE' AND id<>$4::uuid`, p.Scope, p.ScopeID, p.EffectiveFrom, p.ID)
		if err != nil {
			return sender.PacingPolicy{}, err
		}
	}
	overrides, e := json.Marshal(p.Overrides)
	if e != nil {
		return sender.PacingPolicy{}, e
	}
	res, err := tx.ExecContext(ctx, `UPDATE sender_pacing_policies SET provider=NULLIF($2,''),engine=NULLIF($3,''),minimum_delay_ms=$4,maximum_delay_ms=$5,jitter_mode=$6,max_in_flight=$7,messages_per_minute=$8,hourly_allowance=$9,daily_allowance=$10,max_active_campaigns=$11,burst_size=$12,cooldown_seconds=$13,recovery_ramp_minutes=$14,failure_threshold_bps=$15,disconnect_threshold=$16,auto_quarantine=$17,message_type_overrides=$18::jsonb,status=$19,effective_from=$20,effective_to=$21,version=$22,submitted_by=NULLIF($23,'')::uuid,approved_by=NULLIF($24,'')::uuid,reason=$25,updated_at=$26 WHERE id=$1::uuid AND version=$27`, p.ID, p.Provider, p.Engine, p.MinimumDelayMS, p.MaximumDelayMS, p.JitterMode, p.MaxInFlight, p.MessagesPerMinute, p.HourlyAllowance, p.DailyAllowance, p.MaxActiveCampaigns, p.BurstSize, p.CooldownSeconds, p.RecoveryRampMinutes, p.FailureThresholdBPS, p.DisconnectThreshold, p.AutoQuarantine, string(overrides), p.Status, p.EffectiveFrom, p.EffectiveTo, p.Version, p.SubmittedBy, p.ApprovedBy, p.Reason, p.UpdatedAt, expected)
	if err != nil {
		return sender.PacingPolicy{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return sender.PacingPolicy{}, sender.ErrPacingConflict
	}
	action := "SUBMITTED"
	actor := p.SubmittedBy
	if p.Status == sender.PacingActive {
		action = "APPROVED"
		actor = p.ApprovedBy
	}
	if p.Status == sender.PacingRejected {
		action = "REJECTED"
		actor = p.ApprovedBy
	}
	if p.Status == sender.PacingRetired {
		action = "RETIRED"
		actor = p.ApprovedBy
	}
	if actor == "" {
		actor = p.CreatedBy
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sender_pacing_policy_events(id,policy_id,action,actor_id,reason,policy_version,occurred_at) VALUES(gen_random_uuid(),$1::uuid,$2,$3::uuid,$4,$5,$6)`, p.ID, action, actor, p.Reason, p.Version, p.UpdatedAt)
	if err != nil {
		return sender.PacingPolicy{}, err
	}
	if err = tx.Commit(); err != nil {
		return sender.PacingPolicy{}, err
	}
	return p, nil
}
func (r *PacingPolicyRepository) ActiveByScope(ctx context.Context, scope sender.PacingScope, scopeID string, at time.Time) ([]sender.PacingPolicy, error) {
	rows, err := r.DB.QueryContext(ctx, pacingSelect+` WHERE scope=$1 AND coalesce(scope_id,'')=coalesce(NULLIF($2,''),'') AND status='ACTIVE' AND effective_from<=$3 AND (effective_to IS NULL OR effective_to>$3) ORDER BY effective_from,id`, scope, scopeID, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sender.PacingPolicy{}
	for rows.Next() {
		p, e := scanPacing(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
