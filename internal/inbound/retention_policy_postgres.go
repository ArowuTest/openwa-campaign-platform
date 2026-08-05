package inbound

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type PostgreSQLRetentionPolicyStore struct{ DB *sql.DB }

func (s *PostgreSQLRetentionPolicyStore) List(ctx context.Context) ([]RetentionPolicy, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,retention_days,status,effective_from,effective_to,version,created_by,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,created_at,updated_at FROM inbound_retention_policies ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RetentionPolicy
	for rows.Next() {
		p, err := scanRetentionPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *PostgreSQLRetentionPolicyStore) Get(ctx context.Context, id string) (RetentionPolicy, error) {
	p, err := scanRetentionPolicy(s.DB.QueryRowContext(ctx, `SELECT id,retention_days,status,effective_from,effective_to,version,created_by,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,created_at,updated_at FROM inbound_retention_policies WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrRetentionPolicyNotFound
	}
	return p, err
}
func (s *PostgreSQLRetentionPolicyStore) Active(ctx context.Context, at time.Time) (RetentionPolicy, error) {
	p, err := scanRetentionPolicy(s.DB.QueryRowContext(ctx, `SELECT id,retention_days,status,effective_from,effective_to,version,created_by,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,created_at,updated_at FROM inbound_retention_policies WHERE status='ACTIVE' AND effective_from<=$1 AND (effective_to IS NULL OR effective_to>$1) ORDER BY effective_from DESC LIMIT 1`, at))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrRetentionPolicyNotFound
	}
	return p, err
}
func (s *PostgreSQLRetentionPolicyStore) Create(ctx context.Context, p RetentionPolicy) (RetentionPolicy, error) {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO inbound_retention_policies(id,retention_days,status,effective_from,effective_to,version,created_by,reason,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7::uuid,$8,$9,$10)`, p.ID, p.RetentionDays, p.Status, p.EffectiveFrom, p.EffectiveTo, p.Version, p.CreatedBy, p.Reason, p.CreatedAt, p.UpdatedAt)
	return p, err
}
func (s *PostgreSQLRetentionPolicyStore) CompareAndSwap(ctx context.Context, p RetentionPolicy, expected int64) (RetentionPolicy, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if p.Status == RetentionPolicyActive {
		_, err = tx.ExecContext(ctx, `UPDATE inbound_retention_policies SET status='RETIRED',effective_to=$1,version=version+1,updated_at=$2 WHERE id<>$3 AND status='ACTIVE' AND (effective_to IS NULL OR effective_to>$1)`, p.EffectiveFrom, p.UpdatedAt, p.ID)
		if err != nil {
			return p, err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE inbound_retention_policies SET retention_days=$3,status=$4,effective_from=$5,effective_to=$6,version=$7,submitted_by=NULLIF($8,'')::uuid,approved_by=NULLIF($9,'')::uuid,reason=$10,updated_at=$11 WHERE id=$1 AND version=$2`, p.ID, expected, p.RetentionDays, p.Status, p.EffectiveFrom, p.EffectiveTo, p.Version, p.SubmittedBy, p.ApprovedBy, p.Reason, p.UpdatedAt)
	if err != nil {
		return p, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return p, err
	}
	if n != 1 {
		return p, ErrRetentionPolicyConflict
	}
	if err = tx.Commit(); err != nil {
		return p, err
	}
	return p, nil
}

type retentionScanner interface{ Scan(...any) error }

func scanRetentionPolicy(s retentionScanner) (RetentionPolicy, error) {
	var p RetentionPolicy
	err := s.Scan(&p.ID, &p.RetentionDays, &p.Status, &p.EffectiveFrom, &p.EffectiveTo, &p.Version, &p.CreatedBy, &p.SubmittedBy, &p.ApprovedBy, &p.Reason, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}
