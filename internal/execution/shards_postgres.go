package execution

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PostgreSQLShardRepository struct{ DB *sql.DB }

func (r *PostgreSQLShardRepository) Discover(ctx context.Context, targetSize int, now time.Time, limit int) (int, error) {
	if r == nil || r.DB == nil {
		return 0, errors.New("database is required")
	}
	if targetSize <= 0 || targetSize > 100000 {
		return 0, errors.New("target shard size must be between 1 and 100000")
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT c.id::text FROM campaigns c
WHERE c.status IN ('SCHEDULED','SENDING','PAUSED')
  AND EXISTS (SELECT 1 FROM campaign_recipients cr WHERE cr.campaign_id=c.id)
  AND NOT EXISTS (SELECT 1 FROM campaign_dispatch_shards s WHERE s.campaign_id=c.id)
ORDER BY c.requested_start_at NULLS LAST,c.id
FOR UPDATE SKIP LOCKED LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	created := 0
	for _, campaignID := range ids {
		result, err := tx.ExecContext(ctx, `WITH ranked AS (
 SELECT cr.id, ((row_number() OVER (ORDER BY cr.id)-1)/$2)::int AS ordinal
 FROM campaign_recipients cr WHERE cr.campaign_id=$1::uuid
), inserted AS (
 INSERT INTO campaign_dispatch_shards(campaign_id,ordinal,target_size,recipient_count,status,created_at,updated_at)
 SELECT $1::uuid,ordinal,$2,count(*)::bigint,'PENDING',$3,$3 FROM ranked GROUP BY ordinal
 ON CONFLICT(campaign_id,ordinal) DO NOTHING RETURNING id,ordinal
)
UPDATE campaign_recipients cr SET dispatch_shard_id=i.id
FROM ranked r JOIN inserted i ON i.ordinal=r.ordinal
WHERE cr.id=r.id AND cr.dispatch_shard_id IS NULL`, campaignID, targetSize, now.UTC())
		if err != nil {
			return 0, fmt.Errorf("create shards for campaign %s: %w", campaignID, err)
		}
		if n, e := result.RowsAffected(); e == nil && n > 0 {
			created++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return created, nil
}

func (r *PostgreSQLShardRepository) Claim(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]DispatchShard, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || lease <= 0 {
		return nil, errors.New("owner and positive lease are required")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.DB.QueryContext(ctx, `WITH candidates AS (
 SELECT id FROM campaign_dispatch_shards
 WHERE status IN ('PENDING','RUNNING') AND (status='PENDING' OR lease_expires_at<=$1)
 ORDER BY campaign_id,ordinal FOR UPDATE SKIP LOCKED LIMIT $2
)
UPDATE campaign_dispatch_shards s SET status='RUNNING',lease_owner=$3,lease_expires_at=$4,
 lease_version=s.lease_version+1,started_at=coalesce(s.started_at,$1),updated_at=$1
FROM candidates c WHERE s.id=c.id
RETURNING s.id::text,s.campaign_id::text,s.ordinal,s.target_size,s.recipient_count,s.terminal_count,s.failed_count,s.unknown_count,s.status,s.last_recipient_id::text,s.lease_owner,s.lease_version,s.lease_expires_at,s.started_at,s.completed_at,s.created_at,s.updated_at`, now.UTC(), limit, owner, now.UTC().Add(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DispatchShard, 0, limit)
	for rows.Next() {
		v, err := scanShard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *PostgreSQLShardRepository) Renew(ctx context.Context, shard DispatchShard, now time.Time, lease time.Duration) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE campaign_dispatch_shards SET lease_expires_at=$5,updated_at=$4
WHERE id=$1::uuid AND status='RUNNING' AND lease_owner=$2 AND lease_version=$3 AND lease_expires_at>$4`, shard.ID, shard.LeaseOwner, shard.LeaseVersion, now.UTC(), now.UTC().Add(lease))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrShardLeaseConflict
	}
	return nil
}

func (r *PostgreSQLShardRepository) Refresh(ctx context.Context, shard DispatchShard, now time.Time, next time.Duration) (DispatchShard, error) {
	if next <= 0 {
		next = 5 * time.Second
	}
	row := r.DB.QueryRowContext(ctx, `WITH counts AS (
 SELECT count(*)::bigint AS total,
 count(*) FILTER (WHERE status IN ('GATEWAY_ACCEPTED','SENT','DELIVERED','READ','FAILED_PERMANENT','UNKNOWN','SUPPRESSED_BEFORE_SEND','CANCELLED'))::bigint AS terminal,
 count(*) FILTER (WHERE status='FAILED_PERMANENT')::bigint AS failed,
 count(*) FILTER (WHERE status='UNKNOWN')::bigint AS unknown,
 max(id)::text AS last_id
 FROM campaign_recipients WHERE dispatch_shard_id=$1::uuid
), updated AS (
 UPDATE campaign_dispatch_shards s SET recipient_count=c.total,terminal_count=c.terminal,failed_count=c.failed,unknown_count=c.unknown,
 last_recipient_id=NULLIF(c.last_id,'')::uuid,
 status=CASE WHEN c.total>0 AND c.terminal=c.total THEN CASE WHEN c.failed>0 OR c.unknown>0 THEN 'COMPLETED_WITH_EXCEPTIONS' ELSE 'COMPLETED' END ELSE 'PENDING' END,
 completed_at=CASE WHEN c.total>0 AND c.terminal=c.total THEN $4 ELSE NULL END,
 lease_owner=NULL,lease_expires_at=NULL,updated_at=$4
 FROM counts c WHERE s.id=$1::uuid AND s.status='RUNNING' AND s.lease_owner=$2 AND s.lease_version=$3 AND s.lease_expires_at>$4
 RETURNING s.id::text,s.campaign_id::text,s.ordinal,s.target_size,s.recipient_count,s.terminal_count,s.failed_count,s.unknown_count,s.status,s.last_recipient_id::text,s.lease_owner,s.lease_version,s.lease_expires_at,s.started_at,s.completed_at,s.created_at,s.updated_at
) SELECT * FROM updated`, shard.ID, shard.LeaseOwner, shard.LeaseVersion, now.UTC())
	out, err := scanShard(row)
	if errors.Is(err, sql.ErrNoRows) {
		return DispatchShard{}, ErrShardLeaseConflict
	}
	return out, err
}

type shardScanner interface{ Scan(...any) error }

func scanShard(row shardScanner) (DispatchShard, error) {
	var s DispatchShard
	var status string
	var last, owner sql.NullString
	var lease, started, completed sql.NullTime
	err := row.Scan(&s.ID, &s.CampaignID, &s.Ordinal, &s.TargetSize, &s.RecipientCount, &s.TerminalCount, &s.FailedCount, &s.UnknownCount, &status, &last, &owner, &s.LeaseVersion, &lease, &started, &completed, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return DispatchShard{}, err
	}
	s.Status = ShardStatus(status)
	if last.Valid {
		s.LastRecipientID = last.String
	}
	if owner.Valid {
		s.LeaseOwner = owner.String
	}
	if lease.Valid {
		v := lease.Time
		s.LeaseExpiresAt = &v
	}
	if started.Valid {
		v := started.Time
		s.StartedAt = &v
	}
	if completed.Valid {
		v := completed.Time
		s.CompletedAt = &v
	}
	return s, nil
}
