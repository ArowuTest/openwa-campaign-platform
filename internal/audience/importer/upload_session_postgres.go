package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	pgretry "campaign-platform/internal/persistence/postgres"
)

type PostgreSQLUploadSessionRepository struct{ DB *sql.DB }

func (r *PostgreSQLUploadSessionRepository) Create(ctx context.Context, session UploadSession) (UploadSession, bool, error) {
	if r == nil || r.DB == nil {
		return UploadSession{}, false, errors.New("database is required")
	}
	type result struct {
		Session UploadSession
		Created bool
	}
	value, err := pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (result, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return result{}, err
		}
		defer tx.Rollback()

		mapping, err := json.Marshal(session.Mapping)
		if err != nil {
			return result{}, err
		}
		insert, err := tx.ExecContext(ctx, `
INSERT INTO audience_import_upload_sessions(
 id,organisation_id,consent_review_id,purpose_id,channel,wording_version,source_name,source_system,
 default_country_iso2,original_filename,template_version,mapping_definition_id,mapping,update_policy,
 uploaded_by,client_request_id,expected_bytes,uploaded_bytes,part_size,part_count,uploaded_parts,state,
 failure_reason,expires_at,version,created_at,updated_at
) VALUES(
 $1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,NULLIF($8,''),
 NULLIF($9,'')::char(2),$10,$11,NULLIF($12,'')::uuid,$13::jsonb,$14,
 $15::uuid,$16,$17,$18,$19,$20,$21,$22,NULLIF($23,''),$24,$25,$26,$27
)
ON CONFLICT (organisation_id,client_request_id) DO NOTHING`,
			session.ID, session.OrganisationID, session.ConsentReviewID, session.PurposeID, session.Channel,
			session.WordingVersion, session.SourceName, session.SourceSystem, session.DefaultCountryISO2,
			session.OriginalFilename, session.TemplateVersion, session.MappingDefinitionID, string(mapping),
			session.UpdatePolicy, session.UploadedBy, session.ClientRequestID, session.ExpectedBytes,
			session.UploadedBytes, session.PartSize, session.PartCount, session.UploadedParts, session.State,
			session.FailureReason, session.ExpiresAt, session.Version, session.CreatedAt, session.UpdatedAt)
		if err != nil {
			return result{}, fmt.Errorf("insert upload session: %w", err)
		}
		rows, err := insert.RowsAffected()
		if err != nil {
			return result{}, err
		}
		if rows == 0 {
			existing, err := loadUploadSessionByRequest(ctx, tx, session.OrganisationID, session.ClientRequestID, true)
			if err != nil {
				return result{}, err
			}
			if existing.RequestFingerprint() != session.RequestFingerprint() {
				return result{}, ErrUploadSessionReplayConflict
			}
			if err := tx.Commit(); err != nil {
				return result{}, err
			}
			return result{Session: existing}, nil
		}

		statement, err := tx.PrepareContext(ctx, `
INSERT INTO audience_import_upload_parts(
 session_id,part_number,offset_bytes,expected_bytes,object_key,state,sha256,uploaded_bytes,uploaded_at
) VALUES($1::uuid,$2,$3,$4,$5,$6,NULL,0,NULL)`)
		if err != nil {
			return result{}, err
		}
		defer statement.Close()
		for _, part := range session.Parts {
			if _, err := statement.ExecContext(ctx, session.ID, part.Number, part.Offset, part.ExpectedBytes, part.ObjectKey, part.State); err != nil {
				return result{}, fmt.Errorf("insert upload part %d: %w", part.Number, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return result{}, err
		}
		return result{Session: cloneUploadSession(session), Created: true}, nil
	})
	return value.Session, value.Created, err
}

func (r *PostgreSQLUploadSessionRepository) Get(ctx context.Context, identifier string) (UploadSession, error) {
	if r == nil || r.DB == nil {
		return UploadSession{}, errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return UploadSession{}, err
	}
	defer tx.Rollback()
	value, err := loadUploadSessionByID(ctx, tx, identifier, false)
	if err != nil {
		return UploadSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return UploadSession{}, err
	}
	return value, nil
}

func (r *PostgreSQLUploadSessionRepository) RecordPart(ctx context.Context, identifier string, number int, size int64, checksum string, now time.Time) (UploadSession, bool, error) {
	if r == nil || r.DB == nil {
		return UploadSession{}, false, errors.New("database is required")
	}
	type result struct {
		Session UploadSession
		Changed bool
	}
	value, err := pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (result, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return result{}, err
		}
		defer tx.Rollback()
		current, err := loadUploadSessionByID(ctx, tx, identifier, true)
		if err != nil {
			return result{}, err
		}
		next, changed, err := current.RecordPart(number, size, checksum, current.Version, now)
		if err != nil {
			return result{}, err
		}
		if !changed {
			if err := tx.Commit(); err != nil {
				return result{}, err
			}
			return result{Session: current}, nil
		}
		part := next.Parts[number-1]
		updatePart, err := tx.ExecContext(ctx, `
UPDATE audience_import_upload_parts
SET state='UPLOADED',sha256=$4,uploaded_bytes=$5,uploaded_at=$6
WHERE session_id=$1::uuid AND part_number=$2 AND state='PENDING' AND expected_bytes=$3`,
			identifier, number, part.ExpectedBytes, part.SHA256, part.UploadedBytes, part.UploadedAt)
		if err != nil {
			return result{}, err
		}
		partRows, err := updatePart.RowsAffected()
		if err != nil {
			return result{}, err
		}
		if partRows != 1 {
			return result{}, ErrUploadPartConflict
		}
		updateSession, err := tx.ExecContext(ctx, `
UPDATE audience_import_upload_sessions
SET state=$2,uploaded_bytes=$3,uploaded_parts=$4,version=$5,updated_at=$6
WHERE id=$1::uuid AND version=$7 AND state IN ('CREATED','UPLOADING')`,
			identifier, next.State, next.UploadedBytes, next.UploadedParts, next.Version, next.UpdatedAt, current.Version)
		if err != nil {
			return result{}, err
		}
		sessionRows, err := updateSession.RowsAffected()
		if err != nil {
			return result{}, err
		}
		if sessionRows != 1 {
			return result{}, ErrUploadSessionConflict
		}
		if err := tx.Commit(); err != nil {
			return result{}, err
		}
		return result{Session: next, Changed: true}, nil
	})
	return value.Session, value.Changed, err
}

func (r *PostgreSQLUploadSessionRepository) Complete(ctx context.Context, identifier string, expectedVersion int64, now time.Time) (UploadSession, error) {
	if r == nil || r.DB == nil {
		return UploadSession{}, errors.New("database is required")
	}
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (UploadSession, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return UploadSession{}, err
		}
		defer tx.Rollback()
		current, err := loadUploadSessionByID(ctx, tx, identifier, true)
		if err != nil {
			return UploadSession{}, err
		}
		next, err := current.Complete(expectedVersion, now)
		if err != nil {
			return UploadSession{}, err
		}
		update, err := tx.ExecContext(ctx, `
UPDATE audience_import_upload_sessions
SET state='UPLOADED',uploaded_bytes=$2,uploaded_parts=$3,version=$4,updated_at=$5
WHERE id=$1::uuid AND version=$6 AND state IN ('CREATED','UPLOADING')`,
			identifier, next.UploadedBytes, next.UploadedParts, next.Version, next.UpdatedAt, expectedVersion)
		if err != nil {
			return UploadSession{}, err
		}
		rows, err := update.RowsAffected()
		if err != nil {
			return UploadSession{}, err
		}
		if rows != 1 {
			return UploadSession{}, ErrUploadSessionConflict
		}
		if err := tx.Commit(); err != nil {
			return UploadSession{}, err
		}
		return next, nil
	})
}

func (r *PostgreSQLUploadSessionRepository) Abort(ctx context.Context, identifier, reason string, expectedVersion int64, now time.Time) (UploadSession, error) {
	if r == nil || r.DB == nil {
		return UploadSession{}, errors.New("database is required")
	}
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (UploadSession, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return UploadSession{}, err
		}
		defer tx.Rollback()
		current, err := loadUploadSessionByID(ctx, tx, identifier, true)
		if err != nil {
			return UploadSession{}, err
		}
		next, err := current.Abort(reason, expectedVersion, now)
		if err != nil {
			return UploadSession{}, err
		}
		update, err := tx.ExecContext(ctx, `
UPDATE audience_import_upload_sessions
SET state='ABORTED',failure_reason=$2,version=$3,updated_at=$4
WHERE id=$1::uuid AND version=$5 AND state IN ('CREATED','UPLOADING','UPLOADED')`,
			identifier, next.FailureReason, next.Version, next.UpdatedAt, expectedVersion)
		if err != nil {
			return UploadSession{}, err
		}
		rows, err := update.RowsAffected()
		if err != nil {
			return UploadSession{}, err
		}
		if rows != 1 {
			return UploadSession{}, ErrUploadSessionConflict
		}
		if err := tx.Commit(); err != nil {
			return UploadSession{}, err
		}
		return next, nil
	})
}

func (r *PostgreSQLUploadSessionRepository) ClaimReadyFinalisations(ctx context.Context, workerID string, leaseDuration time.Duration, limit int, now time.Time) ([]UploadFinalisationWork, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil, errors.New("finalisation worker ID is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() ([]UploadFinalisationWork, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		rows, err := tx.QueryContext(ctx, `
SELECT id::text
FROM audience_import_upload_sessions
WHERE state='UPLOADED'
   OR (state='FINALISING' AND finaliser_lease_expires_at IS NOT NULL AND finaliser_lease_expires_at<=$1)
ORDER BY updated_at,id
FOR UPDATE SKIP LOCKED
LIMIT $2`, now.UTC(), limit)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, limit)
		for rows.Next() {
			var identifier string
			if err := rows.Scan(&identifier); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, identifier)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		items := make([]UploadFinalisationWork, 0, len(ids))
		for _, identifier := range ids {
			current, err := loadUploadSessionByID(ctx, tx, identifier, true)
			if err != nil {
				return nil, err
			}
			next, lease, err := current.ClaimFinalisation(workerID, leaseDuration, now)
			if err != nil {
				return nil, err
			}
			result, err := tx.ExecContext(ctx, `
UPDATE audience_import_upload_sessions
SET state='FINALISING',
    finaliser_lease_owner=$2,
    finaliser_lease_version=$3,
    finaliser_lease_expires_at=$4,
    version=$5,
    updated_at=$6
WHERE id=$1::uuid
  AND version=$7
  AND (
    state='UPLOADED'
    OR (state='FINALISING' AND finaliser_lease_expires_at<=$6)
  )`,
				identifier, next.FinaliserLeaseOwner, next.FinaliserLeaseVersion, next.FinaliserLeaseExpiresAt,
				next.Version, next.UpdatedAt, current.Version)
			if err != nil {
				return nil, err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return nil, err
			}
			if count != 1 {
				return nil, ErrUploadFinalisationLease
			}
			items = append(items, UploadFinalisationWork{Session: next, Lease: lease})
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return items, nil
	})
}

func (r *PostgreSQLUploadSessionRepository) RenewUploadFinalisation(ctx context.Context, identifier string, lease FinalisationLease, leaseDuration time.Duration, now time.Time) (UploadSession, FinalisationLease, error) {
	if r == nil || r.DB == nil {
		return UploadSession{}, FinalisationLease{}, errors.New("database is required")
	}
	type result struct {
		Session UploadSession
		Lease   FinalisationLease
	}
	value, err := pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (result, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return result{}, err
		}
		defer tx.Rollback()
		current, err := loadUploadSessionByID(ctx, tx, identifier, true)
		if err != nil {
			return result{}, err
		}
		next, renewed, err := current.RenewFinalisation(lease, leaseDuration, now)
		if err != nil {
			return result{}, err
		}
		update, err := tx.ExecContext(ctx, `
UPDATE audience_import_upload_sessions
SET finaliser_lease_expires_at=$2,version=$3,updated_at=$4
WHERE id=$1::uuid
  AND version=$5
  AND state='FINALISING'
  AND finaliser_lease_owner=$6
  AND finaliser_lease_version=$7
  AND finaliser_lease_expires_at=$8
  AND finaliser_lease_expires_at>$4`,
			identifier, renewed.ExpiresAt, next.Version, next.UpdatedAt, current.Version,
			lease.Owner, lease.Version, lease.ExpiresAt.UTC())
		if err != nil {
			return result{}, err
		}
		count, err := update.RowsAffected()
		if err != nil {
			return result{}, err
		}
		if count != 1 {
			return result{}, ErrUploadFinalisationLease
		}
		if err := tx.Commit(); err != nil {
			return result{}, err
		}
		return result{Session: next, Lease: renewed}, nil
	})
	return value.Session, value.Lease, err
}

func (r *PostgreSQLUploadSessionRepository) CompleteUploadFinalisation(ctx context.Context, identifier, importID, wholeFileSHA256, mediaType string, lease FinalisationLease, now time.Time) (UploadSession, error) {
	if r == nil || r.DB == nil {
		return UploadSession{}, errors.New("database is required")
	}
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (UploadSession, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return UploadSession{}, err
		}
		defer tx.Rollback()
		current, err := loadUploadSessionByID(ctx, tx, identifier, true)
		if err != nil {
			return UploadSession{}, err
		}
		next, err := current.CompleteFinalisation(importID, wholeFileSHA256, mediaType, lease, now)
		if err != nil {
			return UploadSession{}, err
		}
		result, err := tx.ExecContext(ctx, `
UPDATE audience_import_upload_sessions
SET state='IMPORT_CREATED',
    final_sha256=$2,
    detected_media_type=$3,
    linked_import_id=$4::uuid,
    failure_reason=NULL,
    finaliser_lease_owner=NULL,
    finaliser_lease_expires_at=NULL,
    version=$5,
    updated_at=$6
WHERE id=$1::uuid
  AND version=$7
  AND state='FINALISING'
  AND finaliser_lease_owner=$8
  AND finaliser_lease_version=$9
  AND finaliser_lease_expires_at=$10
  AND finaliser_lease_expires_at>$6`,
			identifier, next.FinalSHA256, next.DetectedMediaType, next.LinkedImportID, next.Version,
			next.UpdatedAt, current.Version, lease.Owner, lease.Version, lease.ExpiresAt.UTC())
		if err != nil {
			return UploadSession{}, fmt.Errorf("complete upload finalisation: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return UploadSession{}, err
		}
		if count != 1 {
			return UploadSession{}, ErrUploadFinalisationLease
		}
		if err := tx.Commit(); err != nil {
			return UploadSession{}, err
		}
		return next, nil
	})
}

func (r *PostgreSQLUploadSessionRepository) FailUploadFinalisation(ctx context.Context, identifier, reason string, lease FinalisationLease, now time.Time) (UploadSession, error) {
	if r == nil || r.DB == nil {
		return UploadSession{}, errors.New("database is required")
	}
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (UploadSession, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return UploadSession{}, err
		}
		defer tx.Rollback()
		current, err := loadUploadSessionByID(ctx, tx, identifier, true)
		if err != nil {
			return UploadSession{}, err
		}
		next, err := current.FailFinalisation(reason, lease, now)
		if err != nil {
			return UploadSession{}, err
		}
		result, err := tx.ExecContext(ctx, `
UPDATE audience_import_upload_sessions
SET state='FAILED',
    failure_reason=$2,
    finaliser_lease_owner=NULL,
    finaliser_lease_expires_at=NULL,
    version=$3,
    updated_at=$4
WHERE id=$1::uuid
  AND version=$5
  AND state='FINALISING'
  AND finaliser_lease_owner=$6
  AND finaliser_lease_version=$7
  AND finaliser_lease_expires_at=$8
  AND finaliser_lease_expires_at>$4`,
			identifier, next.FailureReason, next.Version, next.UpdatedAt, current.Version,
			lease.Owner, lease.Version, lease.ExpiresAt.UTC())
		if err != nil {
			return UploadSession{}, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return UploadSession{}, err
		}
		if count != 1 {
			return UploadSession{}, ErrUploadFinalisationLease
		}
		if err := tx.Commit(); err != nil {
			return UploadSession{}, err
		}
		return next, nil
	})
}

type uploadQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

const uploadSessionSelect = `SELECT
 id::text,organisation_id::text,consent_review_id::text,purpose_id::text,upper(channel),wording_version,
 source_name,coalesce(source_system,''),coalesce(default_country_iso2::text,''),original_filename,
 template_version,coalesce(mapping_definition_id::text,''),mapping,update_policy,uploaded_by::text,
 client_request_id,expected_bytes,uploaded_bytes,part_size,part_count,uploaded_parts,state,version,
 coalesce(failure_reason,''),coalesce(final_sha256,''),coalesce(detected_media_type,''),
 coalesce(linked_import_id::text,''),coalesce(finaliser_lease_owner,''),finaliser_lease_version,
 finaliser_lease_expires_at,expires_at,created_at,updated_at
FROM audience_import_upload_sessions`

func loadUploadSessionByID(ctx context.Context, q uploadQueryer, identifier string, lock bool) (UploadSession, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	value, err := scanUploadSession(q.QueryRowContext(ctx, uploadSessionSelect+" WHERE id=$1::uuid"+suffix, identifier))
	if errors.Is(err, sql.ErrNoRows) {
		return UploadSession{}, ErrUploadSessionNotFound
	}
	if err != nil {
		return UploadSession{}, err
	}
	return loadUploadParts(ctx, q, value, lock)
}

func loadUploadSessionByRequest(ctx context.Context, q uploadQueryer, organisationID, requestID string, lock bool) (UploadSession, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	value, err := scanUploadSession(q.QueryRowContext(ctx, uploadSessionSelect+" WHERE organisation_id=$1::uuid AND client_request_id=$2"+suffix, organisationID, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return UploadSession{}, ErrUploadSessionNotFound
	}
	if err != nil {
		return UploadSession{}, err
	}
	return loadUploadParts(ctx, q, value, lock)
}

type uploadSessionScanner interface{ Scan(...any) error }

func scanUploadSession(row uploadSessionScanner) (UploadSession, error) {
	var value UploadSession
	var mapping []byte
	var state, policy string
	var finaliserLeaseExpiresAt sql.NullTime
	if err := row.Scan(
		&value.ID, &value.OrganisationID, &value.ConsentReviewID, &value.PurposeID, &value.Channel,
		&value.WordingVersion, &value.SourceName, &value.SourceSystem, &value.DefaultCountryISO2,
		&value.OriginalFilename, &value.TemplateVersion, &value.MappingDefinitionID, &mapping, &policy,
		&value.UploadedBy, &value.ClientRequestID, &value.ExpectedBytes, &value.UploadedBytes,
		&value.PartSize, &value.PartCount, &value.UploadedParts, &state, &value.Version,
		&value.FailureReason, &value.FinalSHA256, &value.DetectedMediaType, &value.LinkedImportID,
		&value.FinaliserLeaseOwner, &value.FinaliserLeaseVersion, &finaliserLeaseExpiresAt,
		&value.ExpiresAt, &value.CreatedAt, &value.UpdatedAt,
	); err != nil {
		return UploadSession{}, err
	}
	if err := json.Unmarshal(mapping, &value.Mapping); err != nil {
		return UploadSession{}, fmt.Errorf("decode upload session mapping: %w", err)
	}
	value.State = UploadSessionState(state)
	value.UpdatePolicy = UpdatePolicy(policy)
	value.Channel = strings.ToUpper(strings.TrimSpace(value.Channel))
	value.DefaultCountryISO2 = strings.ToUpper(strings.TrimSpace(value.DefaultCountryISO2))
	if finaliserLeaseExpiresAt.Valid {
		expiresAt := finaliserLeaseExpiresAt.Time.UTC()
		value.FinaliserLeaseExpiresAt = &expiresAt
	}
	return value, nil
}

func loadUploadParts(ctx context.Context, q uploadQueryer, session UploadSession, lock bool) (UploadSession, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	rows, err := q.QueryContext(ctx, `
SELECT part_number,offset_bytes,expected_bytes,object_key,state,coalesce(sha256,''),uploaded_bytes,uploaded_at
FROM audience_import_upload_parts
WHERE session_id=$1::uuid ORDER BY part_number`+suffix, session.ID)
	if err != nil {
		return UploadSession{}, err
	}
	defer rows.Close()
	parts := make([]UploadPart, 0, session.PartCount)
	for rows.Next() {
		var part UploadPart
		var state string
		var uploadedAt sql.NullTime
		if err := rows.Scan(&part.Number, &part.Offset, &part.ExpectedBytes, &part.ObjectKey, &state, &part.SHA256, &part.UploadedBytes, &uploadedAt); err != nil {
			return UploadSession{}, err
		}
		part.State = UploadPartState(state)
		if uploadedAt.Valid {
			value := uploadedAt.Time.UTC()
			part.UploadedAt = &value
		}
		parts = append(parts, part)
	}
	if err := rows.Err(); err != nil {
		return UploadSession{}, err
	}
	if len(parts) != session.PartCount {
		return UploadSession{}, fmt.Errorf("upload session %s part manifest is incomplete: have %d want %d", session.ID, len(parts), session.PartCount)
	}
	session.Parts = parts
	return session, nil
}
