package sender

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PostgreSQLAllocator struct {
	DB           *sql.DB
	HeartbeatTTL time.Duration
}

const postgresAllocationQuery = `
SELECT ss.id::text
FROM sender_sessions ss
JOIN sender_nodes sn ON sn.id=ss.node_id
JOIN sender_session_leases sl
  ON sl.session_id=ss.id
 AND sl.worker_node_id=ss.node_id
WHERE ss.logical_sender_pool=$2
  AND ss.status IN ('READY','BUSY')
  AND sn.status='READY'
  AND NOT sn.draining
  AND ss.last_heartbeat_at>$4
  AND sn.last_heartbeat_at>$4
  AND sl.expires_at>$3
  AND coalesce(ss.safe_messages_per_minute,0)>0
  AND coalesce(ss.safe_daily_capacity,0)>0
  AND (
    SELECT count(*)
    FROM campaign_recipients active
    WHERE active.assigned_session_id=ss.id
      AND active.status IN ('CLAIMED','SUBMITTING')
  ) < ss.in_flight_limit
ORDER BY md5(ss.id::text || $1), ss.id
LIMIT 1
FOR UPDATE OF ss SKIP LOCKED`

// Assign is idempotent. The campaign recipient row is locked, so concurrent
// workers cannot assign different sessions. Candidate ordering is deterministic
// for a recipient, distributing assignments without a mutable global counter.
// A session is eligible only when its node owns the current lease, both
// heartbeats are fresh and its measured capacity/in-flight bounds are positive.
func (a *PostgreSQLAllocator) Assign(ctx context.Context, recipientID, pool string, now time.Time) (string, error) {
	if a == nil || a.DB == nil {
		return "", errors.New("database is required")
	}
	if strings.TrimSpace(recipientID) == "" || strings.TrimSpace(pool) == "" {
		return "", errors.New("recipient and sender pool are required")
	}
	ttl := a.HeartbeatTTL
	if ttl <= 0 || ttl > 10*time.Minute {
		ttl = 90 * time.Second
	}
	tx, err := a.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var assigned sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT assigned_session_id::text FROM campaign_recipients WHERE id=$1::uuid FOR UPDATE`, recipientID).Scan(&assigned); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("campaign recipient not found")
		}
		return "", err
	}
	if assigned.Valid && assigned.String != "" {
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return assigned.String, nil
	}
	var sessionID string
	if err := tx.QueryRowContext(ctx, postgresAllocationQuery, recipientID, pool, now.UTC(), now.UTC().Add(-ttl)).Scan(&sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNoHealthySession
		}
		return "", fmt.Errorf("select sender session: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE campaign_recipients SET assigned_session_id=$2::uuid,updated_at=$3,version=version+1 WHERE id=$1::uuid AND assigned_session_id IS NULL`, recipientID, sessionID, now.UTC())
	if err != nil {
		return "", fmt.Errorf("assign sender session: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if count != 1 {
		return "", ErrAssignmentConflict
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return sessionID, nil
}
