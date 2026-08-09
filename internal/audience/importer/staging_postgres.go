package importer

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	pgretry "campaign-platform/internal/persistence/postgres"
)

type PostgreSQLStagingRepository struct {
	DB            *sql.DB
	WorkerID      string
	LeaseDuration time.Duration
	Clock         func() time.Time
}

func (r *PostgreSQLStagingRepository) now() time.Time {
	if r.Clock != nil {
		return r.Clock().UTC()
	}
	return time.Now().UTC()
}
func (r *PostgreSQLStagingRepository) workerID() string {
	if strings.TrimSpace(r.WorkerID) == "" {
		return "audience-validation-worker"
	}
	return strings.TrimSpace(r.WorkerID)
}
func (r *PostgreSQLStagingRepository) leaseDuration() time.Duration {
	if r.LeaseDuration <= 0 {
		return 2 * time.Minute
	}
	return r.LeaseDuration
}

func (r *PostgreSQLStagingRepository) Begin(ctx context.Context, importID string) (ValidationLease, error) {
	if r == nil || r.DB == nil {
		return ValidationLease{}, errors.New("database is required")
	}
	now := r.now()
	var lease ValidationLease
	err := r.DB.QueryRowContext(ctx, `
UPDATE audience_imports
SET status='VALIDATING',failure_reason=NULL,
    validation_lease_owner=$2,validation_lease_expires_at=$3,
    validation_lease_version=validation_lease_version+1,updated_at=$4,version=version+1
WHERE id=$1::uuid
  AND status IN ('VALIDATING','PREVIEW_READY','FAILED')
  AND malware_scan_status='CLEAN' AND content_signature_valid=true
  AND (validation_lease_owner IS NULL OR validation_lease_expires_at<=$4 OR validation_lease_owner=$2)
RETURNING validation_lease_owner,validation_lease_version,validation_lease_expires_at`,
		importID, r.workerID(), now.Add(r.leaseDuration()), now).Scan(&lease.Owner, &lease.Version, &lease.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ValidationLease{}, errors.New("import is unavailable, unsafe, or already leased for validation")
	}
	if err != nil {
		return ValidationLease{}, err
	}
	return lease, nil
}

func (r *PostgreSQLStagingRepository) Renew(ctx context.Context, importID string, lease ValidationLease) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	now := r.now()
	result, err := r.DB.ExecContext(ctx, `UPDATE audience_imports
SET validation_lease_expires_at=$4,updated_at=$5
WHERE id=$1::uuid AND status='VALIDATING' AND validation_lease_owner=$2
  AND validation_lease_version=$3 AND validation_lease_expires_at>$5
  AND malware_scan_status='CLEAN' AND content_signature_valid=true`,
		importID, lease.Owner, lease.Version, now.Add(r.leaseDuration()), now)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrImportConflict
	}
	return nil
}

type stagedJSON struct {
	RowNumber         int    `json:"row_number"`
	EncryptedHex      string `json:"encrypted_hex"`
	LookupHMACHex     string `json:"lookup_hmac_hex"`
	MaskedMSISDN      string `json:"masked_msisdn"`
	CountryISO2       string `json:"country_iso2"`
	StateName         string `json:"state_name,omitempty"`
	LGAName           string `json:"lga_name,omitempty"`
	ReportedAge       *int   `json:"reported_age,omitempty"`
	AgeRecordedDate   string `json:"age_recorded_date,omitempty"`
	ProfileRecordedAt string `json:"profile_recorded_at"`
	GenderCode        string `json:"gender_code,omitempty"`
	SourceHash        string `json:"source_hash"`
}

func (r *PostgreSQLStagingRepository) StageBatch(ctx context.Context, importID string, lease ValidationLease, candidates []ContactCandidate) (StageBatchResult, error) {
	if r == nil || r.DB == nil {
		return StageBatchResult{}, errors.New("database is required")
	}
	if len(candidates) == 0 {
		return StageBatchResult{}, nil
	}
	if len(candidates) > 10_000 {
		return StageBatchResult{}, errors.New("staging batch exceeds 10000 rows")
	}
	payload := make([]stagedJSON, len(candidates))
	rows := make(map[int]string, len(candidates))
	for i, value := range candidates {
		if value.E164 != "" || len(value.EncryptedMSISDN) == 0 || len(value.LookupHMAC) == 0 {
			return StageBatchResult{}, errors.New("staging accepts protected candidates only")
		}
		if value.RowNumber < 2 {
			return StageBatchResult{}, errors.New("staging row number must be at least 2")
		}
		if value.ProfileRecordedAt.IsZero() {
			return StageBatchResult{}, errors.New("profile observation timestamp is required")
		}
		lookupHex := hex.EncodeToString(value.LookupHMAC)
		if previous, exists := rows[value.RowNumber]; exists && previous != lookupHex {
			return StageBatchResult{}, fmt.Errorf("row %d appears with different protected MSISDNs in one batch", value.RowNumber)
		}
		rows[value.RowNumber] = lookupHex
		item := stagedJSON{
			RowNumber: value.RowNumber, EncryptedHex: hex.EncodeToString(value.EncryptedMSISDN),
			LookupHMACHex: lookupHex, MaskedMSISDN: value.MaskedMSISDN,
			CountryISO2: strings.ToUpper(strings.TrimSpace(value.Country)), StateName: value.State,
			LGAName: value.LGA, ReportedAge: value.ReportedAge, GenderCode: value.Gender,
			SourceHash: value.SourceHash, ProfileRecordedAt: value.ProfileRecordedAt.UTC().Format(time.RFC3339Nano),
		}
		if !value.AgeRecordedAt.IsZero() {
			item.AgeRecordedDate = value.AgeRecordedAt.UTC().Format("2006-01-02")
		}
		payload[i] = item
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return StageBatchResult{}, err
	}
	type stageOutcome struct {
		result     StageBatchResult
		mismatches int
	}
	outcome, err := pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (stageOutcome, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return stageOutcome{}, err
		}
		defer tx.Rollback()
		now := r.now()
		if err := renewValidationLease(ctx, tx, importID, lease, now, r.leaseDuration()); err != nil {
			return stageOutcome{}, err
		}
		const query = `
WITH raw AS (
  SELECT x.row_number,decode(x.encrypted_hex,'hex') AS encrypted_msisdn,
         decode(x.lookup_hmac_hex,'hex') AS lookup_hmac,x.masked_msisdn,
         upper(x.country_iso2)::char(2) AS country_iso2,nullif(x.state_name,'') AS state_name,
         nullif(x.lga_name,'') AS lga_name,x.reported_age,
         nullif(x.age_recorded_date,'')::date AS age_recorded_at,
         x.profile_recorded_at::timestamptz AS profile_recorded_at,
         nullif(x.gender_code,'') AS gender_code,x.source_hash
  FROM jsonb_to_recordset($2::jsonb) AS x(
    row_number bigint,encrypted_hex text,lookup_hmac_hex text,masked_msisdn text,
    country_iso2 text,state_name text,lga_name text,reported_age smallint,
    age_recorded_date text,profile_recorded_at text,gender_code text,source_hash text)
), ranked AS (
  SELECT raw.*,row_number() OVER(PARTITION BY lookup_hmac ORDER BY row_number) AS duplicate_rank
  FROM raw
), existing_by_row AS MATERIALIZED (
  SELECT r.row_number,r.lookup_hmac AS incoming_lookup,s.msisdn_lookup_hmac AS stored_lookup
  FROM ranked r JOIN audience_import_staging s
    ON s.audience_import_id=$1::uuid AND s.row_number=r.row_number
), existing_by_lookup AS MATERIALIZED (
  SELECT r.row_number,r.lookup_hmac,s.row_number AS first_row
  FROM ranked r JOIN audience_import_staging s
    ON s.audience_import_id=$1::uuid AND s.msisdn_lookup_hmac=r.lookup_hmac
), inserted AS (
  INSERT INTO audience_import_staging(
    audience_import_id,row_number,encrypted_msisdn,msisdn_lookup_hmac,masked_msisdn,
    country_iso2,state_name,lga_name,reported_age,age_recorded_at,profile_recorded_at,gender_code,source_record_hash)
  SELECT $1::uuid,r.row_number,r.encrypted_msisdn,r.lookup_hmac,r.masked_msisdn,
         r.country_iso2,r.state_name,r.lga_name,r.reported_age,r.age_recorded_at,r.profile_recorded_at,r.gender_code,r.source_hash
  FROM ranked r
  WHERE r.duplicate_rank=1
    AND NOT EXISTS(SELECT 1 FROM existing_by_row e WHERE e.row_number=r.row_number)
    AND NOT EXISTS(SELECT 1 FROM existing_by_lookup e WHERE e.row_number=r.row_number)
  ON CONFLICT DO NOTHING
  RETURNING row_number
), duplicate_candidates AS (
  SELECT row_number FROM ranked WHERE duplicate_rank>1
  UNION
  SELECT row_number FROM existing_by_lookup WHERE first_row<>row_number
), duplicate_issues AS (
  INSERT INTO audience_import_issues(audience_import_id,row_number,field_name,issue_code,issue_message,masked_value)
  SELECT $1::uuid,d.row_number,'msisdn','DUPLICATE_MSISDN',
         'duplicates an earlier protected MSISDN row',NULL
  FROM duplicate_candidates d
  ON CONFLICT (audience_import_id,row_number,issue_code) DO NOTHING
  RETURNING 1
), progress AS (
  UPDATE audience_imports
  SET last_processed_row=greatest(last_processed_row,(SELECT coalesce(max(row_number),0) FROM raw)),updated_at=$3
  WHERE id=$1::uuid
  RETURNING 1
)
SELECT (SELECT count(*) FROM inserted),
       (SELECT count(*) FROM duplicate_issues),
       (SELECT count(*) FROM existing_by_row WHERE incoming_lookup=stored_lookup),
       (SELECT count(*) FROM existing_by_row WHERE incoming_lookup<>stored_lookup)`
		var value stageOutcome
		if err := tx.QueryRowContext(ctx, query, importID, string(encoded), now).Scan(&value.result.Inserted, &value.result.SourceDuplicates, &value.result.Replayed, &value.mismatches); err != nil {
			return stageOutcome{}, fmt.Errorf("stage import batch: %w", err)
		}
		if value.mismatches != 0 {
			return stageOutcome{}, fmt.Errorf("%d import rows changed during replay", value.mismatches)
		}
		if err := tx.Commit(); err != nil {
			return stageOutcome{}, err
		}
		return value, nil
	})
	if err != nil {
		return StageBatchResult{}, err
	}
	return outcome.result, nil
}

func (r *PostgreSQLStagingRepository) SaveIssues(ctx context.Context, importID string, lease ValidationLease, issues []RowIssue) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	if len(issues) == 0 {
		return nil
	}
	_, err := pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (struct{}, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return struct{}{}, err
		}
		defer tx.Rollback()
		if err := renewValidationLease(ctx, tx, importID, lease, r.now(), r.leaseDuration()); err != nil {
			return struct{}{}, err
		}
		statement, err := tx.PrepareContext(ctx, `INSERT INTO audience_import_issues(audience_import_id,row_number,field_name,issue_code,issue_message) VALUES($1::uuid,$2,$3,$4,$5) ON CONFLICT (audience_import_id,row_number,issue_code) DO NOTHING`)
		if err != nil {
			return struct{}{}, err
		}
		defer statement.Close()
		for _, issue := range issues {
			if _, err := statement.ExecContext(ctx, importID, issue.RowNumber, nullString(issue.Field), issue.Code, truncate(issue.Message, 1000)); err != nil {
				return struct{}{}, err
			}
		}
		return struct{}{}, tx.Commit()
	})
	return err
}

func (r *PostgreSQLStagingRepository) Summary(ctx context.Context, importID string, lease ValidationLease) (ImportSummary, error) {
	if r == nil || r.DB == nil {
		return ImportSummary{}, errors.New("database is required")
	}
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (ImportSummary, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: false})
		if err != nil {
			return ImportSummary{}, err
		}
		defer tx.Rollback()
		if err := renewValidationLease(ctx, tx, importID, lease, r.now(), r.leaseDuration()); err != nil {
			return ImportSummary{}, err
		}
		var summary ImportSummary
		err = tx.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM audience_import_staging WHERE audience_import_id=$1::uuid),
 (SELECT count(*) FROM audience_import_issues WHERE audience_import_id=$1::uuid AND issue_code='DUPLICATE_MSISDN')`, importID).Scan(&summary.StagedRows, &summary.SourceDuplicates)
		if err != nil {
			return ImportSummary{}, err
		}
		if err := tx.Commit(); err != nil {
			return ImportSummary{}, err
		}
		return summary, nil
	})
}

func (r *PostgreSQLStagingRepository) CompleteValidation(ctx context.Context, importID string, lease ValidationLease, result PreviewResult, summary ImportSummary) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE audience_imports
SET status='PREVIEW_READY',uploaded_rows=$4,valid_rows=$5,invalid_rows=$6,duplicate_rows=$7,
    failure_reason=NULL,validation_lease_owner=NULL,validation_lease_expires_at=NULL,
    version=version+1,updated_at=$8
WHERE id=$1::uuid AND status='VALIDATING' AND validation_lease_owner=$2
  AND validation_lease_version=$3 AND validation_lease_expires_at>$8
  AND malware_scan_status='CLEAN' AND content_signature_valid=true`,
		importID, lease.Owner, lease.Version, result.UploadedRows, summary.StagedRows, result.InvalidRows, summary.SourceDuplicates, r.now())
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrImportConflict
	}
	return nil
}

func (r *PostgreSQLStagingRepository) Fail(ctx context.Context, importID string, lease ValidationLease, cause error) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	detail := "import validation failed"
	if cause != nil {
		detail = truncate(cause.Error(), 1000)
	}
	now := r.now()
	res, err := r.DB.ExecContext(ctx, `UPDATE audience_imports
SET status='FAILED',failure_reason=$4,validation_lease_owner=NULL,validation_lease_expires_at=NULL,
    version=version+1,updated_at=$5
WHERE id=$1::uuid AND validation_lease_owner=$2 AND validation_lease_version=$3
  AND validation_lease_expires_at>$5 AND status='VALIDATING'`, importID, lease.Owner, lease.Version, detail, now)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrImportConflict
	}
	return nil
}

func renewValidationLease(ctx context.Context, tx *sql.Tx, importID string, lease ValidationLease, now time.Time, duration time.Duration) error {
	if strings.TrimSpace(lease.Owner) == "" || lease.Version <= 0 {
		return ErrImportConflict
	}
	res, err := tx.ExecContext(ctx, `UPDATE audience_imports
SET validation_lease_expires_at=$4,updated_at=$5
WHERE id=$1::uuid AND status='VALIDATING' AND validation_lease_owner=$2
  AND validation_lease_version=$3 AND validation_lease_expires_at>$5
  AND malware_scan_status='CLEAN' AND content_signature_valid=true`,
		importID, lease.Owner, lease.Version, now.Add(duration), now)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrImportConflict
	}
	return nil
}

func nullString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
func truncate(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}

// ClaimReady atomically leases clean imports using SKIP LOCKED. The lease
// version is a fencing token used by every subsequent staging mutation.
func (r *PostgreSQLStagingRepository) ClaimReady(ctx context.Context, limit int) ([]ValidationWork, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	now := r.now()
	rows, err := r.DB.QueryContext(ctx, `WITH candidates AS (
  SELECT id FROM audience_imports
  WHERE status='VALIDATING' AND malware_scan_status='CLEAN' AND content_signature_valid=true
    AND (validation_lease_owner IS NULL OR validation_lease_expires_at<=$1)
  ORDER BY created_at,id
  FOR UPDATE SKIP LOCKED
  LIMIT $2
)
UPDATE audience_imports ai
SET validation_lease_owner=$3,validation_lease_expires_at=$4,
    validation_lease_version=ai.validation_lease_version+1,
    version=ai.version+1,updated_at=$1,failure_reason=NULL
FROM candidates c
WHERE ai.id=c.id
RETURNING ai.id::text,ai.object_key,ai.original_filename,ai.detected_media_type,
 ai.file_sha256,ai.byte_size,coalesce(ai.default_country_iso2::text,''),ai.mapping,
 ai.validation_lease_owner,ai.validation_lease_version,ai.validation_lease_expires_at`,
		now, limit, r.workerID(), now.Add(r.leaseDuration()))
	if err != nil {
		return nil, fmt.Errorf("claim audience imports: %w", err)
	}
	defer rows.Close()
	items := make([]ValidationWork, 0, limit)
	for rows.Next() {
		var item ValidationWork
		var mapping []byte
		if err := rows.Scan(&item.ImportID, &item.ObjectKey, &item.OriginalFilename, &item.DetectedMediaType,
			&item.FileSHA256, &item.ByteSize, &item.DefaultCountryISO2, &mapping,
			&item.Lease.Owner, &item.Lease.Version, &item.Lease.ExpiresAt); err != nil {
			return nil, err
		}
		if !json.Valid(mapping) {
			return nil, errors.New("audience import mapping is invalid JSON")
		}
		item.Mapping = append(json.RawMessage(nil), mapping...)
		items = append(items, item)
	}
	return items, rows.Err()
}
