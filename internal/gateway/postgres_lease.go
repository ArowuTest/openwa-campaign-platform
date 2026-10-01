package gateway

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"
)

// SQLLeaseExecutor allows the lease and its governed session update to share
// one transaction instead of committing ownership before telemetry succeeds.
type SQLLeaseExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type PostgreSQLLeaseStore struct{ DB SQLLeaseExecutor }

func (s *PostgreSQLLeaseStore) Acquire(ctx context.Context, sessionID, workerID, token string, now time.Time, ttl time.Duration) (Lease, error) {
	if s.DB == nil {
		return Lease{}, errors.New("database is required")
	}
	if sessionID == "" || workerID == "" || token == "" || ttl <= 0 {
		return Lease{}, errors.New("session, worker, token and positive TTL are required")
	}
	tokenHash := sha256.Sum256([]byte(token))
	expires := now.UTC().Add(ttl)
	const query = `
INSERT INTO sender_session_leases(session_id,worker_node_id,lease_token_hash,version,acquired_at,renewed_at,expires_at)
VALUES($1,$2,$3,1,$4,$4,$5)
ON CONFLICT(session_id) DO UPDATE SET
 worker_node_id=EXCLUDED.worker_node_id,lease_token_hash=EXCLUDED.lease_token_hash,
 version=sender_session_leases.version+1,acquired_at=EXCLUDED.acquired_at,
 renewed_at=EXCLUDED.renewed_at,expires_at=EXCLUDED.expires_at
WHERE sender_session_leases.expires_at <= $4
   OR (sender_session_leases.worker_node_id=EXCLUDED.worker_node_id AND sender_session_leases.lease_token_hash=EXCLUDED.lease_token_hash)
RETURNING version,expires_at`
	lease := Lease{SessionID: sessionID, WorkerID: workerID, Token: token}
	err := s.DB.QueryRowContext(ctx, query, sessionID, workerID, tokenHash[:], now.UTC(), expires).Scan(&lease.Version, &lease.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Lease{}, ErrLeaseHeld
	}
	return lease, err
}
func (s *PostgreSQLLeaseStore) Renew(ctx context.Context, lease Lease, now time.Time, ttl time.Duration) (Lease, error) {
	if s.DB == nil {
		return Lease{}, errors.New("database is required")
	}
	if lease.SessionID == "" || lease.WorkerID == "" || lease.Token == "" || lease.Version <= 0 || ttl <= 0 {
		return Lease{}, errors.New("complete lease and positive TTL are required")
	}
	hash := sha256.Sum256([]byte(lease.Token))
	next := lease
	next.ExpiresAt = now.UTC().Add(ttl)
	const query = `UPDATE sender_session_leases SET version=version+1,renewed_at=$5,expires_at=$6 WHERE session_id=$1 AND worker_node_id=$2 AND lease_token_hash=$3 AND version=$4 AND expires_at>$5 RETURNING version,expires_at`
	err := s.DB.QueryRowContext(ctx, query, lease.SessionID, lease.WorkerID, hash[:], lease.Version, now.UTC(), next.ExpiresAt).Scan(&next.Version, &next.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Lease{}, ErrLeaseLost
	}
	return next, err
}
func (s *PostgreSQLLeaseStore) Release(ctx context.Context, lease Lease) error {
	if s.DB == nil {
		return errors.New("database is required")
	}
	hash := sha256.Sum256([]byte(lease.Token))
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sender_session_leases WHERE session_id=$1 AND worker_node_id=$2 AND lease_token_hash=$3 AND version=$4`, lease.SessionID, lease.WorkerID, hash[:], lease.Version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}
func (s *PostgreSQLLeaseStore) Validate(ctx context.Context, lease Lease, now time.Time) error {
	if s.DB == nil {
		return errors.New("database is required")
	}
	hash := sha256.Sum256([]byte(lease.Token))
	var exists bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sender_session_leases WHERE session_id=$1 AND worker_node_id=$2 AND lease_token_hash=$3 AND version=$4 AND expires_at>$5)`, lease.SessionID, lease.WorkerID, hash[:], lease.Version, now.UTC()).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrLeaseLost
	}
	return nil
}
func (s *PostgreSQLLeaseStore) Get(ctx context.Context, sessionID string) (Lease, bool, error) {
	if s.DB == nil {
		return Lease{}, false, errors.New("database is required")
	}
	var lease Lease
	lease.SessionID = sessionID
	err := s.DB.QueryRowContext(ctx, `SELECT worker_node_id,version,expires_at FROM sender_session_leases WHERE session_id=$1`, sessionID).Scan(&lease.WorkerID, &lease.Version, &lease.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Lease{}, false, nil
	}
	return lease, err == nil, err
}
