package execution

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
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
	var campaignLock, campaignOrganisationID, campaignStatus string
	var campaignStart, campaignEnd sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT id::text,organisation_id::text,status,requested_start_at,completion_deadline_at FROM campaigns WHERE id=$1::uuid FOR UPDATE`, plan.CampaignID).Scan(&campaignLock, &campaignOrganisationID, &campaignStatus, &campaignStart, &campaignEnd); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RoutingPlan{}, ErrRoutingPlanInvalid
		}
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
	var liveReservationExists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM campaign_pool_capacity_reservations WHERE campaign_id=$1::uuid AND status IN ('HELD','ACTIVE'))`, plan.CampaignID).Scan(&liveReservationExists); err != nil {
		return RoutingPlan{}, err
	}
	if liveReservationExists {
		return RoutingPlan{}, ErrRoutingPlanConflict
	}
	if routingPlanCreationBlocked(campaignStatus) {
		return RoutingPlan{}, ErrRoutingPlanConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(plan_version),0)+1 FROM campaign_routing_plans WHERE campaign_id=$1::uuid`, plan.CampaignID).Scan(&plan.Version); err != nil {
		return RoutingPlan{}, err
	}
	if len(plan.Routes) == 0 || len(reservations) != len(plan.Routes) || !campaignStart.Valid || !campaignEnd.Valid || !campaignEnd.Time.After(campaignStart.Time) {
		return RoutingPlan{}, ErrRoutingPlanInvalid
	}
	plan.Routes = append([]PoolRoute(nil), plan.Routes...)
	sort.Slice(plan.Routes, func(i, j int) bool { return plan.Routes[i].SenderPoolID < plan.Routes[j].SenderPoolID })
	windowStart := campaignStart.Time.UTC()
	windowEnd := campaignEnd.Time.UTC()
	reservationByPool := make(map[string]CapacityReservation, len(reservations))
	for _, reservation := range reservations {
		if reservation.ID == "" || reservation.CampaignID != plan.CampaignID || reservation.RoutingPlanID != plan.ID || reservation.SenderPoolID == "" ||
			!reservation.ReservationStart.Equal(windowStart) || !reservation.ReservationEnd.Equal(windowEnd) || reservation.Status != "HELD" || reservation.FencingVersion != 1 ||
			!reservation.CreatedAt.Equal(plan.ApprovedAt) || !reservation.UpdatedAt.Equal(plan.ApprovedAt) {
			return RoutingPlan{}, ErrRoutingPlanInvalid
		}
		if _, exists := reservationByPool[reservation.SenderPoolID]; exists {
			return RoutingPlan{}, ErrRoutingPlanInvalid
		}
		reservationByPool[reservation.SenderPoolID] = reservation
	}
	for _, route := range plan.Routes {
		if route.ReservedMessagesPerMinute < 1 || route.ReservedHourlyUnits < 1 || route.ReservedDailyUnits < route.ReservedHourlyUnits {
			return RoutingPlan{}, ErrRoutingPlanInvalid
		}
		reservation, ok := reservationByPool[route.SenderPoolID]
		if !ok || reservation.ReservedMessagesPerMinute != route.ReservedMessagesPerMinute || reservation.ReservedHourlyUnits != route.ReservedHourlyUnits || reservation.ReservedDailyUnits != route.ReservedDailyUnits {
			return RoutingPlan{}, ErrRoutingPlanInvalid
		}
		var maxMPM int
		var daily int64
		if err := tx.QueryRowContext(ctx, `SELECT max_messages_per_minute,daily_capacity FROM sender_pools WHERE id=$1::uuid AND status='ACTIVE' AND (organisation_id IS NULL OR organisation_id=$2::uuid) FOR UPDATE`, route.SenderPoolID, campaignOrganisationID).Scan(&maxMPM, &daily); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return RoutingPlan{}, ErrRoutingPlanInvalid
			}
			return RoutingPlan{}, err
		}
		var reservedMPM int
		var reservedHourly, reservedDaily int64
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(reserved_messages_per_minute),0)::int,coalesce(sum(reserved_hourly_units),0)::bigint FROM campaign_pool_capacity_reservations WHERE sender_pool_id=$1::uuid AND status IN ('HELD','ACTIVE') AND reservation_start<$3 AND reservation_end>$2`, route.SenderPoolID, windowStart, windowEnd).Scan(&reservedMPM, &reservedHourly); err != nil {
			return RoutingPlan{}, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(day_reserved),0)::bigint FROM (SELECT day_bucket,coalesce(sum(r.reserved_daily_units),0)::bigint AS day_reserved FROM generate_series(date_trunc('day',$2::timestamptz),date_trunc('day',$3::timestamptz-interval '1 microsecond'),interval '1 day') AS day_bucket LEFT JOIN campaign_pool_capacity_reservations r ON r.sender_pool_id=$1::uuid AND r.status IN ('HELD','ACTIVE') AND r.reservation_start<day_bucket+interval '1 day' AND r.reservation_end>day_bucket GROUP BY day_bucket) daily`, route.SenderPoolID, windowStart, windowEnd).Scan(&reservedDaily); err != nil {
			return RoutingPlan{}, err
		}
		if reservationWouldOverbook(maxMPM, daily, reservedMPM, reservedHourly, reservedDaily, route) {
			return RoutingPlan{}, ErrCapacityOverbooked
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO campaign_routing_plans(id,campaign_id,plan_version,distribution_mode,routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by,idempotency_key,request_hash) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10::uuid,$11,$12)`, plan.ID, plan.CampaignID, plan.Version, plan.DistributionMode, plan.RoutingPolicyVersion, plan.CapacityEvidenceVersion, plan.PacingPolicyVersion, plan.FallbackMode, plan.ApprovedAt, plan.ApprovedBy, plan.IdempotencyKey, plan.RequestHash)
	if err != nil {
		return RoutingPlan{}, err
	}
	for _, r := range plan.Routes {
		_, err = tx.ExecContext(ctx, `INSERT INTO campaign_routing_plan_pools(routing_plan_id,sender_pool_id,gateway_pool_id,meta_sender_id,provider,engine,provider_adapter_version,provider_capability_definition_id,provider_capability_definition_version,gateway_pool_version,meta_sender_version,allocation_weight,maximum_recipients,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,allow_reallocation_in,allow_reallocation_out) VALUES($1::uuid,$2::uuid,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,NULLIF($8,'')::uuid,NULLIF($9,0),NULLIF($10,0),NULLIF($11,0),$12,$13,$14,$15,$16,$17,$18)`, plan.ID, r.SenderPoolID, r.GatewayPoolID, r.MetaSenderID, r.Provider, r.Engine, r.ProviderAdapterVersion, r.ProviderDefinitionID, r.ProviderDefinitionVersion, r.GatewayPoolVersion, r.MetaSenderVersion, r.AllocationWeight, r.MaximumRecipients, r.ReservedMessagesPerMinute, r.ReservedHourlyUnits, r.ReservedDailyUnits, r.AllowReallocationIn, r.AllowReallocationOut)
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
	if s == nil || s.DB == nil {
		return RoutingPlan{}, errors.New("database is required")
	}
	var p RoutingPlan
	err := s.DB.QueryRowContext(ctx, `SELECT id::text,campaign_id::text,plan_version,coalesce(distribution_mode,'WEIGHTED'),routing_policy_version,capacity_evidence_version,pacing_policy_version,fallback_mode,approved_at,approved_by::text,idempotency_key,request_hash FROM campaign_routing_plans WHERE id=$1::uuid`, id).Scan(&p.ID, &p.CampaignID, &p.Version, &p.DistributionMode, &p.RoutingPolicyVersion, &p.CapacityEvidenceVersion, &p.PacingPolicyVersion, &p.FallbackMode, &p.ApprovedAt, &p.ApprovedBy, &p.IdempotencyKey, &p.RequestHash)
	if errors.Is(err, sql.ErrNoRows) {
		return RoutingPlan{}, ErrRoutingPlanNotFound
	}
	if err != nil {
		return RoutingPlan{}, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT sender_pool_id::text,coalesce(gateway_pool_id::text,''),coalesce(meta_sender_id::text,''),provider,engine,coalesce(provider_adapter_version,''),coalesce(provider_capability_definition_id::text,''),coalesce(provider_capability_definition_version,0),coalesce(gateway_pool_version,0),coalesce(meta_sender_version,0),allocation_weight,maximum_recipients,reserved_messages_per_minute,reserved_hourly_units,reserved_daily_units,allow_reallocation_in,allow_reallocation_out FROM campaign_routing_plan_pools WHERE routing_plan_id=$1::uuid ORDER BY sender_pool_id`, id)
	if err != nil {
		return RoutingPlan{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var r PoolRoute
		if err := rows.Scan(&r.SenderPoolID, &r.GatewayPoolID, &r.MetaSenderID, &r.Provider, &r.Engine, &r.ProviderAdapterVersion, &r.ProviderDefinitionID, &r.ProviderDefinitionVersion, &r.GatewayPoolVersion, &r.MetaSenderVersion, &r.AllocationWeight, &r.MaximumRecipients, &r.ReservedMessagesPerMinute, &r.ReservedHourlyUnits, &r.ReservedDailyUnits, &r.AllowReallocationIn, &r.AllowReallocationOut); err != nil {
			return RoutingPlan{}, err
		}
		p.Routes = append(p.Routes, r)
	}
	return p, rows.Err()
}
func (s *PostgreSQLRoutingPlanStore) ListByCampaign(ctx context.Context, campaignID string) ([]RoutingPlan, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
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
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
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
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
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

type routingMutationExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func lockRoutingReservations(
	ctx context.Context,
	executor routingMutationExecutor,
	planID string,
	expectedCampaignID string,
) (string, []string, error) {
	var campaignID string
	err := executor.QueryRowContext(ctx, `
SELECT campaign_id::text
FROM campaign_routing_plans
WHERE id=$1::uuid
FOR UPDATE`, planID).Scan(&campaignID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrRoutingPlanNotFound
	}
	if err != nil {
		return "", nil, err
	}
	if expectedCampaignID != "" && campaignID != expectedCampaignID {
		return "", nil, ErrRoutingPlanConflict
	}
	rows, err := executor.QueryContext(ctx, `
SELECT status
FROM campaign_pool_capacity_reservations
WHERE routing_plan_id=$1::uuid
ORDER BY id
FOR UPDATE`, planID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	statuses := []string{}
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			return "", nil, err
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	if len(statuses) == 0 {
		return "", nil, ErrRoutingPlanNotFound
	}
	return campaignID, statuses, nil
}

func activateRoutingReservations(
	ctx context.Context,
	executor routingMutationExecutor,
	planID string,
	campaignID string,
	now time.Time,
) error {
	_, statuses, err := lockRoutingReservations(ctx, executor, planID, campaignID)
	if err != nil {
		return err
	}
	held, active := 0, 0
	for _, status := range statuses {
		switch status {
		case "HELD":
			held++
		case "ACTIVE":
			active++
		default:
			return ErrRoutingPlanConflict
		}
	}
	if active == len(statuses) {
		return nil
	}
	if held != len(statuses) {
		return ErrRoutingPlanConflict
	}
	result, err := executor.ExecContext(ctx, `
UPDATE campaign_pool_capacity_reservations
SET status='ACTIVE',fencing_version=fencing_version+1,updated_at=$2
WHERE routing_plan_id=$1::uuid AND status='HELD'`, planID, now.UTC())
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != int64(len(statuses)) {
		return ErrRoutingPlanConflict
	}
	return nil
}

func (s *PostgreSQLRoutingPlanStore) ActivateReservations(
	ctx context.Context, planID string, now time.Time,
) error {
	if s == nil || s.DB == nil {
		return errors.New("database is required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := activateRoutingReservations(ctx, tx, planID, "", now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgreSQLRoutingPlanStore) ActivateReservationsInTx(
	ctx context.Context,
	tx *sql.Tx,
	planID string,
	campaignID string,
	now time.Time,
) error {
	if s == nil || s.DB == nil || tx == nil {
		return errors.New("database transaction is required")
	}
	return activateRoutingReservations(ctx, tx, planID, campaignID, now)
}

func releaseRoutingReservations(
	ctx context.Context,
	executor routingMutationExecutor,
	planID string,
	expectedCampaignID string,
	actor string,
	now time.Time,
) error {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ErrRoutingPlanInvalid
	}
	var campaignID string
	err := executor.QueryRowContext(ctx, `
SELECT campaign_id::text
FROM campaign_routing_plans
WHERE id=$1::uuid`, planID).Scan(&campaignID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRoutingPlanNotFound
	}
	if err != nil {
		return err
	}
	if expectedCampaignID != "" && campaignID != expectedCampaignID {
		return ErrRoutingPlanConflict
	}
	var campaignStatus string
	err = executor.QueryRowContext(ctx, `
SELECT status
FROM campaigns
WHERE id=$1::uuid
FOR UPDATE`, campaignID).Scan(&campaignStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRoutingPlanNotFound
	}
	if err != nil {
		return err
	}
	if campaignStatus == "DISPATCHING" || campaignStatus == "PAUSED" {
		return ErrRoutingPlanConflict
	}
	lockedCampaignID, statuses, err := lockRoutingReservations(ctx, executor, planID, campaignID)
	if err != nil {
		return err
	}
	if lockedCampaignID != campaignID {
		return ErrRoutingPlanConflict
	}
	releasable, released := 0, 0
	for _, status := range statuses {
		switch status {
		case "HELD", "ACTIVE":
			releasable++
		case "RELEASED":
			released++
		default:
			return ErrRoutingPlanConflict
		}
	}
	if released == len(statuses) {
		return nil
	}
	if releasable != len(statuses) {
		return ErrRoutingPlanConflict
	}
	result, err := executor.ExecContext(ctx, `
UPDATE campaign_pool_capacity_reservations
SET status='RELEASED',fencing_version=fencing_version+1,updated_at=$2,released_by=NULLIF($3,''),released_at=$2
WHERE routing_plan_id=$1::uuid AND status IN ('HELD','ACTIVE')`,
		planID, now.UTC(), actor,
	)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != int64(len(statuses)) {
		return ErrRoutingPlanConflict
	}
	return nil
}

func (s *PostgreSQLRoutingPlanStore) ReleaseReservations(
	ctx context.Context, planID, actor string, now time.Time,
) error {
	if s == nil || s.DB == nil {
		return errors.New("database is required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := releaseRoutingReservations(ctx, tx, planID, "", actor, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgreSQLRoutingPlanStore) ReleaseReservationsInTx(
	ctx context.Context,
	tx *sql.Tx,
	planID string,
	campaignID string,
	actor string,
	now time.Time,
) error {
	if s == nil || s.DB == nil || tx == nil {
		return errors.New("database transaction is required")
	}
	return releaseRoutingReservations(ctx, tx, planID, campaignID, actor, now)
}

func (s *PostgreSQLRoutingPlanStore) PoolExecutionReport(ctx context.Context, planID string) ([]PoolExecutionReport, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT p.routing_plan_id::text,p.sender_pool_id::text,coalesce(p.gateway_pool_id::text,'') AS gateway_pool_id,p.provider,p.engine,
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
 count(cr.id) FILTER (WHERE cr.status IN ('SENT','DELIVERED','READ','FAILED_PERMANENT','UNKNOWN','SUPPRESSED_BEFORE_SEND','CANCELLED'))::bigint
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
	if maxMPM < 1 || daily < 1 || reservedMPM < 0 || reservedHourly < 0 || reservedDaily < 0 ||
		route.ReservedMessagesPerMinute < 1 || route.ReservedHourlyUnits < 1 || route.ReservedDailyUnits < route.ReservedHourlyUnits {
		return true
	}
	maxHourly := saturatingMultiply(int64(maxMPM), 60)
	if reservedMPM > maxMPM || route.ReservedMessagesPerMinute > maxMPM-reservedMPM {
		return true
	}
	if reservedHourly > maxHourly || route.ReservedHourlyUnits > maxHourly-reservedHourly {
		return true
	}
	return reservedDaily > daily || route.ReservedDailyUnits > daily-reservedDaily
}
