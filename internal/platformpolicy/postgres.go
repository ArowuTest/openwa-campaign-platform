package platformpolicy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"campaign-platform/internal/shared/id"
)

type PostgreSQLStore struct{ DB *sql.DB }

type rowScanner interface{ Scan(...any) error }

const configurationColumns = `id::text,configuration_key,scope_type,scope_id,value,value_checksum,status,effective_from,effective_to,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),coalesce(supersedes_id::text,''),coalesce(rollback_of_id::text,''),reason,version,created_at,updated_at`

func scanConfiguration(row rowScanner) (Configuration, error) {
	var v Configuration
	var raw []byte
	var effectiveTo sql.NullTime
	var status, scope string
	err := row.Scan(&v.ID, &v.Key, &scope, &v.ScopeID, &raw, &v.ValueChecksum, &status, &v.EffectiveFrom, &effectiveTo, &v.CreatedBy, &v.SubmittedBy, &v.ApprovedBy, &v.SupersedesID, &v.RollbackOfID, &v.Reason, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return Configuration{}, err
	}
	v.ScopeType = ScopeType(scope)
	v.Status = LifecycleStatus(status)
	v.Value = append([]byte(nil), raw...)
	if effectiveTo.Valid {
		t := effectiveTo.Time.UTC()
		v.EffectiveTo = &t
	}
	return v, nil
}

func (p *PostgreSQLStore) ListConfigurations(ctx context.Context, query ConfigurationQuery) ([]Configuration, error) {
	if p == nil || p.DB == nil {
		return nil, errors.New("database is required")
	}
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT `+configurationColumns+`
FROM platform_configurations
WHERE ($1='' OR configuration_key=$1)
  AND ($2='' OR scope_type=$2)
  AND ($3='' OR scope_id=$3)
  AND ($4='' OR status=$4)
ORDER BY updated_at DESC,id DESC LIMIT $5`, strings.ToUpper(strings.TrimSpace(query.Key)), string(query.ScopeType), strings.TrimSpace(query.ScopeID), string(query.Status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Configuration, 0, limit)
	for rows.Next() {
		v, scanErr := scanConfiguration(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (p *PostgreSQLStore) ListConfigurationPage(ctx context.Context, query ConfigurationQuery, before *time.Time, beforeID string) ([]Configuration, error) {
	if p == nil || p.DB == nil {
		return nil, errors.New("database is required")
	}
	limit := query.Limit
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT `+configurationColumns+`
FROM platform_configurations
WHERE ($1='' OR configuration_key=$1)
  AND ($2='' OR scope_type=$2)
  AND ($3='' OR scope_id=$3)
  AND ($4='' OR status=$4)
  AND ($6::timestamptz IS NULL OR updated_at<$6 OR (updated_at=$6 AND id<NULLIF($7,'')::uuid))
ORDER BY updated_at DESC,id DESC LIMIT $5`, strings.ToUpper(strings.TrimSpace(query.Key)), string(query.ScopeType), strings.TrimSpace(query.ScopeID), string(query.Status), limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Configuration, 0, limit)
	for rows.Next() {
		v, scanErr := scanConfiguration(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (p *PostgreSQLStore) GetConfiguration(ctx context.Context, objectID string) (Configuration, error) {
	v, err := scanConfiguration(p.DB.QueryRowContext(ctx, `SELECT `+configurationColumns+` FROM platform_configurations WHERE id=$1::uuid`, objectID))
	if errors.Is(err, sql.ErrNoRows) {
		return Configuration{}, ErrNotFound
	}
	return v, err
}

func insertConfigurationEvent(ctx context.Context, tx *sql.Tx, event Event) error {
	if event.ID == "" {
		var err error
		event.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	evidence, err := json.Marshal(event.Evidence)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO platform_configuration_events(id,configuration_id,event_type,version,actor_id,reason,evidence,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4,$5::uuid,$6,$7::jsonb,$8)`, event.ID, event.ObjectID, event.EventType, event.Version, event.ActorID, event.Reason, string(evidence), event.OccurredAt)
	return err
}

func configurationArgs(value Configuration) []any {
	return []any{value.ID, value.Key, string(value.ScopeType), value.ScopeID, string(value.Value), value.ValueChecksum, string(value.Status), value.EffectiveFrom, value.EffectiveTo, value.CreatedBy, value.SubmittedBy, value.ApprovedBy, value.SupersedesID, value.RollbackOfID, value.Reason, value.Version, value.CreatedAt, value.UpdatedAt}
}

func (p *PostgreSQLStore) CreateConfiguration(ctx context.Context, value Configuration, event Event) (Configuration, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Configuration{}, err
	}
	defer tx.Rollback()
	args := configurationArgs(value)
	if _, err = tx.ExecContext(ctx, `INSERT INTO platform_configurations(id,configuration_key,scope_type,scope_id,value,value_checksum,status,effective_from,effective_to,created_by,submitted_by,approved_by,supersedes_id,rollback_of_id,reason,version,created_at,updated_at)
VALUES($1::uuid,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10::uuid,NULLIF($11,'')::uuid,NULLIF($12,'')::uuid,NULLIF($13,'')::uuid,NULLIF($14,'')::uuid,$15,$16,$17,$18)`, args...); err != nil {
		return Configuration{}, err
	}
	if err = insertConfigurationEvent(ctx, tx, event); err != nil {
		return Configuration{}, err
	}
	if err = tx.Commit(); err != nil {
		return Configuration{}, err
	}
	return value, nil
}

func (p *PostgreSQLStore) UpdateConfiguration(ctx context.Context, value Configuration, expected int64, event Event) (Configuration, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Configuration{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE platform_configurations SET value=$3::jsonb,value_checksum=$4,status=$5,effective_from=$6,effective_to=$7,submitted_by=NULLIF($8,'')::uuid,approved_by=NULLIF($9,'')::uuid,supersedes_id=NULLIF($10,'')::uuid,rollback_of_id=NULLIF($11,'')::uuid,reason=$12,version=$13,updated_at=$14 WHERE id=$1::uuid AND version=$2`, value.ID, expected, string(value.Value), value.ValueChecksum, string(value.Status), value.EffectiveFrom, value.EffectiveTo, value.SubmittedBy, value.ApprovedBy, value.SupersedesID, value.RollbackOfID, value.Reason, value.Version, value.UpdatedAt)
	if err != nil {
		return Configuration{}, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return Configuration{}, err
	}
	if rows != 1 {
		return Configuration{}, ErrConflict
	}
	if err = insertConfigurationEvent(ctx, tx, event); err != nil {
		return Configuration{}, err
	}
	if err = tx.Commit(); err != nil {
		return Configuration{}, err
	}
	return value, nil
}

func (p *PostgreSQLStore) ActivateConfiguration(ctx context.Context, value Configuration, expected int64, event Event) (Configuration, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Configuration{}, err
	}
	defer tx.Rollback()
	// Lock every potentially overlapping effective record for the same key and
	// scope so two approvals cannot both become active.
	rows, err := tx.QueryContext(ctx, `SELECT id::text,effective_from,effective_to,version FROM platform_configurations WHERE id<>$1::uuid AND configuration_key=$2 AND scope_type=$3 AND scope_id=$4 AND status IN ('ACTIVE','SUPERSEDED') AND (effective_to IS NULL OR effective_to>$5) AND ($6::timestamptz IS NULL OR effective_from<$6) FOR UPDATE`, value.ID, value.Key, string(value.ScopeType), value.ScopeID, value.EffectiveFrom, value.EffectiveTo)
	if err != nil {
		return Configuration{}, err
	}
	type overlap struct {
		id      string
		start   time.Time
		end     sql.NullTime
		version int64
	}
	var overlaps []overlap
	for rows.Next() {
		var o overlap
		if err := rows.Scan(&o.id, &o.start, &o.end, &o.version); err != nil {
			rows.Close()
			return Configuration{}, err
		}
		overlaps = append(overlaps, o)
	}
	if err := rows.Close(); err != nil {
		return Configuration{}, err
	}
	for _, o := range overlaps {
		if !value.EffectiveFrom.After(o.start) {
			return Configuration{}, ErrConflict
		}
		res, updateErr := tx.ExecContext(ctx, `UPDATE platform_configurations SET status='SUPERSEDED',effective_to=$2,version=version+1,updated_at=$3 WHERE id=$1::uuid AND version=$4`, o.id, value.EffectiveFrom, value.UpdatedAt, o.version)
		if updateErr != nil {
			return Configuration{}, updateErr
		}
		affected, affectedErr := res.RowsAffected()
		if affectedErr != nil {
			return Configuration{}, affectedErr
		}
		if affected != 1 {
			return Configuration{}, ErrConflict
		}
		if value.SupersedesID == "" {
			value.SupersedesID = o.id
		}
		supersededEvent := Event{
			ObjectID: o.id, EventType: "SUPERSEDED", Version: o.version + 1,
			ActorID: event.ActorID, Reason: event.Reason,
			Evidence:   map[string]any{"supersededById": value.ID, "effectiveTo": value.EffectiveFrom},
			OccurredAt: event.OccurredAt,
		}
		if err = insertConfigurationEvent(ctx, tx, supersededEvent); err != nil {
			return Configuration{}, err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE platform_configurations SET status='ACTIVE',effective_from=$3,effective_to=$4,approved_by=$5::uuid,supersedes_id=NULLIF($6,'')::uuid,rollback_of_id=NULLIF($7,'')::uuid,reason=$8,version=$9,updated_at=$10 WHERE id=$1::uuid AND version=$2 AND status='PENDING_APPROVAL'`, value.ID, expected, value.EffectiveFrom, value.EffectiveTo, value.ApprovedBy, value.SupersedesID, value.RollbackOfID, value.Reason, value.Version, value.UpdatedAt)
	if err != nil {
		return Configuration{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Configuration{}, err
	}
	if n != 1 {
		return Configuration{}, ErrConflict
	}
	if err = insertConfigurationEvent(ctx, tx, event); err != nil {
		return Configuration{}, err
	}
	if err = tx.Commit(); err != nil {
		return Configuration{}, err
	}
	return value, nil
}

func (p *PostgreSQLStore) ResolveConfiguration(ctx context.Context, key string, scope ScopeType, scopeID string, at time.Time) (Configuration, error) {
	v, err := scanConfiguration(p.DB.QueryRowContext(ctx, `SELECT `+configurationColumns+` FROM platform_configurations WHERE configuration_key=$1 AND scope_type=$2 AND scope_id=$3 AND status IN ('ACTIVE','SUPERSEDED') AND effective_from<=$4 AND (effective_to IS NULL OR effective_to>$4) ORDER BY effective_from DESC,version DESC LIMIT 1`, key, string(scope), scopeID, at))
	if errors.Is(err, sql.ErrNoRows) {
		return Configuration{}, ErrNotFound
	}
	return v, err
}

func (p *PostgreSQLStore) ListConfigurationEvents(ctx context.Context, objectID string, limit int) ([]Event, error) {
	return p.ListConfigurationEventPage(ctx, objectID, limit, nil, "")
}

func (p *PostgreSQLStore) ListConfigurationEventPage(ctx context.Context, objectID string, limit int, before *time.Time, beforeID string) ([]Event, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,configuration_id::text,event_type,version,actor_id::text,reason,evidence,occurred_at FROM platform_configuration_events WHERE configuration_id=$1::uuid AND ($3::timestamptz IS NULL OR occurred_at<$3 OR (occurred_at=$3 AND id<NULLIF($4,'')::uuid)) ORDER BY occurred_at DESC,id DESC LIMIT $2`, objectID, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		var raw []byte
		if err := rows.Scan(&v.ID, &v.ObjectID, &v.EventType, &v.Version, &v.ActorID, &v.Reason, &raw, &v.OccurredAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &v.Evidence); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

const maintenanceColumns = `id::text,name,mode,scope_type,scope_id,starts_at,ends_at,allow_active_dispatch,status,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),coalesce(ended_by::text,''),reason,version,created_at,updated_at`

func scanMaintenance(row rowScanner) (MaintenanceWindow, error) {
	var v MaintenanceWindow
	var mode, scope, status string
	var ends sql.NullTime
	err := row.Scan(&v.ID, &v.Name, &mode, &scope, &v.ScopeID, &v.StartsAt, &ends, &v.AllowActiveDispatch, &status, &v.CreatedBy, &v.SubmittedBy, &v.ApprovedBy, &v.EndedBy, &v.Reason, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return MaintenanceWindow{}, err
	}
	v.Mode, v.ScopeType, v.Status = MaintenanceMode(mode), ScopeType(scope), MaintenanceStatus(status)
	if ends.Valid {
		t := ends.Time.UTC()
		v.EndsAt = &t
	}
	return v, nil
}

func insertMaintenanceEvent(ctx context.Context, tx *sql.Tx, event Event) error {
	if event.ID == "" {
		var err error
		event.ID, err = id.New()
		if err != nil {
			return err
		}
	}
	evidence, err := json.Marshal(event.Evidence)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO maintenance_window_events(id,maintenance_window_id,event_type,version,actor_id,reason,evidence,occurred_at) VALUES($1::uuid,$2::uuid,$3,$4,$5::uuid,$6,$7::jsonb,$8)`, event.ID, event.ObjectID, event.EventType, event.Version, event.ActorID, event.Reason, string(evidence), event.OccurredAt)
	return err
}

func (p *PostgreSQLStore) ListMaintenance(ctx context.Context, status MaintenanceStatus, limit int) ([]MaintenanceWindow, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+maintenanceColumns+` FROM maintenance_windows WHERE ($1='' OR status=$1) ORDER BY updated_at DESC,id DESC LIMIT $2`, string(status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MaintenanceWindow{}
	for rows.Next() {
		v, scanErr := scanMaintenance(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (p *PostgreSQLStore) ListMaintenancePage(ctx context.Context, status MaintenanceStatus, limit int, before *time.Time, beforeID string) ([]MaintenanceWindow, error) {
	if p == nil || p.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 501 {
		limit = 100
	}
	rows, err := p.DB.QueryContext(ctx, `SELECT `+maintenanceColumns+` FROM maintenance_windows
WHERE ($1='' OR status=$1)
  AND ($3::timestamptz IS NULL OR updated_at<$3 OR (updated_at=$3 AND id<NULLIF($4,'')::uuid))
ORDER BY updated_at DESC,id DESC LIMIT $2`, string(status), limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MaintenanceWindow, 0, limit)
	for rows.Next() {
		v, scanErr := scanMaintenance(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (p *PostgreSQLStore) GetMaintenance(ctx context.Context, objectID string) (MaintenanceWindow, error) {
	v, err := scanMaintenance(p.DB.QueryRowContext(ctx, `SELECT `+maintenanceColumns+` FROM maintenance_windows WHERE id=$1::uuid`, objectID))
	if errors.Is(err, sql.ErrNoRows) {
		return MaintenanceWindow{}, ErrNotFound
	}
	return v, err
}

func (p *PostgreSQLStore) CreateMaintenance(ctx context.Context, value MaintenanceWindow, event Event) (MaintenanceWindow, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return MaintenanceWindow{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO maintenance_windows(id,name,mode,scope_type,scope_id,starts_at,ends_at,allow_active_dispatch,status,created_by,submitted_by,approved_by,ended_by,reason,version,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10::uuid,NULLIF($11,'')::uuid,NULLIF($12,'')::uuid,NULLIF($13,'')::uuid,$14,$15,$16,$17)`, value.ID, value.Name, string(value.Mode), string(value.ScopeType), value.ScopeID, value.StartsAt, value.EndsAt, value.AllowActiveDispatch, string(value.Status), value.CreatedBy, value.SubmittedBy, value.ApprovedBy, value.EndedBy, value.Reason, value.Version, value.CreatedAt, value.UpdatedAt)
	if err != nil {
		return MaintenanceWindow{}, err
	}
	if err = insertMaintenanceEvent(ctx, tx, event); err != nil {
		return MaintenanceWindow{}, err
	}
	if err = tx.Commit(); err != nil {
		return MaintenanceWindow{}, err
	}
	return value, nil
}

func (p *PostgreSQLStore) UpdateMaintenance(ctx context.Context, value MaintenanceWindow, expected int64, event Event) (MaintenanceWindow, error) {
	tx, err := p.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return MaintenanceWindow{}, err
	}
	defer tx.Rollback()
	if value.Status == MaintenanceActive {
		var conflict bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM maintenance_windows WHERE id<>$1::uuid AND status='ACTIVE' AND scope_type=$2 AND scope_id=$3 AND (ends_at IS NULL OR ends_at>$4) AND ($5::timestamptz IS NULL OR starts_at<$5) FOR UPDATE)`, value.ID, string(value.ScopeType), value.ScopeID, value.StartsAt, value.EndsAt).Scan(&conflict)
		if err != nil {
			return MaintenanceWindow{}, err
		}
		if conflict {
			return MaintenanceWindow{}, errors.New("overlapping active maintenance window")
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE maintenance_windows SET name=$3,mode=$4,scope_type=$5,scope_id=$6,starts_at=$7,ends_at=$8,allow_active_dispatch=$9,status=$10,submitted_by=NULLIF($11,'')::uuid,approved_by=NULLIF($12,'')::uuid,ended_by=NULLIF($13,'')::uuid,reason=$14,version=$15,updated_at=$16 WHERE id=$1::uuid AND version=$2`, value.ID, expected, value.Name, string(value.Mode), string(value.ScopeType), value.ScopeID, value.StartsAt, value.EndsAt, value.AllowActiveDispatch, string(value.Status), value.SubmittedBy, value.ApprovedBy, value.EndedBy, value.Reason, value.Version, value.UpdatedAt)
	if err != nil {
		return MaintenanceWindow{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return MaintenanceWindow{}, err
	}
	if n != 1 {
		return MaintenanceWindow{}, ErrConflict
	}
	if err = insertMaintenanceEvent(ctx, tx, event); err != nil {
		return MaintenanceWindow{}, err
	}
	if err = tx.Commit(); err != nil {
		return MaintenanceWindow{}, err
	}
	return value, nil
}

func (p *PostgreSQLStore) ListActiveMaintenance(ctx context.Context, at time.Time) ([]MaintenanceWindow, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+maintenanceColumns+` FROM maintenance_windows WHERE status='ACTIVE' AND starts_at<=$1 AND (ends_at IS NULL OR ends_at>$1) ORDER BY starts_at ASC,id ASC`, at)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MaintenanceWindow{}
	for rows.Next() {
		v, scanErr := scanMaintenance(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (p *PostgreSQLStore) ListMaintenanceEvents(ctx context.Context, objectID string, limit int) ([]Event, error) {
	return p.ListMaintenanceEventPage(ctx, objectID, limit, nil, "")
}

func (p *PostgreSQLStore) ListMaintenanceEventPage(ctx context.Context, objectID string, limit int, before *time.Time, beforeID string) ([]Event, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id::text,maintenance_window_id::text,event_type,version,actor_id::text,reason,evidence,occurred_at FROM maintenance_window_events WHERE maintenance_window_id=$1::uuid AND ($3::timestamptz IS NULL OR occurred_at<$3 OR (occurred_at=$3 AND id<NULLIF($4,'')::uuid)) ORDER BY occurred_at DESC,id DESC LIMIT $2`, objectID, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		var raw []byte
		if err := rows.Scan(&v.ID, &v.ObjectID, &v.EventType, &v.Version, &v.ActorID, &v.Reason, &raw, &v.OccurredAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &v.Evidence); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (p *PostgreSQLStore) String() string {
	return fmt.Sprintf("platformpolicy.PostgreSQLStore(%p)", p)
}
