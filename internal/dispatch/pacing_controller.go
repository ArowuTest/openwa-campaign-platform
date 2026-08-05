package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
	"campaign-platform/internal/sender"
)

var (
	ErrHourlyAllowanceExhausted = errors.New("sender hourly allowance exhausted")
	ErrDailyAllowanceExhausted  = errors.New("sender daily allowance exhausted")
	ErrActiveCampaignLimit      = errors.New("sender active campaign limit reached")
)

type PacingController interface {
	Wait(context.Context, delivery.Recipient, Material, time.Time) error
}

type PostgreSQLPacingController struct {
	DB       *sql.DB
	Policies *sender.PacingAdministration
}

func (c *PostgreSQLPacingController) Wait(ctx context.Context, recipient delivery.Recipient, material Material, now time.Time) error {
	if c == nil || c.DB == nil || c.Policies == nil {
		return errors.New("pacing controller dependencies are required")
	}
	scopes := []sender.PacingScopeRef{{Scope: sender.PacingPlatform}}
	if material.Provider != "" {
		scopes = append(scopes, sender.PacingScopeRef{Scope: sender.PacingProvider, ID: material.Provider})
	}
	if material.Engine != "" {
		scopes = append(scopes, sender.PacingScopeRef{Scope: sender.PacingEngine, ID: material.Engine})
	}
	if material.GatewayPoolID != "" {
		scopes = append(scopes, sender.PacingScopeRef{Scope: sender.PacingGatewayPool, ID: material.GatewayPoolID})
	}
	if material.SenderPoolID != "" {
		scopes = append(scopes, sender.PacingScopeRef{Scope: sender.PacingSenderPool, ID: material.SenderPoolID})
	}
	scopes = append(scopes, sender.PacingScopeRef{Scope: sender.PacingSession, ID: material.SessionID}, sender.PacingScopeRef{Scope: sender.PacingCampaign, ID: recipient.CampaignID})
	resolved, err := c.Policies.Resolve(ctx, scopes, now)
	if err != nil {
		return fmt.Errorf("resolve pacing policy: %w", err)
	}
	p := resolved.Policy
	minDelay, maxDelay := p.MinimumDelayMS, p.MaximumDelayMS
	mt := strings.ToUpper(strings.TrimSpace(material.MessageType))
	for _, o := range p.Overrides {
		if o.MessageType == mt {
			minDelay, maxDelay = o.MinimumDelayMS, o.MaximumDelayMS
			break
		}
	}
	tx, err := c.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sender_session_campaign_assignments WHERE sender_session_id=$1::uuid AND status='ACTIVE' AND campaign_id<>$2::uuid`, material.SessionID, recipient.CampaignID).Scan(&active); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sender_session_campaign_assignments WHERE sender_session_id=$1::uuid AND campaign_id=$2::uuid AND status='ACTIVE')`, material.SessionID, recipient.CampaignID).Scan(&exists); err != nil {
		return err
	}
	if !exists && active >= p.MaxActiveCampaigns {
		return ErrActiveCampaignLimit
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sender_session_campaign_assignments(sender_session_id,campaign_id,status,assigned_at,last_used_at) VALUES($1::uuid,$2::uuid,'ACTIVE',$3,$3) ON CONFLICT(sender_session_id,campaign_id) DO UPDATE SET status='ACTIVE',last_used_at=excluded.last_used_at,released_at=NULL`, material.SessionID, recipient.CampaignID, now)
	if err != nil {
		return err
	}
	hour := now.UTC().Truncate(time.Hour)
	res, err := tx.ExecContext(ctx, `INSERT INTO sender_hourly_submission_usage(sender_session_id,hour_start,submission_count,updated_at) VALUES($1::uuid,$2,1,$3) ON CONFLICT(sender_session_id,hour_start) DO UPDATE SET submission_count=sender_hourly_submission_usage.submission_count+1,updated_at=excluded.updated_at WHERE sender_hourly_submission_usage.submission_count<$4`, material.SessionID, hour, now, p.HourlyAllowance)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrHourlyAllowanceExhausted
	}
	day := now.UTC().Format("2006-01-02")
	res, err = tx.ExecContext(ctx, `INSERT INTO sender_daily_submission_usage(sender_session_id,usage_date,submission_count,updated_at) VALUES($1::uuid,$2::date,1,$3) ON CONFLICT(sender_session_id,usage_date) DO UPDATE SET submission_count=sender_daily_submission_usage.submission_count+1,updated_at=excluded.updated_at WHERE sender_daily_submission_usage.submission_count<$4`, material.SessionID, day, now, p.DailyAllowance)
	if err != nil {
		return err
	}
	n, _ = res.RowsAffected()
	if n != 1 {
		return ErrDailyAllowanceExhausted
	}
	var next time.Time
	var seq uint64
	err = tx.QueryRowContext(ctx, `INSERT INTO sender_pacing_runtime(sender_session_id,next_allowed_at,jitter_sequence,policy_id,policy_version,updated_at) VALUES($1::uuid,$2,0,$3::uuid,$4,$2) ON CONFLICT(sender_session_id) DO UPDATE SET policy_id=excluded.policy_id,policy_version=excluded.policy_version,updated_at=excluded.updated_at RETURNING next_allowed_at,jitter_sequence`, material.SessionID, now, p.ID, p.Version).Scan(&next, &seq)
	if err != nil {
		return err
	}
	target := next
	if target.Before(now) {
		target = now
	}
	interval := pacingInterval(material.SessionID, seq, minDelay, maxDelay, p.JitterMode)
	_, err = tx.ExecContext(ctx, `UPDATE sender_pacing_runtime SET next_allowed_at=$2,jitter_sequence=jitter_sequence+1,policy_id=$3::uuid,policy_version=$4,updated_at=$5 WHERE sender_session_id=$1::uuid`, material.SessionID, target.Add(interval), p.ID, p.Version, now)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if !target.After(now) {
		return nil
	}
	timer := time.NewTimer(target.Sub(now))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func pacingInterval(key string, seq uint64, minMS, maxMS int64, mode sender.JitterMode) time.Duration {
	if maxMS <= minMS || mode != sender.JitterUniform {
		return time.Duration(minMS) * time.Millisecond
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	var b [8]byte
	for i := 0; i < 8; i++ {
		b[i] = byte(seq >> (8 * i))
	}
	_, _ = h.Write(b[:])
	span := uint64(maxMS - minMS)
	return time.Duration(minMS+int64(h.Sum64()%(span+1))) * time.Millisecond
}
