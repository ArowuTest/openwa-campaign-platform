package reconciliation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/delivery"
)

type PostgreSQLRepository struct{ DB *sql.DB }

func (r *PostgreSQLRepository) Claim(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]Work, error) {
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
	// Discovery is idempotent and means queue-notification loss cannot strand a
	// campaign. Drafts without a frozen audience are intentionally excluded.
	if _, err := r.DB.ExecContext(ctx, `
INSERT INTO campaign_metric_reconciliations(campaign_id,status,next_run_at,created_at,updated_at)
SELECT id,'IDLE',$1,$1,$1 FROM campaigns
WHERE audience_snapshot_id IS NOT NULL
ON CONFLICT(campaign_id) DO NOTHING`, now.UTC()); err != nil {
		return nil, fmt.Errorf("discover campaign metric reconciliation: %w", err)
	}
	rows, err := r.DB.QueryContext(ctx, `WITH candidates AS (
  SELECT campaign_id FROM campaign_metric_reconciliations
  WHERE next_run_at<=$1
    AND (status<>'PROCESSING' OR lease_expires_at<=$1)
  ORDER BY next_run_at,campaign_id
  FOR UPDATE SKIP LOCKED
  LIMIT $2
)
UPDATE campaign_metric_reconciliations r
SET status='PROCESSING',lease_owner=$3,lease_expires_at=$4,
    lease_version=r.lease_version+1,updated_at=$1,last_error=NULL
FROM candidates c WHERE r.campaign_id=c.campaign_id
RETURNING r.campaign_id::text,r.lease_owner,r.lease_version,r.lease_expires_at`, now.UTC(), limit, owner, now.UTC().Add(lease))
	if err != nil {
		return nil, fmt.Errorf("claim campaign metric reconciliation: %w", err)
	}
	defer rows.Close()
	items := make([]Work, 0, limit)
	for rows.Next() {
		var work Work
		if err := rows.Scan(&work.CampaignID, &work.Lease.Owner, &work.Lease.Version, &work.Lease.ExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, work)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) Renew(ctx context.Context, work Work, now time.Time, lease time.Duration) error {
	return expectOne(ctx, r.DB, `UPDATE campaign_metric_reconciliations
SET lease_expires_at=$5,updated_at=$4
WHERE campaign_id=$1::uuid AND status='PROCESSING' AND lease_owner=$2
  AND lease_version=$3 AND lease_expires_at>$4`, work.CampaignID, work.Lease.Owner, work.Lease.Version, now.UTC(), now.UTC().Add(lease))
}

func (r *PostgreSQLRepository) Record(ctx context.Context, work Work, observation Observation, now time.Time, next time.Duration) error {
	if next <= 0 {
		next = time.Minute
	}
	canonical, err := json.Marshal(observation.Canonical)
	if err != nil {
		return err
	}
	stored, err := json.Marshal(observation.Stored)
	if err != nil {
		return err
	}
	status := "DRIFT"
	if observation.Matched {
		status = "MATCH"
	}
	return expectOne(ctx, r.DB, `UPDATE campaign_metric_reconciliations
SET status=$5,next_run_at=$4+$6::interval,lease_owner=NULL,lease_expires_at=NULL,
    last_checked_at=$4,last_canonical=$7::jsonb,last_stored=$8::jsonb,
    consecutive_drift_count=CASE WHEN $5='MATCH' THEN 0 ELSE consecutive_drift_count+1 END,
    updated_at=$4,last_error=NULL
WHERE campaign_id=$1::uuid AND status='PROCESSING' AND lease_owner=$2
  AND lease_version=$3 AND lease_expires_at>$4`, work.CampaignID, work.Lease.Owner, work.Lease.Version, now.UTC(), status, interval(next), string(canonical), string(stored))
}

func (r *PostgreSQLRepository) Fail(ctx context.Context, work Work, now time.Time, cause error, retry time.Duration) error {
	if retry <= 0 {
		retry = time.Minute
	}
	detail := "metric reconciliation failed"
	if cause != nil {
		detail = safe(cause.Error(), 1000)
	}
	return expectOne(ctx, r.DB, `UPDATE campaign_metric_reconciliations
SET status='FAILED',next_run_at=$4+$5::interval,lease_owner=NULL,lease_expires_at=NULL,
    last_error=$6,updated_at=$4
WHERE campaign_id=$1::uuid AND status='PROCESSING' AND lease_owner=$2
  AND lease_version=$3 AND lease_expires_at>$4`, work.CampaignID, work.Lease.Owner, work.Lease.Version, now.UTC(), interval(retry), detail)
}

func expectOne(ctx context.Context, db *sql.DB, query string, args ...any) error {
	if db == nil {
		return errors.New("database is required")
	}
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrLeaseConflict
	}
	return nil
}

func interval(value time.Duration) string { return fmt.Sprintf("%f seconds", value.Seconds()) }
func safe(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}

type PostgreSQLCalculator struct{ DB *sql.DB }

func (c *PostgreSQLCalculator) Observe(ctx context.Context, campaignID string, observedAt time.Time) (Observation, error) {
	if c == nil || c.DB == nil {
		return Observation{}, errors.New("database is required")
	}
	const query = `WITH canonical AS (
 SELECT
  count(*) FILTER (WHERE status='AUTHORISED')::bigint AS authorised,
  count(*) FILTER (WHERE status IN ('QUEUED','CLAIMED'))::bigint AS queued,
  count(*) FILTER (WHERE status IN ('SUBMITTING','GATEWAY_ACCEPTED'))::bigint AS submitted,
  count(*) FILTER (WHERE status='SENT')::bigint AS sent,
  count(*) FILTER (WHERE status='DELIVERED')::bigint AS delivered,
  count(*) FILTER (WHERE status='READ')::bigint AS read,
  count(*) FILTER (WHERE status IN ('FAILED_RETRYABLE','FAILED_PERMANENT'))::bigint AS failed,
  count(*) FILTER (WHERE status='UNKNOWN')::bigint AS unknown,
  count(*) FILTER (WHERE status='SUPPRESSED_BEFORE_SEND')::bigint AS suppressed
 FROM campaign_recipients WHERE campaign_id=$1::uuid
), excluded AS (
 SELECT count(*)::bigint AS total FROM campaign_release_exclusions WHERE campaign_id=$1::uuid
), stored AS (
 SELECT authorised_total,queued_total,submitted_total,sent_total,delivered_total,
        read_total,failed_total,unknown_total,suppressed_total,excluded_final_check_total
 FROM campaign_metrics WHERE campaign_id=$1::uuid
)
SELECT coalesce(canonical.authorised,0),coalesce(canonical.queued,0),coalesce(canonical.submitted,0),
       coalesce(canonical.sent,0),coalesce(canonical.delivered,0),coalesce(canonical.read,0),
       coalesce(canonical.failed,0),coalesce(canonical.unknown,0),coalesce(canonical.suppressed,0),
       coalesce(excluded.total,0),
       coalesce(stored.authorised_total,0),coalesce(stored.queued_total,0),coalesce(stored.submitted_total,0),
       coalesce(stored.sent_total,0),coalesce(stored.delivered_total,0),coalesce(stored.read_total,0),
       coalesce(stored.failed_total,0),coalesce(stored.unknown_total,0),coalesce(stored.suppressed_total,0),
       coalesce(stored.excluded_final_check_total,0)
FROM canonical CROSS JOIN excluded LEFT JOIN stored ON true`
	result := Observation{CampaignID: campaignID, ObservedAt: observedAt.UTC()}
	err := c.DB.QueryRowContext(ctx, query, campaignID).Scan(
		&result.Canonical.AuthorisedTotal, &result.Canonical.QueuedTotal, &result.Canonical.SubmittedTotal,
		&result.Canonical.SentTotal, &result.Canonical.DeliveredTotal, &result.Canonical.ReadTotal,
		&result.Canonical.FailedTotal, &result.Canonical.UnknownTotal, &result.Canonical.SuppressedTotal,
		&result.Canonical.ExcludedFinalCheckTotal,
		&result.Stored.AuthorisedTotal, &result.Stored.QueuedTotal, &result.Stored.SubmittedTotal,
		&result.Stored.SentTotal, &result.Stored.DeliveredTotal, &result.Stored.ReadTotal,
		&result.Stored.FailedTotal, &result.Stored.UnknownTotal, &result.Stored.SuppressedTotal,
		&result.Stored.ExcludedFinalCheckTotal,
	)
	if err != nil {
		return Observation{}, fmt.Errorf("observe campaign metrics: %w", err)
	}
	result.Matched = result.Canonical == result.Stored
	result.CanonicalTotal = result.Canonical.TotalAccounted()
	result.StoredTotal = result.Stored.TotalAccounted()
	return result, nil
}

var _ = delivery.Metrics{}
