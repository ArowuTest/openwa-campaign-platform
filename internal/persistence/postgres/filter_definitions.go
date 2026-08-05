package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	audiencefilter "campaign-platform/internal/audience/filter"
)

type FilterDefinitionStore struct{ DB *sql.DB }

const filterDefinitionSelect = `SELECT code,display_name,coalesce(description,''),data_type,to_jsonb(allowed_operators)::text,core,filterable,reportable,sensitive,coalesce(required_permission,''),storage,coalesce(queryable_field,''),coalesce(attribute_value_column,''),index_strategy,allowed_values::text,display_order,active,version,created_at,updated_at FROM attribute_definitions`

func (s *FilterDefinitionStore) List(ctx context.Context) ([]audiencefilter.Definition, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := s.DB.QueryContext(ctx, filterDefinitionSelect+` ORDER BY display_order,code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []audiencefilter.Definition{}
	for rows.Next() {
		item, err := scanFilterDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *FilterDefinitionStore) Get(ctx context.Context, code string) (audiencefilter.Definition, error) {
	if s == nil || s.DB == nil {
		return audiencefilter.Definition{}, errors.New("database is required")
	}
	item, err := scanFilterDefinition(s.DB.QueryRowContext(ctx, filterDefinitionSelect+` WHERE code=$1`, strings.ToUpper(strings.TrimSpace(code))))
	if errors.Is(err, sql.ErrNoRows) {
		return audiencefilter.Definition{}, audiencefilter.ErrDefinitionNotFound
	}
	return item, err
}
func (s *FilterDefinitionStore) Generation(ctx context.Context) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, errors.New("database is required")
	}
	var generation int64
	err := s.DB.QueryRowContext(ctx, `SELECT generation FROM filter_registry_generation WHERE singleton=true`).Scan(&generation)
	return generation, err
}
func (s *FilterDefinitionStore) Create(ctx context.Context, d audiencefilter.Definition, actor, reason string) (audiencefilter.Definition, error) {
	if s == nil || s.DB == nil {
		return audiencefilter.Definition{}, errors.New("database is required")
	}
	operators, _ := json.Marshal(d.Operators)
	allowed, _ := json.Marshal(d.AllowedValues)
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return audiencefilter.Definition{}, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO attribute_definitions(code,display_name,description,data_type,allowed_operators,filterable,reportable,sensitive,required_permission,index_strategy,display_order,active,core,storage,queryable_field,attribute_value_column,allowed_values,version,created_at,updated_at) VALUES($1,$2,NULLIF($3,''),$4,ARRAY(SELECT jsonb_array_elements_text($5::jsonb)),$6,$7,$8,NULLIF($9,''),$10,$11,$12,false,'contact_attribute',NULL,$13,$14::jsonb,1,$15,$15) RETURNING id`, d.Code, d.DisplayName, d.Description, d.DataType, string(operators), d.Filterable, d.Reportable, d.Sensitive, d.RequiresPermission, d.IndexStrategy, d.DisplayOrder, d.Active, d.AttributeValueColumn, string(allowed), d.CreatedAt).Scan(&id)
	if isUniqueViolation(err) {
		return audiencefilter.Definition{}, audiencefilter.ErrDefinitionDuplicate
	}
	if err != nil {
		return audiencefilter.Definition{}, err
	}
	if err = insertFilterHistory(ctx, tx, id, d, actor, reason); err != nil {
		return audiencefilter.Definition{}, err
	}
	if err = tx.Commit(); err != nil {
		return audiencefilter.Definition{}, err
	}
	return s.Get(ctx, d.Code)
}
func (s *FilterDefinitionStore) CompareAndSwap(ctx context.Context, d audiencefilter.Definition, expected int64, actor, reason string) (audiencefilter.Definition, error) {
	operators, _ := json.Marshal(d.Operators)
	allowed, _ := json.Marshal(d.AllowedValues)
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return audiencefilter.Definition{}, err
	}
	defer tx.Rollback()
	var id string
	var version int64
	err = tx.QueryRowContext(ctx, `UPDATE attribute_definitions SET display_name=$3,description=NULLIF($4,''),allowed_operators=ARRAY(SELECT jsonb_array_elements_text($5::jsonb)),filterable=$6,reportable=$7,sensitive=$8,required_permission=NULLIF($9,''),index_strategy=$10,display_order=$11,active=$12,allowed_values=$13::jsonb WHERE code=$1 AND version=$2 RETURNING id,version`, d.Code, expected, d.DisplayName, d.Description, string(operators), d.Filterable, d.Reportable, d.Sensitive, d.RequiresPermission, d.IndexStrategy, d.DisplayOrder, d.Active, string(allowed)).Scan(&id, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return audiencefilter.Definition{}, audiencefilter.ErrDefinitionConflict
	}
	if err != nil {
		return audiencefilter.Definition{}, err
	}
	d.Version = version
	if err = insertFilterHistory(ctx, tx, id, d, actor, reason); err != nil {
		return audiencefilter.Definition{}, err
	}
	if err = tx.Commit(); err != nil {
		return audiencefilter.Definition{}, err
	}
	return s.Get(ctx, d.Code)
}
func insertFilterHistory(ctx context.Context, tx *sql.Tx, id string, d audiencefilter.Definition, actor, reason string) error {
	snapshot, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO filter_definition_change_history(definition_id,definition_version,actor_id,reason,snapshot) VALUES($1,$2,$3,$4,$5::jsonb)`, id, d.Version, actor, reason, string(snapshot))
	return err
}
func scanFilterDefinition(row scanner) (audiencefilter.Definition, error) {
	var d audiencefilter.Definition
	var dataType, storage, operatorsJSON, valuesJSON string
	err := row.Scan(&d.Code, &d.DisplayName, &d.Description, &dataType, &operatorsJSON, &d.Core, &d.Filterable, &d.Reportable, &d.Sensitive, &d.RequiresPermission, &storage, &d.QueryableField, &d.AttributeValueColumn, &d.IndexStrategy, &valuesJSON, &d.DisplayOrder, &d.Active, &d.Version, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	d.DataType = audiencefilter.DataType(dataType)
	d.Storage = audiencefilter.Storage(storage)
	if err = json.Unmarshal([]byte(operatorsJSON), &d.Operators); err != nil {
		return d, fmt.Errorf("decode filter operators: %w", err)
	}
	if err = json.Unmarshal([]byte(valuesJSON), &d.AllowedValues); err != nil {
		return d, fmt.Errorf("decode allowed values: %w", err)
	}
	return d, nil
}
