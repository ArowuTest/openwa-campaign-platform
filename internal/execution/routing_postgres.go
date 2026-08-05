package execution

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type PostgreSQLRoutingPlanStore struct{ DB *sql.DB }

func (s *PostgreSQLRoutingPlanStore) Create(ctx context.Context, plan RoutingPlan, reservations []CapacityReservation) (RoutingPlan, error) {
	if s == nil || s.DB == nil {
		return RoutingPlan{}, errors.New("database is required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return RoutingPlan{}, err
	}
	defer tx.Rollback()
	for _, route := range plan.Routes {
		var maxMPM int
		var daily int64
		if err := tx.QueryRowContext(ctx, `SELECT max_messages_per_minute,daily_capacity FROM sender_pools WHERE id=$1::uuid AND status='ACTIVE' FOR UPDATE`, route.SenderPoolID).Scan(&maxMPM, &daily); err != nil {
			return RoutingPlan{}, err
		}
		var reservedMPM int
		var reservedDaily int64
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(reserved_messages_per_minute),0)::int,coalesce(sum(reserved_daily_units),0)::bigint FROM campaign_pool_capacity_reservations WHERE sender_pool_id=$1::uuid AND status IN ('HELD','ACTIVE') AND reservation_start<$3 AND reservation_end>$2`, route.SenderPoolID, reservations[0].ReservationStart, reservations[0].ReservationEnd).Scan(&reservedMPM, &reservedDaily); err != nil {
			return RoutingPlan{}, err
		}
		if reservedMPM+route.ReservedMessagesPerMinute > maxMPM || reservedDaily+route.ReservedDailyUnits > daily {
			return RoutingPlan{}, ErrCapacityOverbooked
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO campaign_routing_plans(id,campaign_id,plan_version,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9::uuid)`, plan.ID, plan.CampaignID, plan.Version, plan.RoutingPolicyVersion, plan.CapacityEvidenceVersion, plan.PacingPolicyVersion, plan.FallbackMode, plan.ApprovedAt, plan.ApprovedBy)
	if err != nil {
		return RoutingPlan{}, err
	}
	for _, r := range plan.Routes {
		_, err = tx.ExecContext(ctx, `INSERT INTO campaign_routing_plan_pools(routing_plan_id,sender_pool_id,gateway_pool_id,provider,engine,allocation_weight,maximum_recipients,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,allow_reallocation_in,allow_reallocation_out) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, plan.ID, r.SenderPoolID, r.GatewayPoolID, r.Provider, r.Engine, r.AllocationWeight, r.MaximumRecipients, r.ReservedMessagesPerMinute, r.ReservedHourlyUnits, r.ReservedDailyUnits, r.AllowReallocationIn, r.AllowReallocationOut)
		if err != nil {
			return RoutingPlan{}, err
		}
	}
	for _, r := range reservations {
		_, err = tx.ExecContext(ctx, `INSERT INTO campaign_pool_capacity_reservations(id,campaign_id,routing_plan_id,sender_pool_id,reservation_start,reservation_end,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,status,fencing_version,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, r.ID, r.CampaignID, r.RoutingPlanID, r.SenderPoolID, r.ReservationStart, r.ReservationEnd, r.ReservedMessagesPerMinute, r.ReservedHourlyUnits, r.ReservedDailyUnits, r.Status, r.FencingVersion, r.CreatedAt, r.UpdatedAt)
		if err != nil {
			return RoutingPlan{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return RoutingPlan{}, err
	}
	return plan, nil
}

func (s *PostgreSQLRoutingPlanStore) Get(ctx context.Context, id string) (RoutingPlan, error) {
	var p RoutingPlan
	err := s.DB.QueryRowContext(ctx, `SELECT id::text,campaign_id::text,plan_version,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by::text FROM campaign_routing_plans WHERE id=$1::uuid`, id).Scan(&p.ID, &p.CampaignID, &p.Version, &p.RoutingPolicyVersion, &p.CapacityEvidenceVersion, &p.PacingPolicyVersion, &p.FallbackMode, &p.ApprovedAt, &p.ApprovedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return RoutingPlan{}, ErrRoutingPlanNotFound
	}
	if err != nil {
		return RoutingPlan{}, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT sender_pool_id::text,gateway_pool_id::text,provider,engine,allocation_weight,maximum_recipients,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,allow_reallocation_in,allow_reallocation_out FROM campaign_routing_plan_pools WHERE routing_plan_id=$1::uuid ORDER BY sender_pool_id`, id)
	if err != nil {
		return RoutingPlan{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var r PoolRoute
		if err := rows.Scan(&r.SenderPoolID, &r.GatewayPoolID, &r.Provider, &r.Engine, &r.AllocationWeight, &r.MaximumRecipients, &r.ReservedMessagesPerMinute, &r.ReservedHourlyUnits, &r.ReservedDailyUnits, &r.AllowReallocationIn, &r.AllowReallocationOut); err != nil {
			return RoutingPlan{}, err
		}
		p.Routes = append(p.Routes, r)
	}
	return p, rows.Err()
}
func (s *PostgreSQLRoutingPlanStore) ListByCampaign(ctx context.Context, campaignID string) ([]RoutingPlan, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text FROM campaign_routing_plans WHERE campaign_id=$1::uuid ORDER BY plan_version DESC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]RoutingPlan, 0, len(ids))
	for _, id := range ids {
		p, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
func (s *PostgreSQLRoutingPlanStore) Reservations(ctx context.Context, planID string) ([]CapacityReservation, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,campaign_id::text,routing_plan_id::text,sender_pool_id::text,reservation_start,reservation_end,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,status,fencing_version,created_at,updated_at FROM campaign_pool_capacity_reservations WHERE routing_plan_id=$1::uuid ORDER BY sender_pool_id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CapacityReservation{}
	for rows.Next() {
		var r CapacityReservation
		if err := rows.Scan(&r.ID, &r.CampaignID, &r.RoutingPlanID, &r.SenderPoolID, &r.ReservationStart, &r.ReservationEnd, &r.ReservedMessagesPerMinute, &r.ReservedHourlyUnits, &r.ReservedDailyUnits, &r.Status, &r.FencingVersion, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *PostgreSQLRoutingPlanStore) ActivateReservations(ctx context.Context, planID string, now time.Time) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE campaign_pool_capacity_reservations SET status='ACTIVE',fencing_version=fencing_version+1,updated_at=$2 WHERE routing_plan_id=$1::uuid AND status='HELD'`, planID, now)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrRoutingPlanNotFound
	}
	return nil
}
func (s *PostgreSQLRoutingPlanStore) ReleaseReservations(ctx context.Context, planID, actor string, now time.Time) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE campaign_pool_capacity_reservations SET status='RELEASED',fencing_version=fencing_version+1,updated_at=$2 WHERE routing_plan_id=$1::uuid AND status IN ('HELD','ACTIVE')`, planID, now)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrRoutingPlanNotFound
	}
	return nil
}

func (s *PostgreSQLRoutingPlanStore) PoolExecutionReport(ctx context.Context, planID string) ([]PoolExecutionReport, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.routing_plan_id::text,p.sender_pool_id::text,p.gateway_pool_id::text,p.provider,p.engine,
 p.maximum_recipients,p.reserved_messages_per_minute,p.reserved_hourly_units,p.reserved_daily_units,
 count(DISTINCT sh.id)::bigint,count(cr.id)::bigint,
 count(cr.id) FILTER (WHERE cr.status IN ('AUTHORISED','QUEUED','CLAIMED'))::bigint,
 count(cr.id) FILTER (WHERE cr.status IN ('SUBMITTING','GATEWAY_ACCEPTED'))::bigint,
 count(cr.id) FILTER (WHERE cr.status='SENT')::bigint,
 count(cr.id) FILTER (WHERE cr.status='DELIVERED')::bigint,
 count(cr.id) FILTER (WHERE cr.status='READ')::bigint,
 count(cr.id) FILTER (WHERE cr.status IN ('FAILED_RETRYABLE','FAILED_PERMANENT'))::bigint,
 count(cr.id) FILTER (WHERE cr.status='UNKNOWN')::bigint,
 count(cr.id) FILTER (WHERE cr.status IN ('GATEWAY_ACCEPTED','SENT','DELIVERED','READ','FAILED_PERMANENT','UNKNOWN','SUPPRESSED_BEFORE_SEND','CANCELLED'))::bigint
FROM campaign_routing_plan_pools p
LEFT JOIN campaign_dispatch_shards sh ON sh.routing_plan_id=p.routing_plan_id AND sh.assigned_sender_pool_id=p.sender_pool_id
LEFT JOIN campaign_recipients cr ON cr.dispatch_shard_id=sh.id
WHERE p.routing_plan_id=$1::uuid
GROUP BY p.routing_plan_id,p.sender_pool_id,p.gateway_pool_id,p.provider,p.engine,p.maximum_recipients,p.reserved_messages_per_minute,p.reserved_hourly_units,p.reserved_daily_units
ORDER BY p.sender_pool_id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PoolExecutionReport{}
	for rows.Next() {
		var v PoolExecutionReport
		if err := rows.Scan(&v.RoutingPlanID, &v.SenderPoolID, &v.GatewayPoolID, &v.Provider, &v.Engine, &v.MaximumRecipients, &v.ReservedMessagesPerMinute, &v.ReservedHourlyUnits, &v.ReservedDailyUnits, &v.ShardCount, &v.RecipientCount, &v.QueuedCount, &v.SubmittedCount, &v.SentCount, &v.DeliveredCount, &v.ReadCount, &v.FailedCount, &v.UnknownCount, &v.TerminalCount); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrRoutingPlanNotFound
	}
	return out, nil
}
