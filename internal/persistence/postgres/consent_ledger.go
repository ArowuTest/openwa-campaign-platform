package postgres

import (
	"campaign-platform/internal/consent"
	"context"
	"database/sql"
	"errors"
	"time"
)

type ConsentLedgerRepository struct{ DB *sql.DB }

func (r *ConsentLedgerRepository) CreateGrant(ctx context.Context, v consent.Grant) (consent.Grant, bool, error) {
	if r.DB == nil {
		return consent.Grant{}, false, errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return consent.Grant{}, false, err
	}
	defer tx.Rollback()
	var id, fingerprint string
	err = tx.QueryRowContext(ctx, `SELECT id::text,request_fingerprint FROM consent_grants WHERE created_by=$1::uuid AND client_request_id=$2`, v.CreatedBy, v.ClientRequestID).Scan(&id, &fingerprint)
	if err == nil {
		if fingerprint != v.RequestFingerprint {
			return consent.Grant{}, false, consent.ErrLedgerReplayConflict
		}
		existing, err := scanGrant(tx.QueryRowContext(ctx, grantSelect+` WHERE id=$1`, id))
		return existing, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return consent.Grant{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO consent_grants(id,contact_id,organisation_id,purpose_id,channel,consent_review_id,wording_version,source_type,source_reference,evidence_object_key,evidence_checksum,effective_from,granted_at,expires_at,status,created_by,client_request_id,request_fingerprint,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`, v.ID, v.ContactID, v.OrganisationID, v.PurposeID, v.Channel, v.ConsentReviewID, v.WordingVersion, v.SourceType, v.SourceReference, v.EvidenceObjectKey, v.EvidenceChecksum, v.EffectiveFrom, v.GrantedAt, v.ExpiresAt, v.Status, v.CreatedBy, v.ClientRequestID, v.RequestFingerprint, v.Version, v.CreatedAt, v.UpdatedAt)
	if err != nil {
		return consent.Grant{}, false, err
	}
	if err = insertConsentEvent(ctx, tx, v.ContactID, v.ID, "", "GRANT_CREATED", v.CreatedBy, "", v.SourceReference, v.CreatedAt); err != nil {
		return consent.Grant{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return consent.Grant{}, false, err
	}
	return v, true, nil
}
func (r *ConsentLedgerRepository) WithdrawGrant(ctx context.Context, id string, in consent.WithdrawInput, now time.Time) (consent.Grant, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return consent.Grant{}, err
	}
	defer tx.Rollback()
	v, err := scanGrant(tx.QueryRowContext(ctx, grantSelect+` WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return consent.Grant{}, consent.ErrGrantNotFound
	}
	if err != nil {
		return consent.Grant{}, err
	}
	if v.Version != in.ExpectedVersion {
		return consent.Grant{}, consent.ErrLedgerConflict
	}
	res, err := tx.ExecContext(ctx, `UPDATE consent_grants SET status='WITHDRAWN',version=version+1,updated_at=$2 WHERE id=$1 AND version=$3`, id, now, in.ExpectedVersion)
	if err != nil {
		return consent.Grant{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return consent.Grant{}, err
	}
	if n != 1 {
		return consent.Grant{}, consent.ErrLedgerConflict
	}
	if err = insertConsentEvent(ctx, tx, v.ContactID, v.ID, "", "GRANT_WITHDRAWN", in.ActorID, in.Reason, in.SourceReference, now); err != nil {
		return consent.Grant{}, err
	}
	if err = tx.Commit(); err != nil {
		return consent.Grant{}, err
	}
	v.Status = consent.GrantWithdrawn
	v.Version++
	v.UpdatedAt = now
	return v, nil
}
func (r *ConsentLedgerRepository) CreateSuppression(ctx context.Context, v consent.Suppression) (consent.Suppression, bool, error) {
	if r.DB == nil {
		return consent.Suppression{}, false, errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return consent.Suppression{}, false, err
	}
	defer tx.Rollback()
	var id, fingerprint string
	err = tx.QueryRowContext(ctx, `SELECT id::text,request_fingerprint FROM suppressions WHERE created_by=$1::uuid AND client_request_id=$2`, v.CreatedBy, v.ClientRequestID).Scan(&id, &fingerprint)
	if err == nil {
		if fingerprint != v.RequestFingerprint {
			return consent.Suppression{}, false, consent.ErrLedgerReplayConflict
		}
		existing, err := scanSuppression(tx.QueryRowContext(ctx, suppressionSelect+` WHERE id=$1`, id))
		return existing, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return consent.Suppression{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO suppressions(id,contact_id,msisdn_lookup_hmac,organisation_id,purpose_id,channel,scope,reason,effective_at,expires_at,active,source_reference,created_by,client_request_id,request_fingerprint,version,created_at) VALUES($1,NULLIF($2,'')::uuid,$3,NULLIF($4,'')::uuid,NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10,$11,NULLIF($12,''),$13,$14,$15,$16,$17)`, v.ID, v.ContactID, v.MSISDNLookupHMAC, v.OrganisationID, v.PurposeID, v.Channel, v.Scope, v.Reason, v.EffectiveAt, v.ExpiresAt, v.Active, v.SourceReference, v.CreatedBy, v.ClientRequestID, v.RequestFingerprint, v.Version, v.CreatedAt)
	if err != nil {
		return consent.Suppression{}, false, err
	}
	if err = insertConsentEvent(ctx, tx, v.ContactID, "", v.ID, "SUPPRESSION_CREATED", v.CreatedBy, v.Reason, v.SourceReference, v.CreatedAt); err != nil {
		return consent.Suppression{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return consent.Suppression{}, false, err
	}
	return v, true, nil
}
func (r *ConsentLedgerRepository) RevokeSuppression(ctx context.Context, id string, in consent.RevokeSuppressionInput, now time.Time) (consent.Suppression, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return consent.Suppression{}, err
	}
	defer tx.Rollback()
	v, err := scanSuppression(tx.QueryRowContext(ctx, suppressionSelect+` WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return consent.Suppression{}, consent.ErrSuppressionNotFound
	}
	if err != nil {
		return consent.Suppression{}, err
	}
	if v.Version != in.ExpectedVersion {
		return consent.Suppression{}, consent.ErrLedgerConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE suppressions SET active=false,revoked_by=$2::uuid,revoked_at=$3,revoke_reason=$4,version=version+1 WHERE id=$1 AND version=$5`, id, in.ActorID, now, in.Reason, in.ExpectedVersion)
	if err != nil {
		return consent.Suppression{}, err
	}
	if err = insertConsentEvent(ctx, tx, v.ContactID, "", v.ID, "SUPPRESSION_REVOKED", in.ActorID, in.Reason, "", now); err != nil {
		return consent.Suppression{}, err
	}
	if err = tx.Commit(); err != nil {
		return consent.Suppression{}, err
	}
	v.Active = false
	v.RevokedBy = in.ActorID
	v.RevokedAt = &now
	v.RevokeReason = in.Reason
	v.Version++
	return v, nil
}
func (r *ConsentLedgerRepository) Events(ctx context.Context, contactID string, limit int) ([]consent.ConsentEvent, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,coalesce(contact_id::text,''),coalesce(grant_id::text,''),coalesce(suppression_id::text,''),event_type,coalesce(actor_id::text,''),coalesce(reason,''),coalesce(source_reference,''),occurred_at FROM consent_events WHERE ($1='' OR contact_id=NULLIF($1,'')::uuid) ORDER BY occurred_at DESC LIMIT $2`, contactID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []consent.ConsentEvent{}
	for rows.Next() {
		var v consent.ConsentEvent
		if err := rows.Scan(&v.ID, &v.ContactID, &v.GrantID, &v.SuppressionID, &v.EventType, &v.ActorID, &v.Reason, &v.SourceReference, &v.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

const grantSelect = `SELECT id::text,contact_id::text,organisation_id::text,purpose_id,channel,consent_review_id::text,wording_version,source_type,coalesce(source_reference,''),coalesce(evidence_object_key,''),evidence_checksum,effective_from,granted_at,expires_at,status,created_by::text,client_request_id,request_fingerprint,version,created_at,updated_at FROM consent_grants`

func scanGrant(row scanner) (consent.Grant, error) {
	var v consent.Grant
	var status string
	var expires sql.NullTime
	err := row.Scan(&v.ID, &v.ContactID, &v.OrganisationID, &v.PurposeID, &v.Channel, &v.ConsentReviewID, &v.WordingVersion, &v.SourceType, &v.SourceReference, &v.EvidenceObjectKey, &v.EvidenceChecksum, &v.EffectiveFrom, &v.GrantedAt, &expires, &status, &v.CreatedBy, &v.ClientRequestID, &v.RequestFingerprint, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	v.Status = consent.GrantStatus(status)
	if expires.Valid {
		t := expires.Time
		v.ExpiresAt = &t
	}
	return v, err
}

const suppressionSelect = `SELECT id::text,coalesce(contact_id::text,''),coalesce(msisdn_lookup_hmac,''::bytea),coalesce(organisation_id::text,''),coalesce(purpose_id,''),coalesce(channel,''),scope,reason,effective_at,expires_at,active,coalesce(source_reference,''),created_by::text,client_request_id,request_fingerprint,coalesce(revoked_by::text,''),revoked_at,coalesce(revoke_reason,''),version,created_at FROM suppressions`

func scanSuppression(row scanner) (consent.Suppression, error) {
	var v consent.Suppression
	var scope string
	var expires, revoked sql.NullTime
	err := row.Scan(&v.ID, &v.ContactID, &v.MSISDNLookupHMAC, &v.OrganisationID, &v.PurposeID, &v.Channel, &scope, &v.Reason, &v.EffectiveAt, &expires, &v.Active, &v.SourceReference, &v.CreatedBy, &v.ClientRequestID, &v.RequestFingerprint, &v.RevokedBy, &revoked, &v.RevokeReason, &v.Version, &v.CreatedAt)
	v.Scope = consent.SuppressionScope(scope)
	if expires.Valid {
		t := expires.Time
		v.ExpiresAt = &t
	}
	if revoked.Valid {
		t := revoked.Time
		v.RevokedAt = &t
	}
	return v, err
}
func insertConsentEvent(ctx context.Context, tx *sql.Tx, contact, grant, suppression, eventType, actor, reason, source string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO consent_events(id,contact_id,grant_id,suppression_id,event_type,actor_id,reason,source_reference,occurred_at) VALUES(gen_random_uuid(),NULLIF($1,'')::uuid,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,$4,NULLIF($5,'')::uuid,NULLIF($6,''),NULLIF($7,''),$8)`, contact, grant, suppression, eventType, actor, reason, source, at)
	return err
}

func (r *ConsentLedgerRepository) EventPage(ctx context.Context, contactID string, limit int, before *time.Time, beforeID string) ([]consent.ConsentEvent, error) {
	if limit <= 0 || limit > 1001 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,coalesce(contact_id::text,''),coalesce(grant_id::text,''),coalesce(suppression_id::text,''),event_type,coalesce(actor_id::text,''),coalesce(reason,''),coalesce(source_reference,''),occurred_at
FROM consent_events
WHERE ($1='' OR contact_id=NULLIF($1,'')::uuid)
  AND ($3::timestamptz IS NULL OR occurred_at<$3 OR (occurred_at=$3 AND id<NULLIF($4,'')::uuid))
ORDER BY occurred_at DESC,id DESC LIMIT $2`, contactID, limit, before, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]consent.ConsentEvent, 0, limit)
	for rows.Next() {
		var value consent.ConsentEvent
		if err := rows.Scan(&value.ID, &value.ContactID, &value.GrantID, &value.SuppressionID, &value.EventType, &value.ActorID, &value.Reason, &value.SourceReference, &value.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}
