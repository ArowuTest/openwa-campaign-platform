package metacloud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type PostgreSQLTemplateStore struct{ DB *sql.DB }

func (s *PostgreSQLTemplateStore) ReplaceWABATemplates(ctx context.Context, organisationID, wabaID string, values []Template, syncedAt time.Time) error {
	if s == nil || s.DB == nil {
		return errors.New("database is required")
	}
	organisationID, wabaID = strings.TrimSpace(organisationID), strings.TrimSpace(wabaID)
	if organisationID == "" || wabaID == "" || syncedAt.IsZero() {
		return ErrTemplateInvalid
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	syncedAt = syncedAt.UTC()
	var inserted bool
	err = tx.QueryRowContext(ctx, `INSERT INTO meta_cloud_template_sync_state(organisation_id,waba_id,last_synced_at,updated_at) VALUES($1::uuid,$2,$3,$3) ON CONFLICT(organisation_id,waba_id) DO NOTHING RETURNING true`, organisationID, wabaID, syncedAt).Scan(&inserted)
	if errors.Is(err, sql.ErrNoRows) {
		var previous time.Time
		if err := tx.QueryRowContext(ctx, `SELECT last_synced_at FROM meta_cloud_template_sync_state WHERE organisation_id=$1::uuid AND waba_id=$2 FOR UPDATE`, organisationID, wabaID).Scan(&previous); err != nil {
			return err
		}
		if !syncedAt.After(previous.UTC()) {
			return nil
		}
	} else if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM meta_cloud_templates WHERE organisation_id=$1::uuid AND waba_id=$2`, organisationID, wabaID); err != nil {
		return err
	}
	for _, value := range values {
		value.OrganisationID, value.WABAID = organisationID, wabaID
		value.Status = strings.ToUpper(strings.TrimSpace(value.Status))
		value.LastSyncedAt = syncedAt.UTC()
		if value.ComponentHash == "" {
			value.ComponentHash, err = CanonicalComponentHash(value.Components)
			if err != nil {
				return err
			}
		}
		if value.MetaTemplateID == "" || value.Name == "" || value.Language == "" || value.Category == "" || value.Status == "" || len(value.ComponentHash) != 64 {
			return ErrTemplateInvalid
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO meta_cloud_templates(organisation_id,waba_id,meta_template_id,name,language,category,status,quality_signal,components,component_hash,last_synced_at,updated_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9::jsonb,$10,$11,$11)`, organisationID, wabaID, value.MetaTemplateID, value.Name, value.Language, value.Category, value.Status, value.QualitySignal, string(value.Components), value.ComponentHash, value.LastSyncedAt); err != nil {
			return err
		}
	}
	if !inserted {
		if _, err := tx.ExecContext(ctx, `UPDATE meta_cloud_template_sync_state SET last_synced_at=$3,updated_at=$3 WHERE organisation_id=$1::uuid AND waba_id=$2`, organisationID, wabaID, syncedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanTemplate(row scanner) (Template, error) {
	var value Template
	var components []byte
	var quality sql.NullString
	err := row.Scan(&value.ID, &value.OrganisationID, &value.WABAID, &value.MetaTemplateID, &value.Name, &value.Language, &value.Category, &value.Status, &quality, &components, &value.ComponentHash, &value.LastSyncedAt, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return Template{}, err
	}
	if quality.Valid {
		value.QualitySignal = quality.String
	}
	value.Components = append([]byte(nil), components...)
	return value, nil
}

const templateSelect = `SELECT id::text,organisation_id::text,waba_id,meta_template_id,name,language,category,status,quality_signal,components,component_hash,last_synced_at,created_at,updated_at FROM meta_cloud_templates`

func (s *PostgreSQLTemplateStore) ListApproved(ctx context.Context, organisationID, wabaID string) ([]Template, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := s.DB.QueryContext(ctx, templateSelect+` WHERE organisation_id=$1::uuid AND waba_id=$2 AND status='APPROVED' ORDER BY name,language`, strings.TrimSpace(organisationID), strings.TrimSpace(wabaID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Template{}
	for rows.Next() {
		value, scanErr := scanTemplate(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *PostgreSQLTemplateStore) FindApproved(ctx context.Context, organisationID, wabaID, name, language string) (Template, error) {
	if s == nil || s.DB == nil {
		return Template{}, errors.New("database is required")
	}
	value, err := scanTemplate(s.DB.QueryRowContext(ctx, templateSelect+` WHERE organisation_id=$1::uuid AND waba_id=$2 AND name=$3 AND language=$4 AND status='APPROVED'`, strings.TrimSpace(organisationID), strings.TrimSpace(wabaID), strings.TrimSpace(name), strings.TrimSpace(language)))
	if errors.Is(err, sql.ErrNoRows) {
		return Template{}, ErrNotFound
	}
	return value, err
}

func scanBinding(row scanner) (Binding, error) {
	var value Binding
	var names, components []byte
	var media sql.NullString
	err := row.Scan(&value.MessageVersionID, &value.TemplateName, &value.Language, &names, &media, &components, &value.TemplateComponentHash, &value.CreatedBy, &value.CreatedAt)
	if err != nil {
		return Binding{}, err
	}
	if media.Valid {
		value.MediaHeaderType = media.String
	}
	if err := json.Unmarshal(names, &value.BodyVariableNames); err != nil {
		return Binding{}, err
	}
	if err := json.Unmarshal(components, &value.ComponentBindings); err != nil {
		return Binding{}, err
	}
	return value, nil
}

const bindingSelect = `SELECT message_version_id::text,template_name,language,body_variable_names,media_header_type,component_bindings,template_component_hash,created_by::text,created_at FROM meta_cloud_message_bindings`

func (s *PostgreSQLTemplateStore) CreateBinding(ctx context.Context, value Binding) (Binding, error) {
	if s == nil || s.DB == nil {
		return Binding{}, errors.New("database is required")
	}
	if strings.TrimSpace(value.MessageVersionID) == "" || strings.TrimSpace(value.TemplateName) == "" || strings.TrimSpace(value.Language) == "" || strings.TrimSpace(value.CreatedBy) == "" || len(value.TemplateComponentHash) != 64 {
		return Binding{}, ErrBindingInvalid
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	} else {
		value.CreatedAt = value.CreatedAt.UTC()
	}
	names, err := json.Marshal(value.BodyVariableNames)
	if err != nil {
		return Binding{}, ErrBindingInvalid
	}
	components, err := json.Marshal(value.ComponentBindings)
	if err != nil {
		return Binding{}, ErrBindingInvalid
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Binding{}, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM message_versions WHERE id=$1::uuid FOR SHARE`, value.MessageVersionID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Binding{}, ErrBindingInvalid
		}
		return Binding{}, err
	}
	if status != "APPROVED" {
		return Binding{}, ErrBindingInvalid
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO meta_cloud_message_bindings(message_version_id,template_name,language,body_variable_names,media_header_type,component_bindings,template_component_hash,created_by,created_at) VALUES($1::uuid,$2,$3,$4::jsonb,NULLIF($5,''),$6::jsonb,$7,$8::uuid,$9) ON CONFLICT(message_version_id) DO NOTHING`, value.MessageVersionID, value.TemplateName, value.Language, string(names), strings.ToUpper(strings.TrimSpace(value.MediaHeaderType)), string(components), value.TemplateComponentHash, value.CreatedBy, value.CreatedAt)
	if err != nil {
		return Binding{}, err
	}
	stored, err := scanBinding(tx.QueryRowContext(ctx, bindingSelect+` WHERE message_version_id=$1::uuid`, value.MessageVersionID))
	if err != nil {
		return Binding{}, err
	}
	if !bindingEqual(stored, value) {
		return Binding{}, ErrBindingConflict
	}
	if err := tx.Commit(); err != nil {
		return Binding{}, err
	}
	return stored, nil
}

func (s *PostgreSQLTemplateStore) GetBinding(ctx context.Context, messageVersionID string) (Binding, error) {
	if s == nil || s.DB == nil {
		return Binding{}, errors.New("database is required")
	}
	value, err := scanBinding(s.DB.QueryRowContext(ctx, bindingSelect+` WHERE message_version_id=$1::uuid`, strings.TrimSpace(messageVersionID)))
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, ErrNotFound
	}
	return value, err
}

var _ TemplateStore = (*PostgreSQLTemplateStore)(nil)
