package inbound

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

type PostgreSQLRotationRepository struct {
	DB      *sql.DB
	Keyring *sharedcrypto.SecretKeyring
}

const rotationCols = `id::text,requested_by::text,target_key_version,status,requested_at,started_at,completed_at,updated_at,processed_count,failed_count,COALESCE(last_processed_id::text,''),COALESCE(failure_reason,''),COALESCE(lease_owner,''),lease_version,COALESCE(lease_expires_at,'epoch'::timestamptz)`
const rotationClaimColumns = `x.id::text,x.requested_by::text,x.target_key_version,x.status,x.requested_at,x.started_at,x.completed_at,x.updated_at,x.processed_count,x.failed_count,COALESCE(x.last_processed_id::text,''),COALESCE(x.failure_reason,''),COALESCE(x.lease_owner,''),x.lease_version,COALESCE(x.lease_expires_at,'epoch'::timestamptz)`

func scanRotation(s scanner) (RotationRun, error) {
	var r RotationRun
	err := s.Scan(&r.ID, &r.RequestedBy, &r.TargetKeyVersion, &r.Status, &r.RequestedAt, &r.StartedAt, &r.CompletedAt, &r.UpdatedAt, &r.ProcessedCount, &r.FailedCount, &r.LastProcessedID, &r.FailureReason, &r.LeaseOwner, &r.LeaseVersion, &r.LeaseExpiresAt)
	return r, err
}
func (r *PostgreSQLRotationRepository) Request(ctx context.Context, run RotationRun) (RotationRun, error) {
	return scanRotation(r.DB.QueryRowContext(ctx, `INSERT INTO inbound_content_reencryption_runs(id,requested_by,target_key_version,requested_at,updated_at,status,next_run_at) VALUES($1::uuid,$2::uuid,$3,$4,$4,'PENDING',$4) RETURNING `+rotationCols, run.ID, run.RequestedBy, run.TargetKeyVersion, run.RequestedAt))
}
func (r *PostgreSQLRotationRepository) GetRun(ctx context.Context, id string) (RotationRun, error) {
	v, err := scanRotation(r.DB.QueryRowContext(ctx, `SELECT `+rotationCols+` FROM inbound_content_reencryption_runs WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return RotationRun{}, ErrNotFound
	}
	return v, err
}
func (r *PostgreSQLRotationRepository) ListRuns(ctx context.Context, limit int) ([]RotationRun, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT `+rotationCols+` FROM inbound_content_reencryption_runs ORDER BY requested_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RotationRun{}
	for rows.Next() {
		v, e := scanRotation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgreSQLRotationRepository) ClaimRun(ctx context.Context, owner string, now time.Time, lease time.Duration) (RotationRun, error) {
	v, err := scanRotation(r.DB.QueryRowContext(ctx, `WITH c AS (SELECT id FROM inbound_content_reencryption_runs WHERE status IN('PENDING','RUNNING','FAILED') AND next_run_at<=$1 AND (lease_expires_at IS NULL OR lease_expires_at<=$1) ORDER BY requested_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE inbound_content_reencryption_runs x SET status='RUNNING',started_at=COALESCE(started_at,$1),updated_at=$1,lease_owner=$2,lease_expires_at=$3,lease_version=lease_version+1,failure_reason=NULL FROM c WHERE x.id=c.id RETURNING `+rotationClaimColumns, now, owner, now.Add(lease)))
	if errors.Is(err, sql.ErrNoRows) {
		return RotationRun{}, ErrNotFound
	}
	return v, err
}
func (r *PostgreSQLRotationRepository) RenewRun(ctx context.Context, run RotationRun, now time.Time, lease time.Duration) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE inbound_content_reencryption_runs SET lease_expires_at=$1,updated_at=$2 WHERE id=$3::uuid AND lease_owner=$4 AND lease_version=$5 AND status='RUNNING'`, now.Add(lease), now, run.ID, run.LeaseOwner, run.LeaseVersion)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRotationLeaseConflict
	}
	return nil
}
func (r *PostgreSQLRotationRepository) ProcessRunBatch(ctx context.Context, run RotationRun, limit int, now time.Time) (RotationRun, bool, error) {
	if r == nil || r.DB == nil || r.Keyring == nil {
		return run, false, errors.New("database and versioned keyring are required")
	}
	if run.TargetKeyVersion != r.Keyring.ActiveVersion() {
		return run, false, fmt.Errorf("rotation target %q is not the active key", run.TargetKeyVersion)
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return run, false, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id::text,message_text_cipher,content_key_version,version FROM inbound_replies WHERE content_redacted_at IS NULL AND message_text_cipher IS NOT NULL AND content_key_version<>$1 AND ($2='' OR id::text>$2) ORDER BY id LIMIT $3 FOR UPDATE SKIP LOCKED`, run.TargetKeyVersion, run.LastProcessedID, limit)
	if err != nil {
		return run, false, err
	}
	type item struct {
		id      string
		cipher  []byte
		key     string
		version int64
	}
	items := []item{}
	for rows.Next() {
		var i item
		if e := rows.Scan(&i.id, &i.cipher, &i.key, &i.version); e != nil {
			rows.Close()
			return run, false, e
		}
		items = append(items, i)
	}
	if e := rows.Close(); e != nil {
		return run, false, e
	}
	processed := int64(0)
	last := run.LastProcessedID
	for _, i := range items {
		plain, e := r.Keyring.Open(i.key, "inbound-reply:"+i.id, i.cipher)
		if e != nil {
			return run, false, e
		}
		cipher, key, e := r.Keyring.Seal("inbound-reply:"+i.id, plain)
		if e != nil {
			return run, false, e
		}
		res, e := tx.ExecContext(ctx, `UPDATE inbound_replies SET message_text_cipher=$1,content_key_version=$2,version=version+1 WHERE id=$3::uuid AND version=$4 AND content_key_version=$5`, cipher, key, i.id, i.version, i.key)
		if e != nil {
			return run, false, e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return run, false, e
		}
		processed += n
		last = i.id
	}
	done := len(items) < limit
	status := "RUNNING"
	var completed any = nil
	if done {
		status = "COMPLETED"
		completed = now
	}
	updated, err := scanRotation(tx.QueryRowContext(ctx, `UPDATE inbound_content_reencryption_runs SET processed_count=processed_count+$1,last_processed_id=NULLIF($2,'')::uuid,status=$3,completed_at=$4,updated_at=$5,lease_owner=CASE WHEN $3='COMPLETED' THEN NULL ELSE lease_owner END,lease_expires_at=CASE WHEN $3='COMPLETED' THEN NULL ELSE lease_expires_at END,next_run_at=$5 WHERE id=$6::uuid AND lease_owner=$7 AND lease_version=$8 RETURNING `+rotationCols, processed, last, status, completed, now, run.ID, run.LeaseOwner, run.LeaseVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return run, false, ErrRotationLeaseConflict
	}
	if err != nil {
		return run, false, err
	}
	if err = tx.Commit(); err != nil {
		return run, false, err
	}
	return updated, done, nil
}
func (r *PostgreSQLRotationRepository) FailRun(ctx context.Context, run RotationRun, now time.Time, cause error) error {
	detail := "rotation failed"
	if cause != nil {
		detail = strings.TrimSpace(cause.Error())
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE inbound_content_reencryption_runs SET status='FAILED',failed_count=failed_count+1,failure_reason=$1,updated_at=$2,next_run_at=$3,lease_owner=NULL,lease_expires_at=NULL WHERE id=$4::uuid AND lease_owner=$5 AND lease_version=$6`, detail, now, now.Add(time.Minute), run.ID, run.LeaseOwner, run.LeaseVersion)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRotationLeaseConflict
	}
	return nil
}
