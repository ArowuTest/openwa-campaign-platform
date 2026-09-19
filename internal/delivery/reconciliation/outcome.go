package reconciliation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type OutcomeCandidate struct {
	RecipientID string
	Status      string
	UpdatedAt   time.Time
}

type OutcomeRepository interface {
	ListStaleOutcomes(context.Context, time.Time, time.Time, int) ([]OutcomeCandidate, error)
	QuarantineStaleSubmitting(context.Context, string, time.Time, time.Time) error
	MarkFinalUnknown(context.Context, string, time.Time, time.Time) error
}

type PostgreSQLOutcomeRepository struct{ DB *sql.DB }

const maxOutcomeQueryLimit = 5000

func outcomeQueryLimit(limit int) (int, error) {
	if limit < 1 || limit > maxOutcomeQueryLimit {
		return 0, fmt.Errorf("outcome reconciliation limit must be between 1 and %d", maxOutcomeQueryLimit)
	}
	return limit, nil
}

func (r *PostgreSQLOutcomeRepository) ListStaleOutcomes(ctx context.Context, before, finalUnknownBefore time.Time, limit int) ([]OutcomeCandidate, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	limit, err := outcomeQueryLimit(limit)
	if err != nil {
		return nil, err
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,status,updated_at
		FROM campaign_recipients
		WHERE reconciliation_required=true
		  AND status IN ('SUBMITTING','GATEWAY_ACCEPTED','SENT','UNKNOWN')
		  AND NOT (status='UNKNOWN' AND last_error_code='FINAL_UNKNOWN')
		  AND updated_at <= $1
		ORDER BY CASE
			WHEN status='SUBMITTING' THEN 0
			WHEN status='UNKNOWN' AND updated_at <= $2 AND last_error_code IS DISTINCT FROM 'FINAL_UNKNOWN' THEN 0
			ELSE 1
		END,
		updated_at,id
		LIMIT $3`, before.UTC(), finalUnknownBefore.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OutcomeCandidate
	for rows.Next() {
		var v OutcomeCandidate
		if err := rows.Scan(&v.RecipientID, &v.Status, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *PostgreSQLOutcomeRepository) QuarantineStaleSubmitting(ctx context.Context, id string, expectedUpdatedAt, now time.Time) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE campaign_recipients SET status='UNKNOWN',last_error_code='OUTCOME_UNKNOWN',last_error_detail='submission outcome remained ambiguous through the configured reconciliation window',reconciliation_required=true,updated_at=$3,version=version+1 WHERE id=$1::uuid AND status='SUBMITTING' AND reconciliation_required=true AND updated_at=$2`, id, expectedUpdatedAt.UTC(), now.UTC())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *PostgreSQLOutcomeRepository) MarkFinalUnknown(ctx context.Context, id string, expectedUpdatedAt, now time.Time) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE campaign_recipients SET last_error_code='FINAL_UNKNOWN',last_error_detail='provider outcome remained ambiguous through the configured reconciliation window',updated_at=$3,version=version+1 WHERE id=$1::uuid AND status='UNKNOWN' AND reconciliation_required=true AND last_error_code IS DISTINCT FROM 'FINAL_UNKNOWN' AND updated_at=$2`, id, expectedUpdatedAt.UTC(), now.UTC())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type OutcomeWorker struct {
	Repository           OutcomeRepository
	PollInterval         time.Duration
	ReconciliationWindow time.Duration
	FinalUnknownWindow   time.Duration
	BatchSize            int
	Clock                func() time.Time
}

func (w *OutcomeWorker) RunOnce(ctx context.Context) (int, error) {
	if w == nil || w.Repository == nil {
		return 0, errors.New("outcome reconciliation repository is required")
	}
	if w.ReconciliationWindow <= 0 || w.FinalUnknownWindow < w.ReconciliationWindow {
		return 0, errors.New("reconciliation windows are invalid")
	}
	now := time.Now().UTC()
	if w.Clock != nil {
		now = w.Clock().UTC()
	}
	items, err := w.Repository.ListStaleOutcomes(ctx, now.Add(-w.ReconciliationWindow), now.Add(-w.FinalUnknownWindow), w.BatchSize)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, item := range items {
		if item.Status == "SUBMITTING" {
			err := w.Repository.QuarantineStaleSubmitting(ctx, item.RecipientID, item.UpdatedAt, now)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return processed, err
			}
			if err == nil {
				processed++
			}
			continue
		}
		if item.Status == "UNKNOWN" && !item.UpdatedAt.After(now.Add(-w.FinalUnknownWindow)) {
			err := w.Repository.MarkFinalUnknown(ctx, item.RecipientID, item.UpdatedAt, now)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return processed, err
			}
			if err == nil {
				processed++
			}
		}
	}
	return processed, nil
}
