package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"campaign-platform/internal/consent"
	"encoding/json"
)

type OptOutPolicyStore struct{ DB *sql.DB }

func (s *OptOutPolicyStore) List(ctx context.Context) ([]consent.GovernedOptOutPolicy, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,keywords,status,effective_from,effective_to,version,created_by,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,created_at,updated_at FROM opt_out_policies ORDER BY created_at DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []consent.GovernedOptOutPolicy
	for rows.Next() {
		p, err := scanOptOut(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *OptOutPolicyStore) Get(ctx context.Context, id string) (consent.GovernedOptOutPolicy, error) {
	p, err := scanOptOut(s.DB.QueryRowContext(ctx, `SELECT id,keywords,status,effective_from,effective_to,version,created_by,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,created_at,updated_at FROM opt_out_policies WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, consent.ErrOptOutPolicyNotFound
	}
	return p, err
}
func (s *OptOutPolicyStore) Active(ctx context.Context, at time.Time) (consent.GovernedOptOutPolicy, error) {
	p, err := scanOptOut(s.DB.QueryRowContext(ctx, `SELECT id,keywords,status,effective_from,effective_to,version,created_by,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,created_at,updated_at FROM opt_out_policies WHERE status='ACTIVE' AND effective_from<=$1 AND (effective_to IS NULL OR effective_to>$1) ORDER BY effective_from DESC LIMIT 1`, at))
	if errors.Is(err, sql.ErrNoRows) {
		return p, consent.ErrOptOutPolicyNotFound
	}
	return p, err
}
func (s *OptOutPolicyStore) Create(ctx context.Context, p consent.GovernedOptOutPolicy) (consent.GovernedOptOutPolicy, error) {
	keywords, err := json.Marshal(p.Keywords)
	if err != nil {
		return p, err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO opt_out_policies(id,keywords,status,effective_from,effective_to,version,created_by,submitted_by,approved_by,reason,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,NULL,NULL,$8,$9,$10)`, p.ID, keywords, p.Status, p.EffectiveFrom, p.EffectiveTo, p.Version, p.CreatedBy, p.Reason, p.CreatedAt, p.UpdatedAt)
	return p, err
}
func (s *OptOutPolicyStore) CompareAndSwap(ctx context.Context, p consent.GovernedOptOutPolicy, expected int64) (consent.GovernedOptOutPolicy, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if p.Status == consent.OptOutPolicyActive {
		_, err = tx.ExecContext(ctx, `UPDATE opt_out_policies SET status='RETIRED',effective_to=$1,version=version+1,updated_at=$2 WHERE id<>$3 AND status='ACTIVE' AND (effective_to IS NULL OR effective_to>$1)`, p.EffectiveFrom, p.UpdatedAt, p.ID)
		if err != nil {
			return p, err
		}
	}
	keywords, err := json.Marshal(p.Keywords)
	if err != nil {
		return p, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE opt_out_policies SET keywords=$1,status=$2,effective_from=$3,effective_to=$4,version=$5,submitted_by=NULLIF($6,''),approved_by=NULLIF($7,''),reason=$8,updated_at=$9 WHERE id=$10 AND version=$11`, keywords, p.Status, p.EffectiveFrom, p.EffectiveTo, p.Version, p.SubmittedBy, p.ApprovedBy, p.Reason, p.UpdatedAt, p.ID, expected)
	if err != nil {
		return p, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return p, err
	}
	if n != 1 {
		return p, consent.ErrOptOutPolicyConflict
	}
	if err = tx.Commit(); err != nil {
		return p, err
	}
	return p, nil
}

type optOutScanner interface{ Scan(...any) error }

func scanOptOut(r optOutScanner) (consent.GovernedOptOutPolicy, error) {
	var p consent.GovernedOptOutPolicy
	var to sql.NullTime
	var keywords []byte
	err := r.Scan(&p.ID, &keywords, &p.Status, &p.EffectiveFrom, &to, &p.Version, &p.CreatedBy, &p.SubmittedBy, &p.ApprovedBy, &p.Reason, &p.CreatedAt, &p.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(keywords, &p.Keywords)
	}
	if to.Valid {
		p.EffectiveTo = &to.Time
	}
	return p, err
}
