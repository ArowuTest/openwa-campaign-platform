package outbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type PostgreSQLRepository struct{ DB *sql.DB }

func (r *PostgreSQLRepository) Claim(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]Record, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if strings.TrimSpace(owner) == "" || lease <= 0 {
		return nil, errors.New("lease owner and positive duration are required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `WITH candidates AS(SELECT id FROM transactional_outbox WHERE available_at<=$1 AND (status='PENDING' OR(status='PROCESSING' AND lease_expires_at<=$1)) ORDER BY available_at,created_at FOR UPDATE SKIP LOCKED LIMIT $2) UPDATE transactional_outbox o SET status='PROCESSING',lease_owner=$3,lease_expires_at=$4,attempt_count=o.attempt_count+1,lease_version=o.lease_version+1,updated_at=$1 FROM candidates c WHERE o.id=c.id RETURNING o.id::text,o.deduplication_key,o.aggregate_type,o.aggregate_id::text,o.event_type,o.payload,o.status,o.available_at,o.attempt_count,o.max_attempts,coalesce(o.lease_owner,''),o.lease_expires_at,o.lease_version,o.created_at,o.updated_at,o.published_at,coalesce(o.last_error_code,''),coalesce(o.last_error_detail,'')`, now.UTC(), limit, owner, now.UTC().Add(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Record{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *PostgreSQLRepository) Complete(ctx context.Context, id, owner string, leaseVersion int64, now time.Time) error {
	return expectOne(ctx, r.DB, `UPDATE transactional_outbox SET status='PUBLISHED',published_at=$4,updated_at=$4,lease_owner=NULL,lease_expires_at=NULL WHERE id=$1::uuid AND status='PROCESSING' AND lease_owner=$2 AND lease_version=$3 AND lease_expires_at>$4`, id, owner, leaseVersion, now.UTC())
}
func (r *PostgreSQLRepository) Fail(ctx context.Context, id, owner string, leaseVersion int64, now time.Time, retryable bool, retryAfter time.Duration, code, detail string) error {
	return expectOne(ctx, r.DB, `UPDATE transactional_outbox SET
 status=CASE WHEN $5 AND attempt_count < max_attempts THEN 'PENDING' ELSE 'FAILED' END,
 available_at=CASE WHEN $5 AND attempt_count < max_attempts THEN $4+$6::interval ELSE available_at END,
 lease_owner=NULL,lease_expires_at=NULL,last_error_code=$7,last_error_detail=$8,updated_at=$4
 WHERE id=$1::uuid AND status='PROCESSING' AND lease_owner=$2 AND lease_version=$3 AND lease_expires_at>$4`, id, owner, leaseVersion, now.UTC(), retryable, fmt.Sprintf("%f seconds", retryAfter.Seconds()), code, detail)
}
func (r *PostgreSQLRepository) Renew(ctx context.Context, id, owner string, leaseVersion int64, now time.Time, lease time.Duration) error {
	return expectOne(ctx, r.DB, `UPDATE transactional_outbox SET lease_expires_at=$5,updated_at=$4 WHERE id=$1::uuid AND status='PROCESSING' AND lease_owner=$2 AND lease_version=$3 AND lease_expires_at>$4`, id, owner, leaseVersion, now.UTC(), now.UTC().Add(lease))
}
func (r *PostgreSQLRepository) Get(ctx context.Context, id string) (Record, error) {
	if r == nil || r.DB == nil {
		return Record{}, errors.New("database is required")
	}
	v, err := scan(r.DB.QueryRowContext(ctx, `SELECT id::text,deduplication_key,aggregate_type,aggregate_id::text,event_type,payload,status,available_at,attempt_count,max_attempts,coalesce(lease_owner,''),lease_expires_at,lease_version,created_at,updated_at,published_at,coalesce(last_error_code,''),coalesce(last_error_detail,'') FROM transactional_outbox WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	return v, err
}

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Record, error) {
	var v Record
	var status string
	var lease, published sql.NullTime
	err := row.Scan(&v.ID, &v.DedupKey, &v.AggregateType, &v.AggregateID, &v.EventType, &v.Payload, &status, &v.AvailableAt, &v.AttemptCount, &v.MaxAttempts, &v.LeaseOwner, &lease, &v.LeaseVersion, &v.CreatedAt, &v.UpdatedAt, &published, &v.LastErrorCode, &v.LastError)
	if err != nil {
		return Record{}, err
	}
	v.Status = Status(status)
	if lease.Valid {
		x := lease.Time
		v.LeaseExpiresAt = &x
	}
	if published.Valid {
		x := published.Time
		v.PublishedAt = &x
	}
	return v, nil
}
func expectOne(ctx context.Context, db *sql.DB, q string, args ...any) error {
	if db == nil {
		return errors.New("database is required")
	}
	result, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLeaseConflict
	}
	return nil
}
