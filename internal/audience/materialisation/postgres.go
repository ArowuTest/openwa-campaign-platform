package materialisation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/segment"
)

type PostgreSQLRepository struct{ DB *sql.DB }

const materialisationColumns = `id::text,campaign_id::text,coalesce(segment_id::text,''),segment_definition,definition_version,eligibility_context,consent_policy_version,configuration_version,requested_by::text,request_fingerprint,status,processed_count,expected_count,coalesce(last_contact_id::text,''),rolling_hash,coalesce(snapshot_id::text,''),attempt_count,next_attempt_at,coalesce(failure_code,''),coalesce(failure_reference,''),coalesce(lease_owner,''),lease_token,lease_expires_at,version,requested_at,started_at,completed_at,coalesce(cancelled_by::text,''),coalesce(cancellation_reason,''),updated_at`

const materialisationSelect = `SELECT ` + materialisationColumns + ` FROM audience_materialisation_jobs`

const materialisationReturningColumns = `j.id::text,j.campaign_id::text,coalesce(j.segment_id::text,''),j.segment_definition,j.definition_version,j.eligibility_context,j.consent_policy_version,j.configuration_version,j.requested_by::text,j.request_fingerprint,j.status,j.processed_count,j.expected_count,coalesce(j.last_contact_id::text,''),j.rolling_hash,coalesce(j.snapshot_id::text,''),j.attempt_count,j.next_attempt_at,coalesce(j.failure_code,''),coalesce(j.failure_reference,''),coalesce(j.lease_owner,''),j.lease_token,j.lease_expires_at,j.version,j.requested_at,j.started_at,j.completed_at,coalesce(j.cancelled_by::text,''),coalesce(j.cancellation_reason,''),j.updated_at`

func (r *PostgreSQLRepository) Create(ctx context.Context, job MaterialisationJob) (MaterialisationJob, error) {
	if r == nil || r.DB == nil {
		return MaterialisationJob{}, errors.New("database is required")
	}
	definition, err := json.Marshal(job.Definition)
	if err != nil {
		return MaterialisationJob{}, err
	}
	eligibility, err := json.Marshal(job.Eligibility)
	if err != nil {
		return MaterialisationJob{}, err
	}
	var createdID string
	err = r.DB.QueryRowContext(ctx, `
INSERT INTO audience_materialisation_jobs(
 id,campaign_id,segment_id,segment_definition,definition_version,eligibility_context,
 consent_policy_version,configuration_version,requested_by,request_fingerprint,status,processed_count,
 expected_count,rolling_hash,attempt_count,next_attempt_at,version,requested_at,updated_at)
VALUES($1::uuid,$2::uuid,NULLIF($3,'')::uuid,$4::jsonb,$5,$6::jsonb,$7,$8,$9::uuid,$10,$11,0,$12,$13,0,$14,1,$15,$15)
ON CONFLICT(campaign_id,request_fingerprint) DO NOTHING
RETURNING id::text`, job.ID, job.CampaignID, job.SegmentID, definition, job.DefinitionVersion, eligibility,
		job.ConsentPolicyVersion, job.ConfigurationVersion, job.RequestedBy, job.RequestFingerprint, job.Status,
		job.ExpectedCount, job.RollingHash, job.NextAttemptAt, job.RequestedAt).Scan(&createdID)
	if err == nil {
		return r.Get(ctx, createdID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return MaterialisationJob{}, fmt.Errorf("create audience materialisation: %w", err)
	}
	existing, loadErr := scanMaterialisation(r.DB.QueryRowContext(ctx, materialisationSelect+` WHERE campaign_id=$1::uuid AND request_fingerprint=$2`, job.CampaignID, job.RequestFingerprint))
	if loadErr == nil {
		return existing, nil
	}
	var activeID string
	activeErr := r.DB.QueryRowContext(ctx, `SELECT id::text FROM audience_materialisation_jobs WHERE campaign_id=$1::uuid AND status IN ('PENDING','RUNNING') LIMIT 1`, job.CampaignID).Scan(&activeID)
	if activeErr == nil {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	if !errors.Is(activeErr, sql.ErrNoRows) {
		return MaterialisationJob{}, activeErr
	}
	return MaterialisationJob{}, loadErr
}

func (r *PostgreSQLRepository) Get(ctx context.Context, identifier string) (MaterialisationJob, error) {
	if r == nil || r.DB == nil {
		return MaterialisationJob{}, errors.New("database is required")
	}
	return scanMaterialisation(r.DB.QueryRowContext(ctx, materialisationSelect+` WHERE id=$1::uuid`, identifier))
}

func (r *PostgreSQLRepository) ListByCampaign(ctx context.Context, campaignID string, limit int) ([]MaterialisationJob, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.DB.QueryContext(ctx, materialisationSelect+` WHERE ($1='' OR campaign_id=NULLIF($1,'')::uuid) ORDER BY requested_at DESC LIMIT $2`, strings.TrimSpace(campaignID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MaterialisationJob{}
	for rows.Next() {
		item, err := scanMaterialisation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) ListByCampaignPage(ctx context.Context, campaignID string, limit int, before *time.Time, beforeID string) ([]MaterialisationJob, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, materialisationSelect+` WHERE campaign_id=$1::uuid AND ($3::timestamptz IS NULL OR requested_at<$3 OR (requested_at=$3 AND id<NULLIF($4,'')::uuid)) ORDER BY requested_at DESC,id DESC LIMIT $2`, campaignID, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MaterialisationJob{}
	for rows.Next() {
		item, scanErr := scanMaterialisation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) Claim(ctx context.Context, owner string, limit int, lease time.Duration, now time.Time) ([]MaterialisationJob, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if strings.TrimSpace(owner) == "" || limit <= 0 || limit > 16 || lease <= 0 {
		return nil, errors.New("valid owner, claim limit and lease are required")
	}
	rows, err := r.DB.QueryContext(ctx, `
WITH candidates AS (
 SELECT id FROM audience_materialisation_jobs
 WHERE status IN ('PENDING','RUNNING') AND next_attempt_at <= $3
   AND (lease_expires_at IS NULL OR lease_expires_at <= $3)
 ORDER BY requested_at,id FOR UPDATE SKIP LOCKED LIMIT $2
)
UPDATE audience_materialisation_jobs j
SET status='RUNNING',lease_owner=$1,lease_token=j.lease_token+1,
    lease_expires_at=$3+make_interval(secs => $4),attempt_count=j.attempt_count+1,
    started_at=coalesce(j.started_at,$3),version=j.version+1,updated_at=$3
FROM candidates c WHERE j.id=c.id
RETURNING `+materialisationReturningColumns, owner, limit, now, lease.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MaterialisationJob{}
	for rows.Next() {
		item, err := scanMaterialisation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgreSQLRepository) Renew(ctx context.Context, identifier, owner string, token int64, now time.Time, lease time.Duration) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE audience_materialisation_jobs SET lease_expires_at=$5,updated_at=$4 WHERE id=$1::uuid AND lease_owner=$2 AND lease_token=$3 AND status='RUNNING'`, identifier, owner, token, now, now.Add(lease))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrMaterialisationConflict
	}
	return nil
}

func (r *PostgreSQLRepository) AppendMembers(ctx context.Context, identifier string, token int64, members []segment.Member, lastContactID, rollingHash string, processedCount int64, now time.Time) (MaterialisationJob, error) {
	if r == nil || r.DB == nil {
		return MaterialisationJob{}, errors.New("database is required")
	}
	if len(members) == 0 || strings.TrimSpace(lastContactID) == "" || len(strings.TrimSpace(rollingHash)) != 64 {
		return MaterialisationJob{}, errors.New("materialisation member checkpoint is invalid")
	}
	payload, err := json.Marshal(members)
	if err != nil {
		return MaterialisationJob{}, err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return MaterialisationJob{}, err
	}
	defer tx.Rollback()
	var currentCount int64
	if err := tx.QueryRowContext(ctx, `SELECT processed_count FROM audience_materialisation_jobs WHERE id=$1::uuid AND lease_token=$2 AND status='RUNNING' FOR UPDATE`, identifier, token).Scan(&currentCount); errors.Is(err, sql.ErrNoRows) {
		return MaterialisationJob{}, ErrMaterialisationConflict
	} else if err != nil {
		return MaterialisationJob{}, err
	}
	if processedCount != currentCount+int64(len(members)) {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO audience_materialisation_members(job_id,contact_id,eligibility_evidence_hash,created_at)
SELECT $1::uuid,x."contactId"::uuid,x."eligibilityEvidenceHash",$3
FROM jsonb_to_recordset($2::jsonb) x("contactId" text,"eligibilityEvidenceHash" text)
ON CONFLICT(job_id,contact_id) DO NOTHING`, identifier, string(payload), now)
	if err != nil {
		return MaterialisationJob{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return MaterialisationJob{}, err
	}
	if inserted != int64(len(members)) {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	result, err = tx.ExecContext(ctx, `
UPDATE audience_materialisation_jobs
SET processed_count=$4,last_contact_id=$3::uuid,rolling_hash=$5,
    lease_expires_at=NULL,lease_owner=NULL,version=version+1,updated_at=$6
WHERE id=$1::uuid AND lease_token=$2 AND status='RUNNING' AND expected_count >= $4`,
		identifier, token, lastContactID, processedCount, rollingHash, now)
	if err != nil {
		return MaterialisationJob{}, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return MaterialisationJob{}, err
	}
	if updated != 1 {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	if err := tx.Commit(); err != nil {
		return MaterialisationJob{}, err
	}
	return r.Get(ctx, identifier)
}

func (r *PostgreSQLRepository) Complete(ctx context.Context, identifier string, token int64, snapshot segment.Snapshot, now time.Time) (MaterialisationJob, error) {
	result, err := r.DB.ExecContext(ctx, `
UPDATE audience_materialisation_jobs
SET status='COMPLETED',snapshot_id=$3::uuid,completed_at=$4,lease_owner=NULL,
    lease_expires_at=NULL,failure_code=NULL,failure_reference=NULL,version=version+1,updated_at=$4
WHERE id=$1::uuid AND lease_token=$2 AND status='RUNNING'
  AND processed_count=expected_count AND rolling_hash=$5`, identifier, token, snapshot.ID, now, snapshot.SnapshotHash)
	if err != nil {
		return MaterialisationJob{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return MaterialisationJob{}, err
	}
	if count != 1 {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	return r.Get(ctx, identifier)
}

func (r *PostgreSQLRepository) Retry(ctx context.Context, identifier string, token int64, code, reference string, nextAttemptAt, now time.Time) error {
	result, err := r.DB.ExecContext(ctx, `
UPDATE audience_materialisation_jobs
SET status='PENDING',failure_code=$3,failure_reference=$4,next_attempt_at=$5,
    lease_owner=NULL,lease_expires_at=NULL,version=version+1,updated_at=$6
WHERE id=$1::uuid AND lease_token=$2 AND status='RUNNING'`, identifier, token, code, reference, nextAttemptAt, now)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrMaterialisationConflict
	}
	return nil
}

func (r *PostgreSQLRepository) Fail(ctx context.Context, identifier string, token int64, code, reference string, now time.Time) error {
	result, err := r.DB.ExecContext(ctx, `
UPDATE audience_materialisation_jobs
SET status='FAILED',failure_code=$3,failure_reference=$4,completed_at=$5,
    lease_owner=NULL,lease_expires_at=NULL,version=version+1,updated_at=$5
WHERE id=$1::uuid AND lease_token=$2 AND status='RUNNING'`, identifier, token, code, reference, now)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrMaterialisationConflict
	}
	return nil
}

func (r *PostgreSQLRepository) Cancel(ctx context.Context, identifier string, version int64, actor, reason string, now time.Time) (MaterialisationJob, error) {
	result, err := r.DB.ExecContext(ctx, `
UPDATE audience_materialisation_jobs
SET status='CANCELLED',completed_at=$5,cancelled_by=$3::uuid,cancellation_reason=$4,lease_owner=NULL,lease_expires_at=NULL,
    version=version+1,updated_at=$5
WHERE id=$1::uuid AND version=$2 AND status IN ('PENDING','RUNNING')`, identifier, version, actor, reason, now)
	if err != nil {
		return MaterialisationJob{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return MaterialisationJob{}, err
	}
	if count != 1 {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	return r.Get(ctx, identifier)
}

func (r *PostgreSQLRepository) StagedMembers(ctx context.Context, identifier string) ([]segment.Member, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT contact_id::text,eligibility_evidence_hash FROM audience_materialisation_members WHERE job_id=$1::uuid ORDER BY contact_id`, identifier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []segment.Member{}
	for rows.Next() {
		var item segment.Member
		if err := rows.Scan(&item.ContactID, &item.EligibilityEvidenceHash); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CommitSnapshot atomically promotes staged members to the immutable snapshot
// tables and completes the durable job. This avoids loading million-record
// audiences into process memory and closes the crash window between snapshot
// creation and job completion.
func (r *PostgreSQLRepository) CommitSnapshot(ctx context.Context, identifier string, token int64, snapshot segment.Snapshot, now time.Time) (MaterialisationJob, error) {
	if r == nil || r.DB == nil {
		return MaterialisationJob{}, errors.New("database is required")
	}
	definition, err := json.Marshal(snapshot.Definition)
	if err != nil {
		return MaterialisationJob{}, err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return MaterialisationJob{}, err
	}
	defer tx.Rollback()
	var expectedCount, processedCount int64
	var rollingHash string
	if err := tx.QueryRowContext(ctx, `SELECT expected_count,processed_count,rolling_hash FROM audience_materialisation_jobs WHERE id=$1::uuid AND lease_token=$2 AND status='RUNNING' FOR UPDATE`, identifier, token).Scan(&expectedCount, &processedCount, &rollingHash); errors.Is(err, sql.ErrNoRows) {
		return MaterialisationJob{}, ErrMaterialisationConflict
	} else if err != nil {
		return MaterialisationJob{}, err
	}
	if expectedCount != processedCount || snapshot.EligibleCount != expectedCount || snapshot.SnapshotHash != rollingHash {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	var snapshotID string
	err = tx.QueryRowContext(ctx, `
INSERT INTO audience_snapshots(id,campaign_id,segment_id,segment_definition,definition_version,consent_policy_version,configuration_version,snapshot_hash,eligible_count,created_by,created_at)
VALUES($1::uuid,$2::uuid,NULLIF($3,'')::uuid,$4::jsonb,$5,$6,$7,$8,$9,$10::uuid,$11)
ON CONFLICT(campaign_id,snapshot_hash) DO NOTHING
RETURNING id::text`, snapshot.ID, snapshot.CampaignID, snapshot.SegmentID, definition, snapshot.DefinitionVersion, snapshot.ConsentPolicyVersion, snapshot.ConfigurationVersion, snapshot.SnapshotHash, snapshot.EligibleCount, snapshot.CreatedBy, snapshot.CreatedAt).Scan(&snapshotID)
	created := true
	if errors.Is(err, sql.ErrNoRows) {
		created = false
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM audience_snapshots WHERE campaign_id=$1::uuid AND snapshot_hash=$2 FOR SHARE`, snapshot.CampaignID, snapshot.SnapshotHash).Scan(&snapshotID)
	}
	if err != nil {
		return MaterialisationJob{}, fmt.Errorf("commit audience snapshot identity: %w", err)
	}
	if created {
		result, err := tx.ExecContext(ctx, `
INSERT INTO audience_snapshot_members(snapshot_id,contact_id,eligibility_evidence)
SELECT $1::uuid,contact_id,jsonb_build_object('hash',eligibility_evidence_hash)
FROM audience_materialisation_members
WHERE job_id=$2::uuid
ORDER BY contact_id`, snapshotID, identifier)
		if err != nil {
			return MaterialisationJob{}, fmt.Errorf("promote audience materialisation members: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return MaterialisationJob{}, err
		}
		if count != expectedCount {
			return MaterialisationJob{}, ErrMaterialisationConflict
		}
	} else {
		var existingCount int64
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM audience_snapshot_members WHERE snapshot_id=$1::uuid`, snapshotID).Scan(&existingCount); err != nil {
			return MaterialisationJob{}, err
		}
		if existingCount != expectedCount {
			return MaterialisationJob{}, ErrMaterialisationConflict
		}
	}
	result, err := tx.ExecContext(ctx, `
UPDATE audience_materialisation_jobs
SET status='COMPLETED',snapshot_id=$3::uuid,completed_at=$4,lease_owner=NULL,
    lease_expires_at=NULL,failure_code=NULL,failure_reference=NULL,version=version+1,updated_at=$4
WHERE id=$1::uuid AND lease_token=$2 AND status='RUNNING'`, identifier, token, snapshotID, now)
	if err != nil {
		return MaterialisationJob{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return MaterialisationJob{}, err
	}
	if count != 1 {
		return MaterialisationJob{}, ErrMaterialisationConflict
	}
	if err := tx.Commit(); err != nil {
		return MaterialisationJob{}, err
	}
	return r.Get(ctx, identifier)
}

type materialisationScanner interface{ Scan(...any) error }

func scanMaterialisation(row materialisationScanner) (MaterialisationJob, error) {
	var item MaterialisationJob
	var definition, eligibility []byte
	var leaseExpiresAt, startedAt, completedAt sql.NullTime
	err := row.Scan(
		&item.ID, &item.CampaignID, &item.SegmentID, &definition, &item.DefinitionVersion,
		&eligibility, &item.ConsentPolicyVersion, &item.ConfigurationVersion, &item.RequestedBy,
		&item.RequestFingerprint, &item.Status, &item.ProcessedCount, &item.ExpectedCount, &item.LastContactID,
		&item.RollingHash, &item.SnapshotID, &item.AttemptCount, &item.NextAttemptAt,
		&item.FailureCode, &item.FailureReference, &item.LeaseOwner, &item.LeaseToken,
		&leaseExpiresAt, &item.Version, &item.RequestedAt, &startedAt, &completedAt,
		&item.CancelledBy, &item.CancellationReason, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MaterialisationJob{}, ErrMaterialisationNotFound
	}
	if err != nil {
		return MaterialisationJob{}, err
	}
	if err := json.Unmarshal(definition, &item.Definition); err != nil {
		return MaterialisationJob{}, err
	}
	if err := json.Unmarshal(eligibility, &item.Eligibility); err != nil {
		return MaterialisationJob{}, err
	}
	if leaseExpiresAt.Valid {
		value := leaseExpiresAt.Time
		item.LeaseExpiresAt = &value
	}
	if startedAt.Valid {
		value := startedAt.Time
		item.StartedAt = &value
	}
	if completedAt.Valid {
		value := completedAt.Time
		item.CompletedAt = &value
	}
	return item, nil
}
