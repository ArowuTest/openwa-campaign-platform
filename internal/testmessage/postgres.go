package testmessage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type PostgreSQLRepository struct{ DB *sql.DB }

func scanRecipient(row interface{ Scan(...any) error }) (Recipient, error) {
	var v Recipient
	var status string
	err := row.Scan(&v.ID, &v.Label, &v.MSISDNEncrypted, &v.MSISDNLookupHash, &v.MaskedMSISDN, &status, &v.CreatedBy, &v.SubmittedBy, &v.ApprovedBy, &v.Reason, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	v.Status = RecipientStatus(status)
	return v, err
}
func (r *PostgreSQLRepository) CreateRecipient(ctx context.Context, v Recipient) (Recipient, error) {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO approved_test_recipients(id,label,msisdn_encrypted,msisdn_lookup_hash,masked_msisdn,status,created_by,reason,version,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7::uuid,$8,$9,$10,$10)`, v.ID, v.Label, v.MSISDNEncrypted, v.MSISDNLookupHash, v.MaskedMSISDN, v.Status, v.CreatedBy, v.Reason, v.Version, v.CreatedAt)
	if err != nil {
		return Recipient{}, err
	}
	return v, nil
}
func (r *PostgreSQLRepository) GetRecipient(ctx context.Context, id string) (Recipient, error) {
	v, err := scanRecipient(r.DB.QueryRowContext(ctx, `SELECT id::text,label,msisdn_encrypted,msisdn_lookup_hash,masked_msisdn,status,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,version,created_at,updated_at FROM approved_test_recipients WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Recipient{}, ErrNotFound
	}
	return v, err
}
func (r *PostgreSQLRepository) ListRecipients(ctx context.Context) ([]Recipient, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,label,msisdn_encrypted,msisdn_lookup_hash,masked_msisdn,status,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,version,created_at,updated_at FROM approved_test_recipients ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recipient{}
	for rows.Next() {
		v, err := scanRecipient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgreSQLRepository) CompareAndSwapRecipient(ctx context.Context, v Recipient, expected int64) (Recipient, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE approved_test_recipients SET status=$2,submitted_by=NULLIF($3,'')::uuid,approved_by=NULLIF($4,'')::uuid,reason=$5,version=version+1,updated_at=$6 WHERE id=$1::uuid AND version=$7`, v.ID, v.Status, v.SubmittedBy, v.ApprovedBy, v.Reason, v.UpdatedAt, expected)
	if err != nil {
		return Recipient{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return Recipient{}, ErrConflict
	}
	v.Version = expected + 1
	return v, nil
}
func scanSend(row interface{ Scan(...any) error }) (Send, error) {
	var v Send
	var status string
	var values []byte
	var lease, completed sql.NullTime
	var owner, providerID, failure sql.NullString
	err := row.Scan(&v.ID, &v.CampaignID, &v.MessageVersionID, &v.MessageContentHash, &v.TestRecipientID, &v.GatewayPoolID, &v.SenderPoolID, &v.Provider, &v.Engine, &v.SenderSessionID, &values, &status, &providerID, &failure, &v.CreatedBy, &v.Reason, &v.IdempotencyKey, &v.AttemptCount, &owner, &v.LeaseVersion, &lease, &v.CreatedAt, &v.UpdatedAt, &completed)
	if err != nil {
		return Send{}, err
	}
	v.Status = SendStatus(status)
	_ = json.Unmarshal(values, &v.VariableValues)
	if owner.Valid {
		v.LeaseOwner = owner.String
	}
	if providerID.Valid {
		v.ProviderMessageID = providerID.String
	}
	if failure.Valid {
		v.FailureCode = failure.String
	}
	if lease.Valid {
		x := lease.Time
		v.LeaseExpiresAt = &x
	}
	if completed.Valid {
		x := completed.Time
		v.CompletedAt = &x
	}
	return v, nil
}

const sendSelect = `SELECT id::text,campaign_id::text,message_version_id::text,message_content_hash,test_recipient_id::text,gateway_pool_id::text,coalesce(sender_pool_id::text,''),provider,engine,sender_session_id::text,variable_values,status,provider_message_id,failure_code,created_by::text,reason,idempotency_key,attempt_count,lease_owner,lease_version,lease_expires_at,created_at,updated_at,completed_at FROM test_message_sends`

func (r *PostgreSQLRepository) CreateSend(ctx context.Context, v Send) (Send, error) {
	values, _ := json.Marshal(v.VariableValues)
	_, err := r.DB.ExecContext(ctx, `INSERT INTO test_message_sends(id,campaign_id,message_version_id,message_content_hash,test_recipient_id,gateway_pool_id,sender_pool_id,provider,engine,sender_session_id,variable_values,status,created_by,reason,idempotency_key,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5::uuid,$6::uuid,NULLIF($7,'')::uuid,$8,$9,$10::uuid,$11::jsonb,$12,$13::uuid,$14,$15,$16,$16)`, v.ID, v.CampaignID, v.MessageVersionID, v.MessageContentHash, v.TestRecipientID, v.GatewayPoolID, v.SenderPoolID, v.Provider, v.Engine, v.SenderSessionID, values, v.Status, v.CreatedBy, v.Reason, v.IdempotencyKey, v.CreatedAt)
	if err != nil {
		return Send{}, err
	}
	return v, nil
}
func (r *PostgreSQLRepository) GetSend(ctx context.Context, id string) (Send, error) {
	v, err := scanSend(r.DB.QueryRowContext(ctx, sendSelect+` WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Send{}, ErrNotFound
	}
	return v, err
}
func (r *PostgreSQLRepository) ListSends(ctx context.Context, campaignID string) ([]Send, error) {
	q := sendSelect
	args := []any{}
	if campaignID != "" {
		q += ` WHERE campaign_id=$1::uuid`
		args = append(args, campaignID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := r.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Send{}
	for rows.Next() {
		v, err := scanSend(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgreSQLRepository) ClaimSends(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]Send, error) {
	rows, err := r.DB.QueryContext(ctx, `WITH c AS (SELECT id FROM test_message_sends WHERE (status='PENDING' AND (attempt_count=0 OR updated_at<=$1-interval '30 seconds')) OR (status='PROCESSING' AND lease_expires_at<=$1) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT $2) UPDATE test_message_sends t SET status='PROCESSING',lease_owner=$3,lease_version=t.lease_version+1,lease_expires_at=$4,attempt_count=t.attempt_count+1,updated_at=$1 FROM c WHERE t.id=c.id RETURNING t.id::text,t.campaign_id::text,t.message_version_id::text,t.message_content_hash,t.test_recipient_id::text,t.gateway_pool_id::text,coalesce(t.sender_pool_id::text,''),t.provider,t.engine,t.sender_session_id::text,t.variable_values,t.status,t.provider_message_id,t.failure_code,t.created_by::text,t.reason,t.idempotency_key,t.attempt_count,t.lease_owner,t.lease_version,t.lease_expires_at,t.created_at,t.updated_at,t.completed_at`, now, limit, owner, now.Add(lease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Send{}
	for rows.Next() {
		v, err := scanSend(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgreSQLRepository) CompleteSend(ctx context.Context, v Send, status SendStatus, providerID, code string, now time.Time) error {
	var completed any
	if status == SendAccepted || status == SendFailed || status == SendUnknown {
		completed = now
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE test_message_sends SET status=$4,provider_message_id=NULLIF($5,''),failure_code=NULLIF($6,''),lease_owner=NULL,lease_expires_at=NULL,updated_at=$7,completed_at=$8 WHERE id=$1::uuid AND lease_owner=$2 AND lease_version=$3`, v.ID, v.LeaseOwner, v.LeaseVersion, status, providerID, code, now, completed)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrLeaseConflict
	}
	return nil
}

func (r *PostgreSQLRepository) ValidateTestRoute(ctx context.Context, gatewayPoolID, senderPoolID, provider, engine, sessionID string) error {
	var valid bool
	err := r.DB.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM sender_sessions s
 JOIN gateway_pools g ON g.id=s.gateway_pool_id
 LEFT JOIN sender_pools p ON p.id=s.sender_pool_id
 WHERE s.id=$1::uuid AND s.gateway_pool_id=$2::uuid AND g.provider=$3 AND g.engine=$4
   AND g.status='ACTIVE' AND s.status IN ('READY','BUSY')
   AND ($5='' OR s.sender_pool_id=$5::uuid)
   AND ($5='' OR p.status='ACTIVE')
)`, sessionID, gatewayPoolID, provider, engine, senderPoolID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrInvalid
	}
	return nil
}
