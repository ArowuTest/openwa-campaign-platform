package metacloud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type PostgreSQLStore struct{ DB *sql.DB }

type scanner interface{ Scan(...any) error }

const senderColumns = `id::text,organisation_id::text,sender_pool_id::text,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,effective_to,version,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,created_at,updated_at`

func scanSender(row scanner) (Sender, error) {
	var value Sender
	var healthObserved, effectiveFrom, effectiveTo sql.NullTime
	err := row.Scan(&value.ID, &value.OrganisationID, &value.SenderPoolID, &value.WABAID, &value.PhoneNumberID,
		&value.DisplayName, &value.BusinessPhoneDisplay, &value.CredentialKey, &value.GraphAPIVersion,
		&value.Status, &value.Health, &healthObserved, &effectiveFrom, &effectiveTo, &value.Version,
		&value.CreatedBy, &value.SubmittedBy, &value.ApprovedBy, &value.Reason, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return Sender{}, err
	}
	if healthObserved.Valid {
		t := healthObserved.Time.UTC()
		value.HealthObservedAt = &t
	}
	if effectiveFrom.Valid {
		t := effectiveFrom.Time.UTC()
		value.EffectiveFrom = &t
	}
	if effectiveTo.Valid {
		t := effectiveTo.Time.UTC()
		value.EffectiveTo = &t
	}
	return value, nil
}

func (s *PostgreSQLStore) ResolveMetaWebhookSender(ctx context.Context, credentialKey, wabaID, phoneNumberID string) (Sender, error) {
	if s == nil || s.DB == nil {
		return Sender{}, errors.New("database is required")
	}
	value, err := scanSender(s.DB.QueryRowContext(ctx, `SELECT `+senderColumns+` FROM meta_cloud_senders WHERE credential_key=$1 AND waba_id=$2 AND phone_number_id=$3 ORDER BY version DESC LIMIT 1`, strings.TrimSpace(credentialKey), strings.TrimSpace(wabaID), strings.TrimSpace(phoneNumberID)))
	if errors.Is(err, sql.ErrNoRows) {
		return Sender{}, ErrNotFound
	}
	return value, err
}

func (s *PostgreSQLStore) Get(ctx context.Context, senderID string) (Sender, error) {
	if s == nil || s.DB == nil {
		return Sender{}, errors.New("database is required")
	}
	value, err := scanSender(s.DB.QueryRowContext(ctx, `SELECT `+senderColumns+` FROM meta_cloud_senders WHERE id=$1::uuid`, senderID))
	if errors.Is(err, sql.ErrNoRows) {
		return Sender{}, ErrNotFound
	}
	return value, err
}

func (s *PostgreSQLStore) List(ctx context.Context) ([]Sender, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+senderColumns+` FROM meta_cloud_senders ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sender{}
	for rows.Next() {
		value, scanErr := scanSender(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *PostgreSQLStore) Create(ctx context.Context, value Sender) (Sender, error) {
	if s == nil || s.DB == nil {
		return Sender{}, errors.New("database is required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Sender{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO meta_cloud_senders(id,organisation_id,sender_pool_id,waba_id,phone_number_id,display_name,business_phone_display,credential_key,graph_api_version,status,health_status,health_observed_at,effective_from,effective_to,version,created_by,submitted_by,approved_by,reason,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16::uuid,NULLIF($17,'')::uuid,NULLIF($18,'')::uuid,$19,$20,$21)`,
		value.ID, value.OrganisationID, value.SenderPoolID, value.WABAID, value.PhoneNumberID, value.DisplayName,
		value.BusinessPhoneDisplay, value.CredentialKey, value.GraphAPIVersion, value.Status, value.Health,
		value.HealthObservedAt, value.EffectiveFrom, value.EffectiveTo, value.Version, value.CreatedBy,
		value.SubmittedBy, value.ApprovedBy, value.Reason, value.CreatedAt, value.UpdatedAt)
	if err != nil {
		return Sender{}, err
	}
	if err := insertSenderEvent(ctx, tx, value, "CREATED", value.CreatedBy, nil); err != nil {
		return Sender{}, err
	}
	if err := tx.Commit(); err != nil {
		return Sender{}, err
	}
	return value, nil
}

func (s *PostgreSQLStore) CompareAndSwap(ctx context.Context, value Sender, expected int64, actor, action string, evidence map[string]any) (Sender, error) {
	if s == nil || s.DB == nil {
		return Sender{}, errors.New("database is required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Sender{}, err
	}
	defer tx.Rollback()
	current, err := scanSender(tx.QueryRowContext(ctx, `SELECT `+senderColumns+` FROM meta_cloud_senders WHERE id=$1::uuid FOR UPDATE`, value.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return Sender{}, ErrNotFound
	}
	if err != nil {
		return Sender{}, err
	}
	if current.Version != expected || !sameSenderIdentity(current, value) {
		return Sender{}, ErrConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE meta_cloud_senders SET status=$2,health_status=$3,health_observed_at=$4,effective_from=$5,effective_to=$6,version=$7,submitted_by=NULLIF($8,'')::uuid,approved_by=NULLIF($9,'')::uuid,reason=$10,updated_at=$11 WHERE id=$1::uuid AND version=$12`,
		value.ID, value.Status, value.Health, value.HealthObservedAt, value.EffectiveFrom, value.EffectiveTo,
		value.Version, value.SubmittedBy, value.ApprovedBy, value.Reason, value.UpdatedAt, expected)
	if err != nil {
		return Sender{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Sender{}, err
	}
	if affected != 1 {
		return Sender{}, ErrConflict
	}
	if err := insertSenderEvent(ctx, tx, value, action, actor, evidence); err != nil {
		return Sender{}, err
	}
	if err := tx.Commit(); err != nil {
		return Sender{}, err
	}
	return value, nil
}

func insertSenderEvent(ctx context.Context, tx *sql.Tx, value Sender, action, actor string, evidence map[string]any) error {
	if evidence == nil {
		evidence = map[string]any{}
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO meta_cloud_sender_events(sender_id,action,actor_id,reason,sender_version,evidence,occurred_at) VALUES($1::uuid,$2,NULLIF($3,'')::uuid,$4,$5,$6::jsonb,$7)`, value.ID, action, actor, value.Reason, value.Version, string(raw), value.UpdatedAt)
	return err
}

func (s *PostgreSQLStore) ListEvents(ctx context.Context, senderID string) ([]Event, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,sender_id::text,action,coalesce(actor_id::text,''),reason,sender_version,evidence,occurred_at FROM meta_cloud_sender_events WHERE sender_id=$1::uuid ORDER BY id`, senderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var value Event
		var raw []byte
		if err := rows.Scan(&value.ID, &value.SenderID, &value.Action, &value.ActorID, &value.Reason, &value.SenderVersion, &raw, &value.OccurredAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &value.Evidence); err != nil {
				return nil, err
			}
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		var exists bool
		if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM meta_cloud_senders WHERE id=$1::uuid)`, senderID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	return out, nil
}

var _ Store = (*PostgreSQLStore)(nil)
var _ = time.Time{}
