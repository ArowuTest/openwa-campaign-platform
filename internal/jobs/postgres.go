package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PostgreSQLRepository is the authoritative durable implementation. Claim uses
// FOR UPDATE SKIP LOCKED so multiple workers can safely compete without duplicate ownership.
type PostgreSQLRepository struct{ DB *sql.DB }

func (r *PostgreSQLRepository) Enqueue(ctx context.Context, job Job) (Job, bool, error) {
	if r.DB == nil {
		return Job{}, false, errors.New("database is required")
	}
	const query = `
INSERT INTO durable_jobs (
 id, job_type, deduplication_key, payload, status, priority, attempt_count,
 max_attempts, available_at, created_at, updated_at
) VALUES ($1,$2,$3,$4,'PENDING',$5,0,$6,$7,$8,$8)
ON CONFLICT (deduplication_key) DO NOTHING`
	result, err := r.DB.ExecContext(ctx, query, job.ID, job.Type, job.DedupKey, []byte(job.Payload), job.Priority, job.MaxAttempts, job.AvailableAt, job.CreatedAt)
	if err != nil {
		return Job{}, false, fmt.Errorf("insert durable job: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Job{}, false, err
	}
	if rows == 1 {
		return job, true, nil
	}
	existing, err := r.byDedup(ctx, job.DedupKey)
	if err != nil {
		return Job{}, false, err
	}
	if existing.Type != job.Type || !jsonEqual(existing.Payload, job.Payload) {
		return Job{}, false, ErrDedupMismatch
	}
	return existing, false, nil
}

func (r *PostgreSQLRepository) Claim(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int, types []string) ([]Job, error) {
	if r.DB == nil {
		return nil, errors.New("database is required")
	}
	if owner == "" || lease <= 0 {
		return nil, errors.New("owner and lease required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	const allTypesQuery = `
WITH candidates AS (
 SELECT id FROM durable_jobs
 WHERE available_at <= $1
   AND (status='PENDING' OR (status='PROCESSING' AND lease_expires_at <= $1))
 ORDER BY priority DESC, available_at, created_at
 FOR UPDATE SKIP LOCKED
 LIMIT $2
)
UPDATE durable_jobs j
SET status='PROCESSING', lease_owner=$3, lease_expires_at=$4,
    attempt_count=j.attempt_count+1, lease_version=j.lease_version+1, updated_at=$1
FROM candidates c
WHERE j.id=c.id
RETURNING j.id,j.job_type,j.deduplication_key,j.payload,j.status,j.priority,
 j.attempt_count,j.max_attempts,j.available_at,j.lease_owner,j.lease_expires_at,
 j.lease_version,j.last_error_code,j.last_error_detail,j.created_at,j.updated_at,j.completed_at`
	const oneTypeQuery = `
WITH candidates AS (
 SELECT id FROM durable_jobs
 WHERE job_type=$2 AND available_at <= $1
   AND (status='PENDING' OR (status='PROCESSING' AND lease_expires_at <= $1))
 ORDER BY priority DESC, available_at, created_at
 FOR UPDATE SKIP LOCKED
 LIMIT $3
)
UPDATE durable_jobs j
SET status='PROCESSING', lease_owner=$4, lease_expires_at=$5,
    attempt_count=j.attempt_count+1, lease_version=j.lease_version+1, updated_at=$1
FROM candidates c
WHERE j.id=c.id
RETURNING j.id,j.job_type,j.deduplication_key,j.payload,j.status,j.priority,
 j.attempt_count,j.max_attempts,j.available_at,j.lease_owner,j.lease_expires_at,
 j.lease_version,j.last_error_code,j.last_error_detail,j.created_at,j.updated_at,j.completed_at`
	const manyTypesQuery = `
WITH candidates AS (
 SELECT id FROM durable_jobs
 WHERE available_at <= $1
   AND (status='PENDING' OR (status='PROCESSING' AND lease_expires_at <= $1))
   AND job_type IN (SELECT jsonb_array_elements_text($2::jsonb))
 ORDER BY priority DESC, available_at, created_at
 FOR UPDATE SKIP LOCKED
 LIMIT $3
)
UPDATE durable_jobs j
SET status='PROCESSING', lease_owner=$4, lease_expires_at=$5,
    attempt_count=j.attempt_count+1, lease_version=j.lease_version+1, updated_at=$1
FROM candidates c
WHERE j.id=c.id
RETURNING j.id,j.job_type,j.deduplication_key,j.payload,j.status,j.priority,
 j.attempt_count,j.max_attempts,j.available_at,j.lease_owner,j.lease_expires_at,
 j.lease_version,j.last_error_code,j.last_error_detail,j.created_at,j.updated_at,j.completed_at`

	var rows *sql.Rows
	var err error
	expires := now.UTC().Add(lease)
	switch len(types) {
	case 0:
		rows, err = r.DB.QueryContext(ctx, allTypesQuery, now.UTC(), limit, owner, expires)
	case 1:
		jobType := strings.TrimSpace(types[0])
		if jobType == "" {
			return nil, errors.New("job type filter cannot be blank")
		}
		rows, err = r.DB.QueryContext(ctx, oneTypeQuery, now.UTC(), jobType, limit, owner, expires)
	default:
		cleaned := make([]string, 0, len(types))
		seen := make(map[string]struct{}, len(types))
		for _, value := range types {
			value = strings.TrimSpace(value)
			if value == "" {
				return nil, errors.New("job type filter cannot be blank")
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			cleaned = append(cleaned, value)
		}
		encodedTypes, marshalErr := json.Marshal(cleaned)
		if marshalErr != nil {
			return nil, fmt.Errorf("encode job type filter: %w", marshalErr)
		}
		rows, err = r.DB.QueryContext(ctx, manyTypesQuery, now.UTC(), string(encodedTypes), limit, owner, expires)
	}
	if err != nil {
		return nil, fmt.Errorf("claim durable jobs: %w", err)
	}
	defer rows.Close()
	items := make([]Job, 0, limit)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, job)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) Complete(ctx context.Context, jobID, owner string, leaseVersion int64, now time.Time) error {
	const query = `UPDATE durable_jobs SET status='COMPLETED',completed_at=$4,updated_at=$4,lease_owner=NULL,lease_expires_at=NULL WHERE id=$1 AND status='PROCESSING' AND lease_owner=$2 AND lease_version=$3 AND lease_expires_at>$4`
	return expectOne(ctx, r.DB, query, jobID, owner, leaseVersion, now.UTC())
}
func (r *PostgreSQLRepository) Fail(ctx context.Context, jobID, owner string, leaseVersion int64, now time.Time, retryable bool, retryAfter time.Duration, code, detail string) error {
	const query = `
UPDATE durable_jobs SET
 status=CASE WHEN $5 AND attempt_count<max_attempts THEN 'PENDING' ELSE 'DEAD_LETTER' END,
 available_at=CASE WHEN $5 AND attempt_count<max_attempts THEN $4+$6::interval ELSE available_at END,
 last_error_code=$7,last_error_detail=$8,updated_at=$4,lease_owner=NULL,lease_expires_at=NULL
WHERE id=$1 AND status='PROCESSING' AND lease_owner=$2 AND lease_version=$3 AND lease_expires_at>$4`
	return expectOne(ctx, r.DB, query, jobID, owner, leaseVersion, now.UTC(), retryable, intervalLiteral(retryAfter), clean(code), clean(detail))
}
func (r *PostgreSQLRepository) Renew(ctx context.Context, jobID, owner string, leaseVersion int64, now time.Time, lease time.Duration) error {
	const query = `UPDATE durable_jobs SET lease_expires_at=$5,updated_at=$4 WHERE id=$1 AND status='PROCESSING' AND lease_owner=$2 AND lease_version=$3 AND lease_expires_at>$4`
	return expectOne(ctx, r.DB, query, jobID, owner, leaseVersion, now.UTC(), now.UTC().Add(lease))
}
func (r *PostgreSQLRepository) Cancel(ctx context.Context, jobID string, now time.Time) error {
	if r.DB == nil {
		return errors.New("database is required")
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE durable_jobs SET status='CANCELLED',updated_at=$2 WHERE id=$1 AND status='PENDING'`, jobID, now.UTC())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}
	var status string
	if err := r.DB.QueryRowContext(ctx, `SELECT status FROM durable_jobs WHERE id=$1`, jobID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	switch Status(status) {
	case StatusCompleted, StatusDeadLetter, StatusCancelled:
		return nil
	case StatusProcessing:
		return ErrJobInProgress
	default:
		return ErrJobInProgress
	}
}
func (r *PostgreSQLRepository) Get(ctx context.Context, jobID string) (Job, error) {
	const query = `SELECT id,job_type,deduplication_key,payload,status,priority,attempt_count,max_attempts,available_at,lease_owner,lease_expires_at,lease_version,last_error_code,last_error_detail,created_at,updated_at,completed_at FROM durable_jobs WHERE id=$1`
	job, err := scanJob(r.DB.QueryRowContext(ctx, query, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return job, err
}
func (r *PostgreSQLRepository) PendingCount(ctx context.Context, types []string, now time.Time) (int, error) {
	query := `SELECT count(*) FROM durable_jobs WHERE status='PENDING' AND available_at <= $1`
	args := []any{now.UTC()}
	if len(types) > 0 {
		encodedTypes, marshalErr := json.Marshal(types)
		if marshalErr != nil {
			return 0, fmt.Errorf("encode job type filter: %w", marshalErr)
		}
		query += ` AND job_type IN (SELECT jsonb_array_elements_text($2::jsonb))`
		args = append(args, string(encodedTypes))
	}
	var count int
	err := r.DB.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}
func (r *PostgreSQLRepository) byDedup(ctx context.Context, key string) (Job, error) {
	const query = `SELECT id,job_type,deduplication_key,payload,status,priority,attempt_count,max_attempts,available_at,lease_owner,lease_expires_at,lease_version,last_error_code,last_error_detail,created_at,updated_at,completed_at FROM durable_jobs WHERE deduplication_key=$1`
	return scanJob(r.DB.QueryRowContext(ctx, query, key))
}

type scanner interface{ Scan(...any) error }

func scanJob(row scanner) (Job, error) {
	var job Job
	var payload []byte
	var status string
	var owner, code, detail sql.NullString
	var lease, completed sql.NullTime
	err := row.Scan(&job.ID, &job.Type, &job.DedupKey, &payload, &status, &job.Priority, &job.AttemptCount, &job.MaxAttempts, &job.AvailableAt, &owner, &lease, &job.LeaseVersion, &code, &detail, &job.CreatedAt, &job.UpdatedAt, &completed)
	if err != nil {
		return Job{}, err
	}
	if !json.Valid(payload) {
		return Job{}, errors.New("durable job payload is invalid JSON")
	}
	job.Payload = append([]byte(nil), payload...)
	job.Status = Status(status)
	if owner.Valid {
		job.LeaseOwner = owner.String
	}
	if lease.Valid {
		v := lease.Time
		job.LeaseExpiresAt = &v
	}
	if code.Valid {
		job.LastErrorCode = code.String
	}
	if detail.Valid {
		job.LastError = detail.String
	}
	if completed.Valid {
		v := completed.Time
		job.CompletedAt = &v
	}
	return job, nil
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
	if count == 0 {
		return ErrLeaseConflict
	}
	return nil
}
func intervalLiteral(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	return fmt.Sprintf("%f seconds", duration.Seconds())
}

func (r *PostgreSQLRepository) List(ctx context.Context, q Query) ([]Job, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	statuses, err := json.Marshal(q.Statuses)
	if err != nil {
		return nil, err
	}
	types, err := json.Marshal(q.Types)
	if err != nil {
		return nil, err
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id,job_type,deduplication_key,payload,status,priority,attempt_count,max_attempts,available_at,lease_owner,lease_expires_at,lease_version,last_error_code,last_error_detail,created_at,updated_at,completed_at
FROM durable_jobs
WHERE (jsonb_array_length($1::jsonb)=0 OR status IN (SELECT jsonb_array_elements_text($1::jsonb)))
  AND (jsonb_array_length($2::jsonb)=0 OR job_type IN (SELECT jsonb_array_elements_text($2::jsonb)))
ORDER BY created_at DESC,id DESC LIMIT $3`, string(statuses), string(types), q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Job, 0, q.Limit)
	for rows.Next() {
		v, e := scanJob(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) Summary(ctx context.Context, now time.Time) (QueueSummary, error) {
	if r == nil || r.DB == nil {
		return QueueSummary{}, errors.New("database is required")
	}
	out := QueueSummary{AsAt: now.UTC(), CountsByStatus: map[Status]int64{}, CountsByType: map[string]int64{}}
	rows, err := r.DB.QueryContext(ctx, `SELECT status,count(*) FROM durable_jobs GROUP BY status`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var s Status
		var c int64
		if err := rows.Scan(&s, &c); err != nil {
			rows.Close()
			return out, err
		}
		out.CountsByStatus[s] = c
	}
	if err := rows.Close(); err != nil {
		return out, err
	}
	rows, err = r.DB.QueryContext(ctx, `SELECT job_type,count(*) FROM durable_jobs GROUP BY job_type`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var t string
		var c int64
		if err := rows.Scan(&t, &c); err != nil {
			rows.Close()
			return out, err
		}
		out.CountsByType[t] = c
	}
	if err := rows.Close(); err != nil {
		return out, err
	}
	var oldest sql.NullTime
	err = r.DB.QueryRowContext(ctx, `SELECT min(created_at),count(*) FILTER(WHERE status='PROCESSING'),count(*) FILTER(WHERE status='PROCESSING' AND lease_expires_at<=$1) FROM durable_jobs WHERE status IN ('PENDING','PROCESSING')`, now.UTC()).Scan(&oldest, &out.ProcessingLeases, &out.ExpiredLeases)
	if err != nil {
		return out, err
	}
	if oldest.Valid {
		v := oldest.Time
		out.OldestPendingAt = &v
	}
	return out, nil
}

func (r *PostgreSQLRepository) RetryDeadLetter(ctx context.Context, id, actor, reason string, now time.Time) (Job, error) {
	if r == nil || r.DB == nil {
		return Job{}, errors.New("database is required")
	}
	row := r.DB.QueryRowContext(ctx, `WITH updated AS (
 UPDATE durable_jobs SET status='PENDING',attempt_count=0,available_at=$4,last_error_code=NULL,last_error_detail=NULL,completed_at=NULL,updated_at=$4
 WHERE id=$1::uuid AND status='DEAD_LETTER'
 RETURNING id,job_type,deduplication_key,payload,status,priority,attempt_count,max_attempts,available_at,lease_owner,lease_expires_at,lease_version,last_error_code,last_error_detail,created_at,updated_at,completed_at
), event AS (
 INSERT INTO durable_job_administration_events(job_id,action,actor_id,reason,previous_status,current_status,occurred_at)
 SELECT id,'RETRY_DEAD_LETTER',$2::uuid,$3,'DEAD_LETTER','PENDING',$4 FROM updated
)
SELECT * FROM updated`, id, actor, reason, now.UTC())
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, r.administrativeMiss(ctx, id)
	}
	return job, err
}
func (r *PostgreSQLRepository) CancelPending(ctx context.Context, id, actor, reason string, now time.Time) (Job, error) {
	if r == nil || r.DB == nil {
		return Job{}, errors.New("database is required")
	}
	row := r.DB.QueryRowContext(ctx, `WITH updated AS (
 UPDATE durable_jobs SET status='CANCELLED',updated_at=$4 WHERE id=$1::uuid AND status='PENDING'
 RETURNING id,job_type,deduplication_key,payload,status,priority,attempt_count,max_attempts,available_at,lease_owner,lease_expires_at,lease_version,last_error_code,last_error_detail,created_at,updated_at,completed_at
), event AS (
 INSERT INTO durable_job_administration_events(job_id,action,actor_id,reason,previous_status,current_status,occurred_at)
 SELECT id,'CANCEL_PENDING',$2::uuid,$3,'PENDING','CANCELLED',$4 FROM updated
)
SELECT * FROM updated`, id, actor, reason, now.UTC())
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, r.administrativeMiss(ctx, id)
	}
	return job, err
}
func (r *PostgreSQLRepository) administrativeMiss(ctx context.Context, id string) error {
	var status string
	err := r.DB.QueryRowContext(ctx, `SELECT status FROM durable_jobs WHERE id=$1::uuid`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrAdministrativeConflict
}
func (r *PostgreSQLRepository) Events(ctx context.Context, id string, limit int) ([]AdministrationEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT job_id::text,action,actor_id::text,reason,previous_status,current_status,occurred_at FROM durable_job_administration_events WHERE job_id=$1::uuid ORDER BY occurred_at DESC,id DESC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdministrationEvent
	for rows.Next() {
		var v AdministrationEvent
		if err := rows.Scan(&v.JobID, &v.Action, &v.ActorID, &v.Reason, &v.Previous, &v.Current, &v.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
