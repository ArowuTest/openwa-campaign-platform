package consent

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type PostgreSQLOptOutMetricRecorder struct{ DB *sql.DB }

type OptOutMetricExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func RecordOptOutMetric(ctx context.Context, exec OptOutMetricExecer, campaignID, suppressionID string, now time.Time) error {
	if exec == nil {
		return errors.New("database executor is required")
	}
	_, err := exec.ExecContext(ctx, `INSERT INTO campaign_opt_out_metric_events(suppression_id,campaign_id,recorded_at)
VALUES($1::uuid,$2::uuid,$3)
ON CONFLICT(suppression_id) DO NOTHING`, suppressionID, campaignID, now.UTC())
	return err
}

func (r *PostgreSQLOptOutMetricRecorder) RecordOptOut(ctx context.Context, campaignID, suppressionID string, now time.Time) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	return RecordOptOutMetric(ctx, r.DB, campaignID, suppressionID, now)
}
