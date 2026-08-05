package inbound

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"campaign-platform/internal/shared/id"
)

type PostgreSQLGovernanceOperations struct {
	DB      *sql.DB
	Replies Repository
}

func (o *PostgreSQLGovernanceOperations) RetentionSweep(ctx context.Context, worker string, now time.Time) (int64, error) {
	if o == nil || o.DB == nil || o.Replies == nil {
		return 0, errors.New("database and reply repository are required")
	}
	worker = strings.TrimSpace(worker)
	if worker == "" {
		return 0, errors.New("worker is required")
	}
	runID, err := id.New()
	if err != nil {
		return 0, err
	}
	if _, err = o.DB.ExecContext(ctx, `INSERT INTO inbound_retention_sweep_runs(id,worker_id,started_at,status) VALUES($1::uuid,$2,$3,'RUNNING')`, runID, worker, now); err != nil {
		return 0, err
	}
	count, err := o.Replies.RedactExpired(ctx, now)
	if err != nil {
		_, _ = o.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE inbound_retention_sweep_runs SET status='FAILED',completed_at=$1,failure_reason=$2 WHERE id=$3::uuid`, time.Now().UTC(), safeGovernanceError(err), runID)
		return 0, err
	}
	_, err = o.DB.ExecContext(ctx, `UPDATE inbound_retention_sweep_runs SET status='COMPLETED',completed_at=$1,redacted_count=$2 WHERE id=$3::uuid`, time.Now().UTC(), count, runID)
	return count, err
}
func safeGovernanceError(err error) string {
	if err == nil {
		return "operation failed"
	}
	v := strings.TrimSpace(err.Error())
	if len(v) > 1000 {
		return v[:1000]
	}
	return v
}
