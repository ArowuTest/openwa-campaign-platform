package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type PostgreSQLQueueRepairRepository struct{ DB *sql.DB }

// RepairMissing recreates only dispatch jobs whose authoritative outbox event
// was already published but whose durable job is absent. The original outbox
// deduplication key is reused, so concurrent repair and normal publication are
// protected by the durable_jobs unique constraint.
func (r *PostgreSQLQueueRepairRepository) RepairMissing(ctx context.Context, now time.Time, limit int) (int, error) {
	if r == nil || r.DB == nil {
		return 0, errors.New("database is required")
	}
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	result, err := r.DB.ExecContext(ctx, `WITH missing AS (
 SELECT o.deduplication_key,o.payload,o.available_at,o.created_at
 FROM transactional_outbox o
 JOIN campaign_recipients cr ON cr.id=o.aggregate_id
 LEFT JOIN durable_jobs j ON j.deduplication_key=o.deduplication_key
 WHERE o.event_type='CAMPAIGN_RECIPIENT_AUTHORISED' AND o.status='PUBLISHED' AND j.id IS NULL
   AND cr.status IN ('AUTHORISED','QUEUED','FAILED_RETRYABLE')
 ORDER BY o.published_at,o.id LIMIT $1
)
INSERT INTO durable_jobs(job_type,deduplication_key,payload,status,priority,attempt_count,max_attempts,available_at,created_at,updated_at)
SELECT 'DISPATCH_CAMPAIGN_RECIPIENT',m.deduplication_key,
 jsonb_build_object('campaignRecipientId',m.payload->>'campaignRecipientId'),'PENDING',0,0,8,greatest(m.available_at,$2),m.created_at,$2
FROM missing m ON CONFLICT(deduplication_key) DO NOTHING`, limit, now.UTC())
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}
