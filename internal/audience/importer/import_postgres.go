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

type PostgreSQLImportRepository struct{ DB *sql.DB }

func (r *PostgreSQLImportRepository) Create(ctx context.Context, batch ImportBatch) (ImportBatch, bool, error) {
	if r == nil || r.DB == nil {
		return ImportBatch{}, false, errors.New("database is required")
	}
	type result struct {
		Batch   ImportBatch
		Created bool
	}
	value, err := pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (result, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return result{}, err
		}
		defer tx.Rollback()
		existing, err := scanImport(tx.QueryRowContext(ctx, importSelect+` WHERE organisation_id=$1::uuid AND client_request_id=$2 FOR SHARE`, batch.OrganisationID, batch.ClientRequestID))
		if err == nil {
			if !sameImportRequest(existing, batch) {
				return result{}, ErrImportReplayConflict
			}
			if err := tx.Commit(); err != nil {
				return result{}, err
			}
			return result{Batch: existing}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return result{}, err
		}
		existing, err = scanImport(tx.QueryRowContext(ctx, importSelect+` WHERE organisation_id=$1::uuid AND file_sha256=$2 FOR SHARE`, batch.OrganisationID, batch.FileSHA256))
		if err == nil {
			if err := tx.Commit(); err != nil {
				return result{}, err
			}
			return result{Batch: existing}, ErrDuplicateImportFile
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return result{}, err
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO audience_imports(
 id,organisation_id,consent_review_id,purpose_id,channel,wording_version,source_name,source_system,default_country_iso2,
 object_key,original_filename,detected_media_type,file_sha256,byte_size,template_version,mapping_definition_id,mapping,update_policy,status,
 malware_scan_status,content_signature_valid,client_request_id,uploaded_by,source_expires_at,version,created_at,updated_at
) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,NULLIF($8,''),NULLIF($9,'')::char(2),$10,$11,$12,$13,$14,$15,NULLIF($16,'')::uuid,$17::jsonb,$18,$19,$20,$21,$22,NULLIF($23,'')::uuid,$24,$25,$26,$26)`,
			batch.ID, batch.OrganisationID, batch.ConsentReviewID, batch.PurposeID, batch.Channel, batch.WordingVersion, batch.SourceName, batch.SourceSystem, batch.DefaultCountryISO2,
			batch.ObjectKey, batch.OriginalFilename, batch.DetectedMediaType, batch.FileSHA256, batch.ByteSize, batch.TemplateVersion, batch.MappingDefinitionID, []byte(batch.Mapping), batch.UpdatePolicy, batch.Status,
			batch.MalwareStatus, batch.ContentSignatureValid, batch.ClientRequestID, batch.UploadedBy, batch.SourceExpiresAt, batch.Version, batch.CreatedAt)
		if err != nil {
			return result{}, fmt.Errorf("insert audience import: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return result{}, err
		}
		return result{Batch: batch, Created: true}, nil
	})
	if err != nil {
		return value.Batch, value.Created, err
	}
	return value.Batch, value.Created, nil
}

func (r *PostgreSQLImportRepository) Get(ctx context.Context, identifier string) (ImportBatch, error) {
	if r == nil || r.DB == nil {
		return ImportBatch{}, errors.New("database is required")
	}
	value, err := scanImport(r.DB.QueryRowContext(ctx, importSelect+` WHERE id=$1::uuid`, identifier))
	if errors.Is(err, sql.ErrNoRows) {
		return ImportBatch{}, ErrImportNotFound
	}
	return value, err
}

func (r *PostgreSQLImportRepository) RecordScan(ctx context.Context, identifier string, status MalwareStatus, signatureValid bool, reason string, expectedVersion int64, now time.Time) (ImportBatch, error) {
	if r == nil || r.DB == nil {
		return ImportBatch{}, errors.New("database is required")
	}
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (ImportBatch, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return ImportBatch{}, err
		}
		defer tx.Rollback()
		current, err := scanImport(tx.QueryRowContext(ctx, importSelect+` WHERE id=$1::uuid FOR UPDATE`, identifier))
		if errors.Is(err, sql.ErrNoRows) {
			return ImportBatch{}, ErrImportNotFound
		}
		if err != nil {
			return ImportBatch{}, err
		}
		if isExactScanReplay(current, status, signatureValid, reason, expectedVersion) {
			if err := tx.Commit(); err != nil {
				return ImportBatch{}, err
			}
			return current, nil
		}
		next, err := current.Scan(status, signatureValid, reason, expectedVersion, now)
		if err != nil {
			return ImportBatch{}, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE audience_imports SET status=$2,malware_scan_status=$3,content_signature_valid=$4,failure_reason=NULLIF($5,''),version=$6,updated_at=$7 WHERE id=$1::uuid AND version=$8`, identifier, next.Status, next.MalwareStatus, next.ContentSignatureValid, next.FailureReason, next.Version, next.UpdatedAt, expectedVersion)
		if err != nil {
			return ImportBatch{}, err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return ImportBatch{}, err
		}
		if rows != 1 {
			return ImportBatch{}, ErrImportConflict
		}
		if err := tx.Commit(); err != nil {
			return ImportBatch{}, err
		}
		return next, nil
	})
}

func (r *PostgreSQLImportRepository) Approve(ctx context.Context, identifier, actorID string, expectedVersion int64, now time.Time) (ImportBatch, error) {
	if r == nil || r.DB == nil {
		return ImportBatch{}, errors.New("database is required")
	}
	return pgretry.RetryValue(ctx, pgretry.DefaultRetryPolicy(), func() (ImportBatch, error) {
		tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return ImportBatch{}, err
		}
		defer tx.Rollback()
		current, err := scanImport(tx.QueryRowContext(ctx, importSelect+` WHERE id=$1::uuid FOR UPDATE`, identifier))
		if errors.Is(err, sql.ErrNoRows) {
			return ImportBatch{}, ErrImportNotFound
		}
		if err != nil {
			return ImportBatch{}, err
		}
		if isExactApprovalReplay(current, actorID, expectedVersion) {
			if err := tx.Commit(); err != nil {
				return ImportBatch{}, err
			}
			return current, nil
		}
		var consentValid bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM consent_reviews cr JOIN consent_purposes cp
   ON cp.consent_review_id=cr.id AND cp.organisation_id=cr.organisation_id
 WHERE cr.id=$1::uuid AND cr.organisation_id=$2::uuid AND cr.status='APPROVED'
   AND cr.expires_at>=$3 AND cp.id=$4::uuid AND cp.active
   AND upper(cp.channel)=$5 AND cp.wording_version=$6
)`, current.ConsentReviewID, current.OrganisationID, now.UTC(), current.PurposeID, current.Channel, current.WordingVersion).Scan(&consentValid)
		if err != nil {
			return ImportBatch{}, err
		}
		next, err := current.Approve(actorID, expectedVersion, consentValid, now)
		if err != nil {
			return ImportBatch{}, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE audience_imports SET status='APPROVED',approved_by=NULLIF($2,'')::uuid,approved_at=$3,version=$4,updated_at=$3 WHERE id=$1::uuid AND version=$5 AND status='PREVIEW_READY'`, identifier, actorID, next.ApprovedAt, next.Version, expectedVersion)
		if err != nil {
			return ImportBatch{}, err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return ImportBatch{}, err
		}
		if rows != 1 {
			return ImportBatch{}, ErrImportConflict
		}
		if err := tx.Commit(); err != nil {
			return ImportBatch{}, err
		}
		return next, nil
	})
}

const importSelect = `SELECT id::text,organisation_id::text,consent_review_id::text,purpose_id::text,upper(channel),wording_version,source_name,coalesce(source_system,''),coalesce(default_country_iso2::text,''),object_key,original_filename,coalesce(detected_media_type,''),file_sha256,byte_size,coalesce(template_version,''),coalesce(mapping_definition_id::text,''),mapping,coalesce(update_policy,'NEWEST_SOURCE'),status,coalesce(malware_scan_status,'PENDING'),coalesce(content_signature_valid,false),coalesce(client_request_id,''),uploaded_rows,valid_rows,invalid_rows,duplicate_rows,suppressed_rows,inserted_contacts,updated_contacts,coalesce(uploaded_by::text,''),coalesce(approved_by::text,''),approved_at,coalesce(failure_reason,''),source_expires_at,source_deleted_at,rolled_back_at,coalesce(rolled_back_by::text,''),coalesce(rollback_reason,''),version,created_at,updated_at FROM audience_imports`

type importScanner interface{ Scan(...any) error }

func scanImport(row importScanner) (ImportBatch, error) {
	var value ImportBatch
	var status, malware, updatePolicy string
	var approvedAt, sourceExpiresAt, sourceDeletedAt, rolledBackAt sql.NullTime
	var mapping []byte
	err := row.Scan(&value.ID, &value.OrganisationID, &value.ConsentReviewID, &value.PurposeID, &value.Channel, &value.WordingVersion, &value.SourceName, &value.SourceSystem, &value.DefaultCountryISO2, &value.ObjectKey, &value.OriginalFilename, &value.DetectedMediaType, &value.FileSHA256, &value.ByteSize, &value.TemplateVersion, &value.MappingDefinitionID, &mapping, &updatePolicy, &status, &malware, &value.ContentSignatureValid, &value.ClientRequestID, &value.UploadedRows, &value.ValidRows, &value.InvalidRows, &value.DuplicateRows, &value.SuppressedRows, &value.InsertedContacts, &value.UpdatedContacts, &value.UploadedBy, &value.ApprovedBy, &approvedAt, &value.FailureReason, &sourceExpiresAt, &sourceDeletedAt, &rolledBackAt, &value.RolledBackBy, &value.RollbackReason, &value.Version, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return ImportBatch{}, err
	}
	value.Mapping = append(json.RawMessage(nil), mapping...)
	value.Status = ImportStatus(status)
	value.MalwareStatus = MalwareStatus(malware)
	value.UpdatePolicy = UpdatePolicy(updatePolicy)
	value.Channel = strings.ToUpper(value.Channel)
	value.DefaultCountryISO2 = strings.ToUpper(strings.TrimSpace(value.DefaultCountryISO2))
	if approvedAt.Valid {
		v := approvedAt.Time
		value.ApprovedAt = &v
	}
	if sourceExpiresAt.Valid {
		v := sourceExpiresAt.Time
		value.SourceExpiresAt = &v
	}
	if sourceDeletedAt.Valid {
		v := sourceDeletedAt.Time
		value.SourceDeletedAt = &v
	}
	if rolledBackAt.Valid {
		v := rolledBackAt.Time
		value.RolledBackAt = &v
	}
	return value, nil
}
