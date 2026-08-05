package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type PostgreSQLConflictRepository struct{ DB *sql.DB }

func (r *PostgreSQLConflictRepository) List(ctx context.Context, importID string, status ConflictStatus, limit int) ([]ProfileConflict, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,audience_import_id::text,contact_id::text,masked_msisdn,field_name,coalesce(existing_value,''),coalesce(incoming_value,''),status,coalesce(resolution,''),coalesce(reason,''),coalesce(resolved_by::text,''),resolved_at,version,created_at FROM audience_profile_conflicts WHERE audience_import_id=$1::uuid AND status=$2 ORDER BY created_at,id LIMIT $3`, importID, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list audience profile conflicts: %w", err)
	}
	defer rows.Close()
	out := make([]ProfileConflict, 0)
	for rows.Next() {
		var value ProfileConflict
		var resolvedAt sql.NullTime
		var resolution string
		if err := rows.Scan(&value.ID, &value.AudienceImportID, &value.ContactID, &value.MaskedMSISDN, &value.Field, &value.ExistingValue, &value.IncomingValue, &value.Status, &resolution, &value.Reason, &value.ResolvedBy, &resolvedAt, &value.Version, &value.CreatedAt); err != nil {
			return nil, err
		}
		value.Resolution = ConflictResolution(resolution)
		if resolvedAt.Valid {
			t := resolvedAt.Time
			value.ResolvedAt = &t
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (r *PostgreSQLConflictRepository) Resolve(ctx context.Context, conflictID string, resolution ConflictResolution, reason, actor string, expectedVersion int64, now time.Time) (ProfileConflict, error) {
	if r == nil || r.DB == nil {
		return ProfileConflict{}, errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ProfileConflict{}, err
	}
	defer tx.Rollback()
	var value ProfileConflict
	var status string
	err = tx.QueryRowContext(ctx, `SELECT id::text,audience_import_id::text,contact_id::text,masked_msisdn,field_name,coalesce(existing_value,''),coalesce(incoming_value,''),status,version,created_at FROM audience_profile_conflicts WHERE id=$1::uuid FOR UPDATE`, conflictID).Scan(&value.ID, &value.AudienceImportID, &value.ContactID, &value.MaskedMSISDN, &value.Field, &value.ExistingValue, &value.IncomingValue, &status, &value.Version, &value.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ProfileConflict{}, ErrConflictNotFound
	}
	if err != nil {
		return ProfileConflict{}, err
	}
	if value.Version != expectedVersion {
		return ProfileConflict{}, ErrConflictVersion
	}
	if ConflictStatus(status) != ConflictPending {
		return ProfileConflict{}, ErrConflictState
	}
	if resolution == ResolutionUseIncoming {
		column := map[string]string{"country_id": "country_id", "state_id": "state_id", "lga_id": "lga_id", "reported_age": "reported_age", "gender_code": "gender_code"}[value.Field]
		if column == "" {
			return ProfileConflict{}, errors.New("unsupported conflict field")
		}
		query := fmt.Sprintf(`UPDATE contacts SET %s=CASE WHEN $2='' THEN NULL ELSE $2 END,updated_at=$3 WHERE id=$1::uuid`, column)
		if value.Field == "country_id" || value.Field == "state_id" || value.Field == "lga_id" {
			query = fmt.Sprintf(`UPDATE contacts SET %s=CASE WHEN $2='' THEN NULL ELSE $2::uuid END,updated_at=$3 WHERE id=$1::uuid`, column)
		}
		if value.Field == "reported_age" {
			query = `UPDATE contacts SET reported_age=CASE WHEN $2='' THEN NULL ELSE $2::smallint END,updated_at=$3 WHERE id=$1::uuid`
		}
		if _, err := tx.ExecContext(ctx, query, value.ContactID, value.IncomingValue, now); err != nil {
			return ProfileConflict{}, fmt.Errorf("apply incoming conflict value: %w", err)
		}
	}
	err = tx.QueryRowContext(ctx, `UPDATE audience_profile_conflicts SET status='RESOLVED',resolution=$2,reason=$3,resolved_by=$4::uuid,resolved_at=$5,version=version+1 WHERE id=$1::uuid AND version=$6 RETURNING status,resolution,reason,resolved_by::text,resolved_at,version`, conflictID, resolution, reason, actor, now, expectedVersion).Scan(&value.Status, &value.Resolution, &value.Reason, &value.ResolvedBy, &value.ResolvedAt, &value.Version)
	if err != nil {
		return ProfileConflict{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProfileConflict{}, err
	}
	return value, nil
}
