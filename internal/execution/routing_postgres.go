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
	// Serialise routing-plan creation per campaign and assign the next version
	// inside the same transaction. This prevents two approvers from both
	// creating version 1 (or otherwise colliding on a client-supplied version).
	var campaignLock string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM campaigns WHERE id=$1::uuid FOR UPDATE`, plan.CampaignID).Scan(&campaignLock); err != nil {
		return RoutingPlan{}, err
	}
	var existingID, existingHash string
	err = tx.QueryRowContext(ctx, `SELECT id::text,request_hash FROM campaign_routing_plans WHERE campaign_id=$1::uuid AND idempotency_key=$2`, plan.CampaignID, plan.IdempotencyKey).Scan(&existingID, &existingHash)
	switch {
	case err == nil:
		if existingHash != plan.RequestHash {
			return RoutingPlan{}, ErrRoutingPlanConflict
		}
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return RoutingPlan{}, rollbackErr
		}
		return s.Get(ctx, existingID)
	case !errors.Is(err, sql.ErrNoRows):
		return RoutingPlan{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(plan_version),0)+1 FROM campaign_routing_plans WHERE campaign_id=$1::uuid`, plan.CampaignID).Scan(&plan.Version); err != nil {
		return RoutingPlan{}, err
	}
	if len(plan.Routes) == 0 || len(reservations) != len(plan.Routes) {
		return RoutingPlan{}, ErrRoutingPlanInvalid
	}
	for _, route := range plan.Routes {
		var maxMPM int
		var daily int64
		if err := tx.QueryRowContext(ctx, `SELECT max_messages_per_minute,daily_capacity FROM sender_pools WHERE id=$1::uuid AND status='ACTIVE' FOR UPDATE`, route.SenderPoolID).Scan(&maxMPM, &daily); err != nil {
			return RoutingPlan{}, err
		}
		var reservedMPM int
		var reservedHourly, reservedDaily int64
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(reserved_messages_per_minute),0)::int,coalesce(sum(reserved_hourly_units),0)::bigint,coalesce(sum(reserved_daily_units),0)::bigint FROM campaign_pool_capacity_reservations WHERE sender_pool_id=$1::uuid AND status IN ('HELD','ACTIVE') AND reservation_start<$3 AND reservation_end>$2`, route.SenderPoolID, reservations[0].ReservationStart, reservations[0].ReservationEnd).Scan(&reservedMPM, &reservedHourly, &reservedDaily); err != nil {
			return RoutingPlan{}, err
		}
		if reservationWouldOverbook(maxMPM, daily, reservedMPM, reservedHourly, reservedDaily, route) {
			return RoutingPlan{}, ErrCapacityOverbooked
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO campaign_routing_plans(id,campaign_id,plan_version,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by,idempotency_key,request_hash) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9::uuid,$10,$11)`, plan.ID, plan.CampaignID, plan.Version, plan.RoutingPolicyVersion, plan.CapacityEvidenceVersion, plan.PacingPolicyVersion, plan.FallbackMode, plan.ApprovedAt, plan.ApprovedBy, plan.IdempotencyKey, plan.RequestHash)
	if err != nil {
		return RoutingPlan{}, err
	}
	for _, r := range plan.Routes {
		_, err = tx.ExecContext(ctx, `INSERT INTO campaign_routing_plan_pools(routing_plan_id,sender_pool_id,gateway_pool_id,provider,engine,provider_adapter_version,provider_capability_definition_id,provider_capability_definition_version,gateway_pool_version,allocation_weight,maximum_recipients,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,allow_reallocation_in,allow_reallocation_out) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7::uuid,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, plan.ID, r.SenderPoolID, r.GatewayPoolID, r.Provider, r.Engine, r.ProviderAdapterVersion, r.ProviderDefinitionID, r.ProviderDefinitionVersion, r.GatewayPoolVersion, r.AllocationWeight, r.MaximumRecipients, r.ReservedMessagesPerMinute, r.ReservedHourlyUnits, r.ReservedDailyUnits, r.AllowReallocationIn, r.AllowReallocationOut)
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
	err := s.DB.QueryRowContext(ctx, `SELECT id::text,campaign_id::text,plan_version,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by::text,idempotency_key,request_hash FROM campaign_routing_plans WHERE id=$1::uuid`, id).Scan(&p.ID, &p.CampaignID, &p.Version, &p.RoutingPolicyVersion, &p.CapacityEvidenceVersion, &p.PacingPolicyVersion, &p.FallbackMode, &p.ApprovedAt, &p.ApprovedBy, &p.IdempotencyKey, &p.RequestHash)
	if errors.Is(err, sql.ErrNoRows) {
		return RoutingPlan{}, ErrRoutingPlanNotFound
	}
	if err != nil {
		return RoutingPlan{}, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT sender_pool_id::text,gateway_pool_id::text,provider,engine,coalesce(provider_adapter_version,''),coalesce(provider_capability_definition_id::text,''),coalesce(provider_capability_definition_version,0),coalesce(gateway_pool_version,0),allocation_weight,maximum_recipients,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,allow_reallocation_in,allow_reallocation_out FROM campaign_routing_plan_pools WHERE routing_plan_id=$1::uuid ORDER BY sender_pool_id`, id)
	if err != nil {
		return RoutingPlan{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var r PoolRoute
		if err := rows.Scan(&r.SenderPoolID, &r.GatewayPoolID, &r.Provider, &r.Engine, &r.ProviderAdapterVersion, &r.ProviderDefinitionID, &r.ProviderDefinitionVersion, &r.GatewayPoolVersion, &r.AllocationWeight, &r.MaximumRecipients, &r.ReservedMessagesPerMinute, &r.ReservedHourlyUnits, &r.ReservedDailyUnits, &r.AllowReallocationIn, &r.AllowReallocationOut); err != nil {
			return RoutingPlan{}, err
		}
		p.Routes = append(p.Routes, r)
	}
	return p, rows.Err()
}
func (s *PostgreSQLRoutingPlanStore) ListByCampaign(ctx context.Context, campaignID string) ([]RoutingPlan, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text FROM campaign_routing_plans WHERE campaign_id=$1::uuid ORDER BY plan_version DESC LIMIT 100`, campaignID)
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
func (s *PostgreSQLRoutingPlanStore) ListRoutingPlanPage(ctx context.Context, campaignID string, limit int, beforeVersion int64) ([]RoutingPlan, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text FROM campaign_routing_plans WHERE campaign_id=$1::uuid AND ($3=0 OR plan_version<$3) ORDER BY plan_version DESC LIMIT $2`, campaignID, limit, beforeVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var identifier string
		if err := rows.Scan(&identifier); err != nil {
			return nil, err
		}
		ids = append(ids, identifier)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]RoutingPlan, 0, len(ids))
	for _, identifier := range ids {
		plan, err := s.Get(ctx, identifier)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
	return out, nil
}

func (s *PostgreSQLRoutingPlanStore) LatestByCampaign(ctx context.Context, campaignID string) (RoutingPlan, error) {
	if s == nil || s.DB == nil {
		return RoutingPlan{}, errors.New("database is required")
	}
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT id::text FROM campaign_routing_plans WHERE campaign_id=$1::uuid ORDER BY plan_version DESC,approved_at DESC,id DESC LIMIT 1`, campaignID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return RoutingPlan{}, ErrRoutingPlanNotFound
	}
	if err != nil {
		return RoutingPlan{}, err
	}
	return s.Get(ctx, id)
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
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists bool
		if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM campaign_pool_capacity_reservations WHERE routing_plan_id=$1::uuid AND status='ACTIVE')`, planID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrRoutingPlanNotFound
		}
	}
	return nil
}
func (s *PostgreSQLRoutingPlanStore) ReleaseReservations(ctx context.Context, planID, actor string, now time.Time) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE campaign_pool_capacity_reservations SET status='RELEASED',fencing_version=fencing_version+1,updated_at=$2 WHERE routing_plan_id=$1::uuid AND status IN ('HELD','ACTIVE')`, planID, now)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrRoutingPlanNotFound
	}
	return nil
}

func (s *PostgreSQLRoutingPlanStore) PoolExecutionReport(ctx context.Context, planID string) ([]PoolExecutionReport, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.routing_plan_id::text,p.sender_pool_id::text,p.gateway_pool_id::text,p.provider,p.engine,
 coalesce(p.provider_adapter_version,''),coalesce(p.provider_capability_definition_id::text,''),coalesce(p.provider_capability_definition_version,0),coalesce(p.gateway_pool_version,0),
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
GROUP BY p.routing_plan_id,p.sender_pool_id,p.gateway_pool_id,p.provider,p.engine,p.provider_adapter_version,p.provider_capability_definition_id,p.provider_capability_definition_version,p.gateway_pool_version,p.maximum_recipients,p.reserved_messages_per_minute,p.reserved_hourly_units,p.reserved_daily_units
ORDER BY p.sender_pool_id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PoolExecutionReport{}
	for rows.Next() {
		var v PoolExecutionReport
		if err := rows.Scan(&v.RoutingPlanID, &v.SenderPoolID, &v.GatewayPoolID, &v.Provider, &v.Engine, &v.ProviderAdapterVersion, &v.ProviderDefinitionID, &v.ProviderDefinitionVersion, &v.GatewayPoolVersion, &v.MaximumRecipients, &v.ReservedMessagesPerMinute, &v.ReservedHourlyUnits, &v.ReservedDailyUnits, &v.ShardCount, &v.RecipientCount, &v.QueuedCount, &v.SubmittedCount, &v.SentCount, &v.DeliveredCount, &v.ReadCount, &v.FailedCount, &v.UnknownCount, &v.TerminalCount); err != nil {
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

func reservationWouldOverbook(maxMPM int, daily int64, reservedMPM int, reservedHourly, reservedDaily int64, route PoolRoute) bool {
	maxHourly := saturatingMultiply(int64(maxMPM), 60)
	return reservedMPM+route.ReservedMessagesPerMinute > maxMPM ||
		reservedHourly+route.ReservedHourlyUnits > maxHourly ||
		reservedDaily+route.ReservedDailyUnits > daily
}
