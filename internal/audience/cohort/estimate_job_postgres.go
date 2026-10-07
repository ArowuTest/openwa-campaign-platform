package cohort

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/jobs"
	postgresrepo "campaign-platform/internal/persistence/postgres"
)

type PostgreSQLEstimateRepository struct {
	DB    *sql.DB
	Queue *jobs.PostgreSQLRepository
}

func (r *PostgreSQLEstimateRepository) queue() *jobs.PostgreSQLRepository {
	if r.Queue != nil {
		return r.Queue
	}
	return &jobs.PostgreSQLRepository{DB: r.DB}
}

func (r *PostgreSQLEstimateRepository) Schedule(ctx context.Context, input EstimateJobRequest, now time.Time) (EstimateJobRecord, bool, error) {
	if r == nil || r.DB == nil {
		return EstimateJobRecord{}, false, errors.New("database is required")
	}
	input, err := normalizeEstimateRequest(input)
	if err != nil {
		return EstimateJobRecord{}, false, err
	}
	fingerprint, err := estimateRequestFingerprint(input)
	if err != nil {
		return EstimateJobRecord{}, false, err
	}
	if existing, err := r.loadByRequest(ctx, r.DB, input.OrganisationID, input.ClientRequestID); err == nil {
		if existing.RequestFingerprint != fingerprint {
			return EstimateJobRecord{}, false, ErrEstimateReplayConflict
		}
		job, getErr := r.queue().Get(ctx, existing.ID)
		if getErr != nil {
			return EstimateJobRecord{}, false, getErr
		}
		existing.Job = job
		return existing, false, nil
	} else if !errors.Is(err, ErrEstimateNotFound) {
		return EstimateJobRecord{}, false, err
	}

	job, err := jobs.NewJob(jobs.EnqueueInput{
		Type:        EstimateJobType,
		DedupKey:    estimateDedupKey(input),
		Payload:     map[string]any{},
		MaxAttempts: 5,
		AvailableAt: now.UTC(),
	}, now.UTC())
	if err != nil {
		return EstimateJobRecord{}, false, err
	}

	// Retry only server-confirmed transaction aborts, preserving the job ID,
	// request fingerprint and as-of time across fresh atomic transactions.
	result, err := postgresrepo.RetryValue(ctx, postgresrepo.DefaultRetryPolicy(), func() (estimateScheduleResult, error) {
		record, created, err := r.scheduleInTx(ctx, input, fingerprint, job, now)
		return estimateScheduleResult{record: record, created: created}, err
	})
	return result.record, result.created, err
}

type estimateScheduleResult struct {
	record  EstimateJobRecord
	created bool
}

func (r *PostgreSQLEstimateRepository) scheduleInTx(ctx context.Context, input EstimateJobRequest, fingerprint string, job jobs.Job, now time.Time) (EstimateJobRecord, bool, error) {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return EstimateJobRecord{}, false, err
	}
	defer tx.Rollback()

	enqueued, created, err := r.queue().EnqueueInTx(ctx, tx, job)
	if err != nil {
		return EstimateJobRecord{}, false, err
	}
	if !created {
		existing, loadErr := r.loadByID(ctx, tx, enqueued.ID)
		if loadErr != nil {
			return EstimateJobRecord{}, false, loadErr
		}
		if existing.RequestFingerprint != fingerprint {
			return EstimateJobRecord{}, false, ErrEstimateReplayConflict
		}
		if err := tx.Commit(); err != nil {
			return EstimateJobRecord{}, false, err
		}
		existing.Job = enqueued
		return existing, false, nil
	}

	definition, err := json.Marshal(input.Definition)
	if err != nil {
		return EstimateJobRecord{}, false, err
	}
	_, err = tx.ExecContext(ctx,
		"INSERT INTO audience_cohort_estimates("+
			"id,organisation_id,purpose_id,channel,definition,as_of,requested_by,client_request_id,request_fingerprint,created_at,updated_at"+
			") VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5::jsonb,$6,$7::uuid,$8,$9,$10,$10)",
		enqueued.ID, input.OrganisationID, input.PurposeID, input.Channel, string(definition), now.UTC(),
		input.RequestedBy, input.ClientRequestID, fingerprint, now.UTC(),
	)
	if err != nil {
		return EstimateJobRecord{}, false, fmt.Errorf("insert cohort estimate evidence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return EstimateJobRecord{}, false, err
	}
	record := EstimateJobRecord{
		ID: enqueued.ID, OrganisationID: input.OrganisationID, PurposeID: input.PurposeID,
		Channel: input.Channel, Definition: input.Definition, AsOf: now.UTC(),
		RequestedBy: input.RequestedBy, ClientRequestID: input.ClientRequestID,
		RequestFingerprint: fingerprint, Job: enqueued, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	return record, true, nil
}

type estimateQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const estimateRecordSelect = "SELECT " +
	"id::text,organisation_id::text,purpose_id::text,channel,definition,as_of,requested_by::text," +
	"client_request_id,request_fingerprint,eligible_count,breakdown,calculated_at," +
	"coalesce(consent_review_id::text,''),consent_review_version,coalesce(consent_wording_version,'')," +
	"coalesce(organisation_policy_id::text,''),organisation_policy_version,created_at,updated_at " +
	"FROM audience_cohort_estimates"

func (r *PostgreSQLEstimateRepository) loadByRequest(ctx context.Context, q estimateQueryer, organisationID, requestID string) (EstimateJobRecord, error) {
	record, err := scanEstimateRecord(q.QueryRowContext(ctx,
		estimateRecordSelect+" WHERE organisation_id=$1::uuid AND client_request_id=$2",
		strings.TrimSpace(organisationID), strings.TrimSpace(requestID),
	))
	if errors.Is(err, sql.ErrNoRows) {
		return EstimateJobRecord{}, ErrEstimateNotFound
	}
	return record, err
}

func (r *PostgreSQLEstimateRepository) loadByID(ctx context.Context, q estimateQueryer, identifier string) (EstimateJobRecord, error) {
	record, err := scanEstimateRecord(q.QueryRowContext(ctx, estimateRecordSelect+" WHERE id=$1::uuid", strings.TrimSpace(identifier)))
	if errors.Is(err, sql.ErrNoRows) {
		return EstimateJobRecord{}, ErrEstimateNotFound
	}
	return record, err
}

type estimateScanner interface {
	Scan(...any) error
}

func scanEstimateRecord(row estimateScanner) (EstimateJobRecord, error) {
	var record EstimateJobRecord
	var definition, breakdown []byte
	var eligible sql.NullInt64
	var calculated sql.NullTime
	var reviewVersion, policyVersion sql.NullInt64
	if err := row.Scan(
		&record.ID, &record.OrganisationID, &record.PurposeID, &record.Channel, &definition, &record.AsOf,
		&record.RequestedBy, &record.ClientRequestID, &record.RequestFingerprint,
		&eligible, &breakdown, &calculated,
		&record.Evidence.ConsentReviewID, &reviewVersion, &record.Evidence.ConsentWordingVersion,
		&record.Evidence.OrganisationPolicyID, &policyVersion, &record.CreatedAt, &record.UpdatedAt,
	); err != nil {
		return EstimateJobRecord{}, err
	}
	if err := json.Unmarshal(definition, &record.Definition); err != nil {
		return EstimateJobRecord{}, fmt.Errorf("decode cohort estimate definition: %w", err)
	}
	if calculated.Valid {
		var value EligibilityBreakdown
		if len(breakdown) == 0 || !eligible.Valid {
			return EstimateJobRecord{}, errors.New("completed cohort estimate is missing result evidence")
		}
		if err := json.Unmarshal(breakdown, &value); err != nil {
			return EstimateJobRecord{}, fmt.Errorf("decode cohort estimate breakdown: %w", err)
		}
		record.Result = &Estimate{EligibleCount: eligible.Int64, Breakdown: &value, CalculatedAt: calculated.Time.UTC()}
		if !reviewVersion.Valid || !policyVersion.Valid {
			return EstimateJobRecord{}, errors.New("completed cohort estimate is missing governance versions")
		}
		record.Evidence.ConsentReviewVersion = reviewVersion.Int64
		record.Evidence.OrganisationPolicyVersion = policyVersion.Int64
	}
	return record, nil
}

func (r *PostgreSQLEstimateRepository) Get(ctx context.Context, identifier string) (EstimateJobRecord, error) {
	if r == nil || r.DB == nil {
		return EstimateJobRecord{}, errors.New("database is required")
	}
	record, err := r.loadByID(ctx, r.DB, identifier)
	if err != nil {
		return EstimateJobRecord{}, err
	}
	job, err := r.queue().Get(ctx, record.ID)
	if err != nil {
		return EstimateJobRecord{}, err
	}
	record.Job = job
	return record, nil
}

func (r *PostgreSQLEstimateRepository) StoreResult(ctx context.Context, identifier, owner string, leaseVersion int64, result Estimate, evidence EstimateGovernanceEvidence, now time.Time) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	if err := validateEstimateEvidence(result, evidence); err != nil {
		return err
	}
	breakdown, err := json.Marshal(result.Breakdown)
	if err != nil {
		return err
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Lock the job before the estimate, matching scheduling's lock order.
	// An unlocked UPDATE source can retain a stale owner/version snapshot while
	// waiting for the estimate row, allowing a reclaimed owner to publish.
	var lockedID string
	err = tx.QueryRowContext(ctx,
		"SELECT id::text FROM durable_jobs WHERE id=$1::uuid AND status='PROCESSING' "+
			"AND lease_owner=$2 AND lease_version=$3 "+
			"AND lease_expires_at>GREATEST($4::timestamptz,clock_timestamp()) FOR UPDATE",
		identifier, owner, leaseVersion, now.UTC(),
	).Scan(&lockedID)
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.ErrLeaseConflict
	}
	if err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx,
		"SELECT id::text FROM audience_cohort_estimates WHERE id=$1::uuid FOR UPDATE", identifier,
	).Scan(&lockedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return jobs.ErrLeaseConflict
		}
		return err
	}

	// The estimate lock may have taken longer than the lease. Recheck at the
	// write boundary with database wall time, retaining the stricter supplied
	// time contract for injected clocks. Keep the job lock through commit.
	update := "UPDATE audience_cohort_estimates e SET " +
		"eligible_count=$5,breakdown=$6::jsonb,calculated_at=$7,consent_review_id=$8::uuid," +
		"consent_review_version=$9,consent_wording_version=$10,organisation_policy_id=$11::uuid," +
		"organisation_policy_version=$12,updated_at=$4 " +
		"FROM durable_jobs j WHERE e.id=$1::uuid AND j.id=e.id AND j.status='PROCESSING' " +
		"AND j.lease_owner=$2 AND j.lease_version=$3 " +
		"AND j.lease_expires_at>GREATEST($4::timestamptz,clock_timestamp())"
	res, err := tx.ExecContext(ctx, update,
		identifier, owner, leaseVersion, now.UTC(), result.EligibleCount, string(breakdown), result.CalculatedAt.UTC(),
		evidence.ConsentReviewID, evidence.ConsentReviewVersion, evidence.ConsentWordingVersion,
		evidence.OrganisationPolicyID, evidence.OrganisationPolicyVersion,
	)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return jobs.ErrLeaseConflict
	}
	return tx.Commit()
}

func (r *PostgreSQLEstimateRepository) ListPage(ctx context.Context, organisationID string, limit int, cursor string) (EstimateJobPage, error) {
	if r == nil || r.DB == nil {
		return EstimateJobPage{}, errors.New("database is required")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	before, beforeID, err := decodeEstimateCursor(cursor)
	if err != nil {
		return EstimateJobPage{}, err
	}
	query := estimateRecordSelect + " WHERE organisation_id=$1::uuid"
	args := []any{strings.TrimSpace(organisationID)}
	if before != nil {
		args = append(args, before.UTC(), beforeID)
		query += " AND (created_at,id)<($2,$3::uuid)"
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d", len(args))
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return EstimateJobPage{}, err
	}
	defer rows.Close()
	items := make([]EstimateJobRecord, 0, limit+1)
	for rows.Next() {
		record, scanErr := scanEstimateRecord(rows)
		if scanErr != nil {
			return EstimateJobPage{}, scanErr
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return EstimateJobPage{}, err
	}
	page := EstimateJobPage{}
	if len(items) > limit {
		page.NextCursor, err = encodeEstimateCursor(items[limit-1].CreatedAt, items[limit-1].ID)
		if err != nil {
			return EstimateJobPage{}, err
		}
		items = items[:limit]
	}
	for index := range items {
		job, getErr := r.queue().Get(ctx, items[index].ID)
		if getErr != nil {
			return EstimateJobPage{}, getErr
		}
		items[index].Job = job
	}
	page.Items = items
	return page, nil
}
