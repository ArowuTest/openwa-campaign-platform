package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type PostgreSQLStore struct{ DB *sql.DB }

func (s *PostgreSQLStore) RecordAdmission(ctx context.Context, e CapacityEvidence) error {
	if s == nil || s.DB == nil {
		return errors.New("database is required")
	}
	reasons, _ := json.Marshal(e.Reasons)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO campaign_capacity_assessments(campaign_id,capacity_reference_id,evidence_version,remaining_recipients,available_messages_per_minute,available_daily_capacity,safety_margin_percent,required_messages_per_minute,effective_messages_per_minute,forecast_completion_at,deadline_at,decision,reasons,evaluated_at) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, e.CampaignID, e.PoolID, e.EvidenceVersion, e.RemainingRecipients, e.AvailableMessagesPerMinute, e.AvailableDailyCapacity, e.SafetyMarginPercent, e.RequiredMessagesPerMinute, e.EffectiveMessagesPerMinute, e.ForecastCompletionAt, e.DeadlineAt, e.Decision, reasons, e.EvaluatedAt)
	return err
}
func (s *PostgreSQLStore) Metrics(ctx context.Context, id string) (Metrics, error) {
	var m Metrics
	err := s.DB.QueryRowContext(ctx, `SELECT cm.authorised_total,cm.queued_total,cm.submitted_total,cm.sent_total,cm.delivered_total,cm.read_total,cm.failed_total,cm.unknown_total,cm.suppressed_total,coalesce((SELECT count(*) FROM campaign_recipients cr WHERE cr.campaign_id=cm.campaign_id AND cr.status IN ('AUTHORISED','QUEUED','CLAIMED','SUBMITTING','GATEWAY_ACCEPTED','FAILED_RETRYABLE')),0) FROM campaign_metrics cm WHERE cm.campaign_id=$1::uuid`, id).Scan(&m.Authorised, &m.Queued, &m.Submitted, &m.Sent, &m.Delivered, &m.Read, &m.Failed, &m.Unknown, &m.Suppressed, &m.Pending)
	return m, err
}
func (s *PostgreSQLStore) Capacity(ctx context.Context, referenceID string, now time.Time) (int, int64, error) {
	var rate int
	var daily int64
	err := s.DB.QueryRowContext(ctx, `
WITH pool_capacity AS (
 SELECT coalesce(sum(least(ss.safe_messages_per_minute,sp.max_messages_per_minute)),0)::int AS rate,
        greatest(coalesce(sum(greatest(ss.safe_daily_capacity-ss.sent_today,0)),0)-sp.reserved_capacity,0)::bigint AS daily
 FROM sender_pools sp LEFT JOIN sender_sessions ss ON ss.sender_pool_id=sp.id AND ss.status IN('READY','BUSY') AND ss.last_heartbeat_at>$2
 WHERE sp.id=$1::uuid AND sp.status='ACTIVE' GROUP BY sp.reserved_capacity
), session_capacity AS (
 SELECT ss.safe_messages_per_minute::int AS rate,greatest(ss.safe_daily_capacity-ss.sent_today,0)::bigint AS daily
 FROM sender_sessions ss JOIN sender_nodes sn ON sn.id=ss.node_id
 WHERE ss.id=$1::uuid AND ss.status IN('READY','BUSY') AND sn.status='READY' AND NOT sn.draining AND ss.last_heartbeat_at>$2 AND sn.last_heartbeat_at>$2
)
SELECT rate,daily FROM pool_capacity UNION ALL SELECT rate,daily FROM session_capacity LIMIT 1`, referenceID, now.Add(-90*time.Second)).Scan(&rate, &daily)
	return rate, daily, err
}

func (s *PostgreSQLStore) RecordEvent(ctx context.Context, campaignID, eventType, actor, reason string, details map[string]any, now time.Time) error {
	if s == nil || s.DB == nil {
		return errors.New("database is required")
	}
	b, _ := json.Marshal(details)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO campaign_execution_events(campaign_id,event_type,actor_id,reason,details,created_at) VALUES($1::uuid,$2,$3,NULLIF($4,''),$5,$6)`, campaignID, eventType, actor, reason, b, now.UTC())
	return err
}
