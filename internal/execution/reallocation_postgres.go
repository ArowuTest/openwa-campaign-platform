package execution

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/shared/id"
)

func (r *PostgreSQLShardRepository) ReallocateShard(ctx context.Context, shardID, targetPoolID, actor, packed string, now time.Time) (ShardReallocation, error) {
	if r == nil || r.DB == nil {
		return ShardReallocation{}, errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ShardReallocation{}, err
	}
	defer tx.Rollback()
	var e ShardReallocation
	var status string
	var terminal int64
	var leaseOwner sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT s.campaign_id::text,s.routing_plan_id::text,s.assigned_sender_pool_id::text,s.status,s.terminal_count,s.lease_owner,s.lease_version
FROM campaign_dispatch_shards s WHERE s.id=$1::uuid FOR UPDATE`, shardID).Scan(&e.CampaignID, &e.RoutingPlanID, &e.FromSenderPoolID, &status, &terminal, &leaseOwner, &e.PreviousLeaseVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ShardReallocation{}, ErrReallocationInvalid
	}
	if err != nil {
		return ShardReallocation{}, err
	}
	if status != string(ShardPending) || terminal != 0 || leaseOwner.Valid {
		return ShardReallocation{}, ErrReallocationUnsafe
	}
	if e.FromSenderPoolID == targetPoolID {
		return ShardReallocation{}, ErrReallocationConflict
	}
	var sourceOut, targetIn bool
	err = tx.QueryRowContext(ctx, `SELECT
 coalesce((SELECT allow_reallocation_out FROM campaign_routing_plan_pools WHERE routing_plan_id=$1::uuid AND sender_pool_id=$2::uuid),false),
 coalesce((SELECT allow_reallocation_in FROM campaign_routing_plan_pools WHERE routing_plan_id=$1::uuid AND sender_pool_id=$3::uuid),false)`, e.RoutingPlanID, e.FromSenderPoolID, targetPoolID).Scan(&sourceOut, &targetIn)
	if err != nil {
		return ShardReallocation{}, err
	}
	if !sourceOut || !targetIn {
		return ShardReallocation{}, ErrReallocationInvalid
	}
	var unsafe int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM campaign_recipients WHERE dispatch_shard_id=$1::uuid AND status NOT IN ('AUTHORISED','QUEUED','FAILED_RETRYABLE')`, shardID).Scan(&unsafe); err != nil {
		return ShardReallocation{}, err
	}
	if unsafe != 0 {
		return ShardReallocation{}, ErrReallocationUnsafe
	}
	parts := strings.SplitN(packed, "\x1f", 2)
	e.ID, err = id.New()
	if err != nil {
		return ShardReallocation{}, err
	}
	e.DispatchShardID = shardID
	e.ToSenderPoolID = targetPoolID
	e.ActorID = actor
	e.Reason = parts[0]
	if len(parts) == 2 {
		e.EvidenceReference = parts[1]
	}
	e.NewLeaseVersion = e.PreviousLeaseVersion + 1
	e.CreatedAt = now.UTC()
	res, err := tx.ExecContext(ctx, `UPDATE campaign_dispatch_shards SET assigned_sender_pool_id=$2::uuid,lease_version=lease_version+1,updated_at=$3 WHERE id=$1::uuid AND lease_version=$4`, shardID, targetPoolID, now.UTC(), e.PreviousLeaseVersion)
	if err != nil {
		return ShardReallocation{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return ShardReallocation{}, err
	}
	if n != 1 {
		return ShardReallocation{}, ErrReallocationConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO campaign_shard_reallocations(id,campaign_id,routing_plan_id,dispatch_shard_id,from_sender_pool_id,to_sender_pool_id,actor_id,reason,evidence_reference,previous_lease_version,new_lease_version,created_at)
VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,$8,$9,$10,$11,$12)`, e.ID, e.CampaignID, e.RoutingPlanID, e.DispatchShardID, e.FromSenderPoolID, e.ToSenderPoolID, e.ActorID, e.Reason, e.EvidenceReference, e.PreviousLeaseVersion, e.NewLeaseVersion, e.CreatedAt)
	if err != nil {
		return ShardReallocation{}, err
	}
	if err = tx.Commit(); err != nil {
		return ShardReallocation{}, err
	}
	return e, nil
}

func (r *PostgreSQLShardRepository) ListShardReallocationPage(ctx context.Context, shardID string, limit int, after *time.Time, afterID string) ([]ShardReallocation, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,campaign_id::text,routing_plan_id::text,dispatch_shard_id::text,from_sender_pool_id::text,to_sender_pool_id::text,actor_id::text,reason,evidence_reference,previous_lease_version,new_lease_version,created_at FROM campaign_shard_reallocations WHERE dispatch_shard_id=$1::uuid AND ($3::timestamptz IS NULL OR created_at>$3 OR (created_at=$3 AND id>NULLIF($4,'')::uuid)) ORDER BY created_at ASC,id ASC LIMIT $2`, shardID, limit, after, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShardReallocation{}
	for rows.Next() {
		var event ShardReallocation
		if err := rows.Scan(&event.ID, &event.CampaignID, &event.RoutingPlanID, &event.DispatchShardID, &event.FromSenderPoolID, &event.ToSenderPoolID, &event.ActorID, &event.Reason, &event.EvidenceReference, &event.PreviousLeaseVersion, &event.NewLeaseVersion, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (r *PostgreSQLShardRepository) ListShardReallocations(ctx context.Context, shardID string) ([]ShardReallocation, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,campaign_id::text,routing_plan_id::text,dispatch_shard_id::text,from_sender_pool_id::text,to_sender_pool_id::text,actor_id::text,reason,evidence_reference,previous_lease_version,new_lease_version,created_at FROM campaign_shard_reallocations WHERE dispatch_shard_id=$1::uuid ORDER BY created_at,id`, shardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShardReallocation{}
	for rows.Next() {
		var e ShardReallocation
		if err := rows.Scan(&e.ID, &e.CampaignID, &e.RoutingPlanID, &e.DispatchShardID, &e.FromSenderPoolID, &e.ToSenderPoolID, &e.ActorID, &e.Reason, &e.EvidenceReference, &e.PreviousLeaseVersion, &e.NewLeaseVersion, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
