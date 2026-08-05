package segment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type PostgreSQLDefinitionRepository struct{ DB *sql.DB }

func (r *PostgreSQLDefinitionRepository) Create(ctx context.Context, v Definition, reason string) (Definition, error) {
	if r == nil || r.DB == nil {
		return Definition{}, errors.New("database is required")
	}
	payload, err := json.Marshal(v.Definition)
	if err != nil {
		return Definition{}, err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Definition{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO segments(id,name,description,organisation_id,definition,definition_version,status,created_by,created_at,updated_at,updated_by,version) VALUES($1::uuid,$2,$3,$4::uuid,$5::jsonb,$6,$7,$8::uuid,$9,$10,$11::uuid,$12)`, v.ID, v.Name, v.Description, v.OrganisationID, payload, v.Version, v.Status, v.CreatedBy, v.CreatedAt, v.UpdatedAt, v.UpdatedBy, v.Version)
	if err != nil {
		return Definition{}, fmt.Errorf("create segment: %w", err)
	}
	if err := insertDefinitionVersion(ctx, tx, v, reason); err != nil {
		return Definition{}, err
	}
	if err := tx.Commit(); err != nil {
		return Definition{}, err
	}
	return v, nil
}
func (r *PostgreSQLDefinitionRepository) Get(ctx context.Context, id string) (Definition, error) {
	if r == nil || r.DB == nil {
		return Definition{}, errors.New("database is required")
	}
	var v Definition
	var payload []byte
	err := r.DB.QueryRowContext(ctx, `SELECT id::text,organisation_id::text,name,coalesce(description,''),definition,status,version,coalesce(created_by::text,''),coalesce(updated_by::text,''),created_at,updated_at FROM segments WHERE id=$1::uuid`, id).Scan(&v.ID, &v.OrganisationID, &v.Name, &v.Description, &payload, &v.Status, &v.Version, &v.CreatedBy, &v.UpdatedBy, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Definition{}, ErrDefinitionNotFound
	}
	if err != nil {
		return Definition{}, err
	}
	if err := json.Unmarshal(payload, &v.Definition); err != nil {
		return Definition{}, err
	}
	return v, nil
}
func (r *PostgreSQLDefinitionRepository) List(ctx context.Context, org string, limit int) ([]Definition, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,organisation_id::text,name,coalesce(description,''),definition,status,version,coalesce(created_by::text,''),coalesce(updated_by::text,''),created_at,updated_at FROM segments WHERE ($1='' OR organisation_id=NULLIF($1,'')::uuid) ORDER BY updated_at DESC,id LIMIT $2`, org, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Definition{}
	for rows.Next() {
		var v Definition
		var payload []byte
		if err := rows.Scan(&v.ID, &v.OrganisationID, &v.Name, &v.Description, &payload, &v.Status, &v.Version, &v.CreatedBy, &v.UpdatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &v.Definition); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgreSQLDefinitionRepository) Update(ctx context.Context, v Definition, expected int64, reason string) (Definition, error) {
	if r == nil || r.DB == nil {
		return Definition{}, errors.New("database is required")
	}
	payload, err := json.Marshal(v.Definition)
	if err != nil {
		return Definition{}, err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Definition{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE segments SET name=$2,description=$3,definition=$4::jsonb,definition_version=$5,status=$6,updated_by=$7::uuid,updated_at=$8,version=$5 WHERE id=$1::uuid AND version=$9`, v.ID, v.Name, v.Description, payload, v.Version, v.Status, v.UpdatedBy, v.UpdatedAt, expected)
	if err != nil {
		return Definition{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Definition{}, err
	}
	if n == 0 {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM segments WHERE id=$1::uuid)`, v.ID).Scan(&exists); err != nil {
			return Definition{}, err
		}
		if !exists {
			return Definition{}, ErrDefinitionNotFound
		}
		return Definition{}, ErrDefinitionConflict
	}
	if err := insertDefinitionVersion(ctx, tx, v, reason); err != nil {
		return Definition{}, err
	}
	if err := tx.Commit(); err != nil {
		return Definition{}, err
	}
	return v, nil
}
func (r *PostgreSQLDefinitionRepository) Versions(ctx context.Context, id string, limit int) ([]DefinitionVersion, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT segment_id::text,version,name,coalesce(description,''),definition,status,changed_by::text,reason,created_at FROM segment_definition_versions WHERE segment_id=$1::uuid ORDER BY version DESC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DefinitionVersion{}
	for rows.Next() {
		var v DefinitionVersion
		var payload []byte
		if err := rows.Scan(&v.SegmentID, &v.Version, &v.Name, &v.Description, &payload, &v.Status, &v.ChangedBy, &v.Reason, &v.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &v.Definition); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		var exists bool
		if err := r.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM segments WHERE id=$1::uuid)`, id).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrDefinitionNotFound
		}
	}
	return out, rows.Err()
}
func insertDefinitionVersion(ctx context.Context, tx *sql.Tx, v Definition, reason string) error {
	payload, err := json.Marshal(v.Definition)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO segment_definition_versions(segment_id,version,name,description,definition,status,changed_by,reason,created_at) VALUES($1::uuid,$2,$3,$4,$5::jsonb,$6,$7::uuid,$8,$9)`, v.ID, v.Version, v.Name, v.Description, payload, v.Status, v.UpdatedBy, reason, v.UpdatedAt)
	return err
}
