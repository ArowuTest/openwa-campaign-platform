package provider

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type PostgreSQLStore struct{ DB *sql.DB }

func (s *PostgreSQLStore) List(ctx context.Context) ([]Definition, error) {
	rows, err := s.DB.QueryContext(ctx, providerSelect+` ORDER BY created_at DESC,id DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Definition{}
	for rows.Next() {
		d, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *PostgreSQLStore) Get(ctx context.Context, id string) (Definition, error) {
	d, err := scanDefinition(s.DB.QueryRowContext(ctx, providerSelect+` WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Definition{}, ErrNotFound
	}
	return d, err
}
func (s *PostgreSQLStore) Active(ctx context.Context, p string, c Channel, e string, at time.Time) (Definition, error) {
	d, err := scanDefinition(s.DB.QueryRowContext(ctx, providerSelect+` WHERE provider=$1 AND channel=$2 AND engine=$3 AND status='ACTIVE' AND effective_from<=$4 AND (effective_to IS NULL OR effective_to>$4) ORDER BY effective_from DESC LIMIT 1`, strings.ToUpper(strings.TrimSpace(p)), c, strings.ToUpper(strings.TrimSpace(e)), at.UTC()))
	if errors.Is(err, sql.ErrNoRows) {
		return Definition{}, ErrNotFound
	}
	return d, err
}
func (s *PostgreSQLStore) Create(ctx context.Context, d Definition) (Definition, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return d, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO provider_capability_definitions(id,provider,channel,engine,adapter_version,minimum_gateway_version,capabilities,maximum_attachment_bytes,status,effective_from,effective_to,version,created_by,reason,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$12,$13::uuid,$14,$15,$16)`, d.ID, d.Provider, d.Channel, d.Engine, d.AdapterVersion, d.MinimumGatewayVersion, capabilityStrings(d.Capabilities), d.MaximumAttachmentBytes, d.Status, d.EffectiveFrom, d.EffectiveTo, d.Version, d.CreatedBy, d.Reason, d.CreatedAt, d.UpdatedAt)
	if err != nil {
		return d, err
	}
	if err = insertProviderEvent(ctx, tx, d, "CREATED", d.CreatedBy); err != nil {
		return d, err
	}
	if err = tx.Commit(); err != nil {
		return d, err
	}
	return d, nil
}
func (s *PostgreSQLStore) CompareAndSwap(ctx context.Context, d Definition, expected int64) (Definition, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return d, err
	}
	defer tx.Rollback()
	if d.Status == StatusActive {
		routeKey := d.Provider + "\x1f" + string(d.Channel) + "\x1f" + d.Engine
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, routeKey); err != nil {
			return d, err
		}
		if strings.TrimSpace(d.ApprovedBy) == "" {
			return d, errors.New("provider capability activation approver is required")
		}
		_, err = tx.ExecContext(ctx, `WITH superseded AS (
  UPDATE provider_capability_definitions
  SET status=CASE WHEN effective_from >= $1 OR $1 <= $2 THEN 'RETIRED' ELSE status END,
      effective_to=CASE WHEN effective_from < $1 THEN $1 ELSE effective_to END,
      approved_by=$3::uuid,reason=$4,version=version+1,updated_at=$2
  WHERE id<>$5::uuid AND provider=$6 AND channel=$7 AND engine=$8 AND status='ACTIVE'
    AND effective_from<$9 AND (effective_to IS NULL OR effective_to>$1)
  RETURNING id,version
)
INSERT INTO provider_capability_events(definition_id,action,actor_id,reason,definition_version,occurred_at)
SELECT id,'SUPERSEDED',$3::uuid,$4,version,$2 FROM superseded`, d.EffectiveFrom, d.UpdatedAt, d.ApprovedBy, "superseded by provider definition "+d.ID, d.ID, d.Provider, d.Channel, d.Engine, coalesceEnd(d.EffectiveTo))
		if err != nil {
			return d, err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE provider_capability_definitions SET provider=$3,channel=$4,engine=$5,adapter_version=$6,minimum_gateway_version=NULLIF($7,''),capabilities=$8,maximum_attachment_bytes=$9,status=$10,effective_from=$11,effective_to=$12,version=$13,submitted_by=NULLIF($14,'')::uuid,approved_by=NULLIF($15,'')::uuid,reason=$16,updated_at=$17 WHERE id=$1::uuid AND version=$2`, d.ID, expected, d.Provider, d.Channel, d.Engine, d.AdapterVersion, d.MinimumGatewayVersion, capabilityStrings(d.Capabilities), d.MaximumAttachmentBytes, d.Status, d.EffectiveFrom, d.EffectiveTo, d.Version, d.SubmittedBy, d.ApprovedBy, d.Reason, d.UpdatedAt)
	if err != nil {
		return d, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return d, err
	}
	if n != 1 {
		return d, ErrConflict
	}
	action, actor := string(d.Status), d.SubmittedBy
	if d.Status == StatusActive || d.Status == StatusRejected || d.Status == StatusRetired {
		actor = d.ApprovedBy
	}
	if err = insertProviderEvent(ctx, tx, d, action, actor); err != nil {
		return d, err
	}
	if err = tx.Commit(); err != nil {
		return d, err
	}
	return d, nil
}

func (s *PostgreSQLStore) ListEvents(ctx context.Context, definitionID string) ([]Event, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,definition_id::text,action,actor_id::text,reason,definition_version,occurred_at FROM provider_capability_events WHERE definition_id=$1::uuid ORDER BY id DESC LIMIT 5000`, definitionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.DefinitionID, &event.Action, &event.ActorID, &event.Reason, &event.DefinitionVersion, &event.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func insertProviderEvent(ctx context.Context, tx *sql.Tx, d Definition, action, actor string) error {
	if strings.TrimSpace(actor) == "" {
		return errors.New("provider capability event actor is required")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO provider_capability_events(definition_id,action,actor_id,reason,definition_version,occurred_at) VALUES($1::uuid,$2,$3::uuid,$4,$5,$6)`, d.ID, action, actor, d.Reason, d.Version, d.UpdatedAt)
	return err
}

const providerSelect = `SELECT id::text,provider,channel,engine,adapter_version,coalesce(minimum_gateway_version,''),capabilities,maximum_attachment_bytes,status,effective_from,effective_to,version,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,created_at,updated_at FROM provider_capability_definitions`

type scanner interface{ Scan(...any) error }

func scanDefinition(s scanner) (Definition, error) {
	var d Definition
	var caps []string
	err := s.Scan(&d.ID, &d.Provider, &d.Channel, &d.Engine, &d.AdapterVersion, &d.MinimumGatewayVersion, &caps, &d.MaximumAttachmentBytes, &d.Status, &d.EffectiveFrom, &d.EffectiveTo, &d.Version, &d.CreatedBy, &d.SubmittedBy, &d.ApprovedBy, &d.Reason, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return Definition{}, err
	}
	d.Capabilities = make([]Capability, len(caps))
	for i, v := range caps {
		d.Capabilities[i] = Capability(v)
	}
	return d, nil
}
func capabilityStrings(in []Capability) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}
func coalesceEnd(v *time.Time) time.Time {
	if v != nil {
		return v.UTC()
	}
	return time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
}
