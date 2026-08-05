package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (r *PostgreSQLMergeRepository) ClaimMergeReady(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]MergeWork, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || lease <= 0 {
		return nil, errors.New("owner and positive lease are required")
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := r.DB.QueryContext(ctx, `WITH candidates AS (
 SELECT id FROM audience_imports
 WHERE status IN('APPROVED','IMPORTING')
   AND coalesce(merge_next_attempt_at,'-infinity'::timestamptz) <= $1
   AND (merge_lease_owner IS NULL OR merge_lease_expires_at <= $1)
 ORDER BY approved_at NULLS LAST,created_at,id
 FOR UPDATE SKIP LOCKED LIMIT $2
)
UPDATE audience_imports ai SET status='IMPORTING',merge_lease_owner=$3,
 merge_lease_expires_at=$4,merge_lease_version=merge_lease_version+1,
 merge_attempt_count=merge_attempt_count+1,updated_at=$1
FROM candidates c WHERE ai.id=c.id
RETURNING ai.id::text,ai.merge_lease_owner,ai.merge_lease_version,ai.merge_lease_expires_at`, now.UTC(), limit, owner, now.UTC().Add(lease))
	if err != nil {
		return nil, fmt.Errorf("claim import merge: %w", err)
	}
	defer rows.Close()
	out := make([]MergeWork, 0, limit)
	for rows.Next() {
		var w MergeWork
		if err := rows.Scan(&w.ImportID, &w.Lease.Owner, &w.Lease.Version, &w.Lease.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
func (r *PostgreSQLMergeRepository) RenewMergeLease(ctx context.Context, w MergeWork, now time.Time, lease time.Duration) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE audience_imports SET merge_lease_expires_at=$5,updated_at=$4
WHERE id=$1::uuid AND status='IMPORTING' AND merge_lease_owner=$2 AND merge_lease_version=$3 AND merge_lease_expires_at>$4`, w.ImportID, w.Lease.Owner, w.Lease.Version, now.UTC(), now.UTC().Add(lease))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrImportConflict
	}
	return nil
}
func (r *PostgreSQLMergeRepository) ReleaseMergeLease(ctx context.Context, w MergeWork, now time.Time, cause error, retry time.Duration) error {
	if retry <= 0 {
		retry = 30 * time.Second
	}
	detail := "audience merge failed"
	if cause != nil {
		detail = strings.TrimSpace(cause.Error())
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE audience_imports SET status='APPROVED',merge_lease_owner=NULL,merge_lease_expires_at=NULL,
 merge_next_attempt_at=$5,merge_last_error=$6,updated_at=$4,version=version+1
WHERE id=$1::uuid AND status='IMPORTING' AND merge_lease_owner=$2 AND merge_lease_version=$3`, w.ImportID, w.Lease.Owner, w.Lease.Version, now.UTC(), now.UTC().Add(retry), detail)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrImportConflict
	}
	return nil
}

var _ MergeWorkRepository = (*PostgreSQLMergeRepository)(nil)
var _ = sql.ErrNoRows
