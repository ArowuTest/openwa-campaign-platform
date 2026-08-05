package testmessage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"campaign-platform/internal/provider"
)

type PostgreSQLRepository struct{ DB *sql.DB }

func scanRecipient(row interface{ Scan(...any) error }) (Recipient, error) {
	var v Recipient
	var status string
	if err := row.Scan(&v.ID, &v.Label, &v.MSISDNEncrypted, &v.MSISDNLookupHash, &v.MaskedMSISDN, &status, &v.CreatedBy, &v.SubmittedBy, &v.ApprovedBy, &v.Reason, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return Recipient{}, err
	}
	v.Status = RecipientStatus(status)
	return v, nil
}

func (r *PostgreSQLRepository) CreateRecipient(ctx context.Context, v Recipient) (Recipient, error) {
	if r == nil || r.DB == nil {
		return Recipient{}, errors.New("test-message database is required")
	}
	_, err := r.DB.ExecContext(ctx, `INSERT INTO approved_test_recipients(id,label,msisdn_encrypted,msisdn_lookup_hash,masked_msisdn,status,created_by,reason,version,created_at,updated_at) VALUES($1::uuid,$2,$3,$4,$5,$6,$7::uuid,$8,$9,$10,$10)`, v.ID, v.Label, v.MSISDNEncrypted, v.MSISDNLookupHash, v.MaskedMSISDN, v.Status, v.CreatedBy, v.Reason, v.Version, v.CreatedAt)
	if err != nil {
		return Recipient{}, fmt.Errorf("create approved test recipient: %w", err)
	}
	return v, nil
}

func (r *PostgreSQLRepository) GetRecipient(ctx context.Context, id string) (Recipient, error) {
	if r == nil || r.DB == nil {
		return Recipient{}, errors.New("test-message database is required")
	}
	v, err := scanRecipient(r.DB.QueryRowContext(ctx, `SELECT id::text,label,msisdn_encrypted,msisdn_lookup_hash,masked_msisdn,status,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,version,created_at,updated_at FROM approved_test_recipients WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Recipient{}, ErrNotFound
	}
	if err != nil {
		return Recipient{}, fmt.Errorf("get approved test recipient: %w", err)
	}
	return v, nil
}

func (r *PostgreSQLRepository) ListRecipients(ctx context.Context) ([]Recipient, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("test-message database is required")
	}
	// Approved test recipients are an administrative reference set, but still cap
	// the response to prevent a malformed or abused list request from exhausting
	// the API process. A future cursor endpoint can expose deeper history.
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,label,msisdn_encrypted,msisdn_lookup_hash,masked_msisdn,status,created_by::text,coalesce(submitted_by::text,''),coalesce(approved_by::text,''),reason,version,created_at,updated_at FROM approved_test_recipients ORDER BY created_at DESC,id DESC LIMIT 1000`)
	if err != nil {
		return nil, fmt.Errorf("list approved test recipients: %w", err)
	}
	defer rows.Close()
	out := []Recipient{}
	for rows.Next() {
		v, err := scanRecipient(rows)
		if err != nil {
			return nil, fmt.Errorf("scan approved test recipient: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate approved test recipients: %w", err)
	}
	return out, nil
}

func (r *PostgreSQLRepository) CompareAndSwapRecipient(ctx context.Context, v Recipient, expected int64) (Recipient, error) {
	if r == nil || r.DB == nil {
		return Recipient{}, errors.New("test-message database is required")
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE approved_test_recipients SET status=$2,submitted_by=NULLIF($3,'')::uuid,approved_by=NULLIF($4,'')::uuid,reason=$5,version=version+1,updated_at=$6 WHERE id=$1::uuid AND version=$7`, v.ID, v.Status, v.SubmittedBy, v.ApprovedBy, v.Reason, v.UpdatedAt, expected)
	if err != nil {
		return Recipient{}, fmt.Errorf("update approved test recipient: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Recipient{}, fmt.Errorf("inspect approved test recipient update: %w", err)
	}
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
	if err := row.Scan(
		&v.ID, &v.CampaignID, &v.MessageVersionID, &v.MessageContentHash, &v.TestRecipientID,
		&v.GatewayPoolID, &v.GatewayPoolVersion, &v.SenderPoolID, &v.Provider, &v.Engine,
		&v.ProviderAdapterVersion, &v.ProviderDefinitionID, &v.ProviderDefinitionVersion,
		&v.SenderSessionID, &values, &status, &providerID, &failure,
		&v.CreatedBy, &v.Reason, &v.IdempotencyKey, &v.AttemptCount, &owner,
		&v.LeaseVersion, &lease, &v.CreatedAt, &v.UpdatedAt, &completed,
	); err != nil {
		return Send{}, err
	}
	v.Status = SendStatus(status)
	if err := json.Unmarshal(values, &v.VariableValues); err != nil {
		return Send{}, fmt.Errorf("decode test-message variable values: %w", err)
	}
	if v.VariableValues == nil {
		v.VariableValues = map[string]string{}
	}
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

const sendSelect = `SELECT id::text,campaign_id::text,message_version_id::text,message_content_hash,test_recipient_id::text,gateway_pool_id::text,coalesce(gateway_pool_version,0),coalesce(sender_pool_id::text,''),provider,engine,coalesce(provider_adapter_version,''),coalesce(provider_capability_definition_id::text,''),coalesce(provider_capability_definition_version,0),sender_session_id::text,variable_values,status,provider_message_id,failure_code,created_by::text,reason,idempotency_key,attempt_count,lease_owner,lease_version,lease_expires_at,created_at,updated_at,completed_at FROM test_message_sends`

func (r *PostgreSQLRepository) CreateSend(ctx context.Context, v Send) (Send, error) {
	if r == nil || r.DB == nil {
		return Send{}, errors.New("test-message database is required")
	}
	values, err := json.Marshal(v.VariableValues)
	if err != nil {
		return Send{}, fmt.Errorf("encode test-message variable values: %w", err)
	}
	_, err = r.DB.ExecContext(ctx, `INSERT INTO test_message_sends(id,campaign_id,message_version_id,message_content_hash,test_recipient_id,gateway_pool_id,gateway_pool_version,sender_pool_id,provider,engine,provider_adapter_version,provider_capability_definition_id,provider_capability_definition_version,sender_session_id,variable_values,status,created_by,reason,idempotency_key,created_at,updated_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5::uuid,$6::uuid,$7,NULLIF($8,'')::uuid,$9,$10,$11,$12::uuid,$13,$14::uuid,$15::jsonb,$16,$17::uuid,$18,$19,$20,$20) ON CONFLICT (idempotency_key) DO NOTHING`, v.ID, v.CampaignID, v.MessageVersionID, v.MessageContentHash, v.TestRecipientID, v.GatewayPoolID, v.GatewayPoolVersion, v.SenderPoolID, v.Provider, v.Engine, v.ProviderAdapterVersion, v.ProviderDefinitionID, v.ProviderDefinitionVersion, v.SenderSessionID, values, v.Status, v.CreatedBy, v.Reason, v.IdempotencyKey, v.CreatedAt)
	if err != nil {
		return Send{}, fmt.Errorf("create test-message send: %w", err)
	}
	existing, err := scanSend(r.DB.QueryRowContext(ctx, sendSelect+` WHERE idempotency_key=$1`, v.IdempotencyKey))
	if err != nil {
		return Send{}, fmt.Errorf("read test-message idempotency result: %w", err)
	}
	if !sameSendRequest(existing, v) {
		return Send{}, ErrConflict
	}
	return existing, nil
}

func sameSendRequest(a, b Send) bool {
	if a.CampaignID != b.CampaignID || a.MessageVersionID != b.MessageVersionID || a.MessageContentHash != b.MessageContentHash ||
		a.TestRecipientID != b.TestRecipientID || a.GatewayPoolID != b.GatewayPoolID || a.GatewayPoolVersion != b.GatewayPoolVersion || a.SenderPoolID != b.SenderPoolID ||
		a.Provider != b.Provider || a.Engine != b.Engine || a.ProviderAdapterVersion != b.ProviderAdapterVersion ||
		a.ProviderDefinitionID != b.ProviderDefinitionID || a.ProviderDefinitionVersion != b.ProviderDefinitionVersion ||
		a.SenderSessionID != b.SenderSessionID || a.CreatedBy != b.CreatedBy || a.Reason != b.Reason {
		return false
	}
	if len(a.VariableValues) != len(b.VariableValues) {
		return false
	}
	for k, v := range a.VariableValues {
		if b.VariableValues[k] != v {
			return false
		}
	}
	return true
}

func (r *PostgreSQLRepository) GetSend(ctx context.Context, id string) (Send, error) {
	if r == nil || r.DB == nil {
		return Send{}, errors.New("test-message database is required")
	}
	v, err := scanSend(r.DB.QueryRowContext(ctx, sendSelect+` WHERE id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Send{}, ErrNotFound
	}
	if err != nil {
		return Send{}, fmt.Errorf("get test-message send: %w", err)
	}
	return v, nil
}

func (r *PostgreSQLRepository) ListSends(ctx context.Context, campaignID string) ([]Send, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("test-message database is required")
	}
	q := sendSelect
	args := []any{}
	if campaignID != "" {
		q += ` WHERE campaign_id=$1::uuid`
		args = append(args, campaignID)
	}
	q += ` ORDER BY created_at DESC,id DESC LIMIT 1000`
	rows, err := r.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list test-message sends: %w", err)
	}
	defer rows.Close()
	out := []Send{}
	for rows.Next() {
		v, err := scanSend(rows)
		if err != nil {
			return nil, fmt.Errorf("scan test-message send: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate test-message sends: %w", err)
	}
	return out, nil
}

func (r *PostgreSQLRepository) ClaimSends(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]Send, error) {
	if r == nil || r.DB == nil {
		return nil, errors.New("test-message database is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.DB.QueryContext(ctx, `WITH c AS (SELECT id FROM test_message_sends WHERE (status='PENDING' AND (attempt_count=0 OR updated_at<=$1-interval '30 seconds')) OR (status='PROCESSING' AND lease_expires_at<=$1) ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT $2) UPDATE test_message_sends t SET status='PROCESSING',lease_owner=$3,lease_version=t.lease_version+1,lease_expires_at=$4,attempt_count=t.attempt_count+1,updated_at=$1 FROM c WHERE t.id=c.id RETURNING t.id::text,t.campaign_id::text,t.message_version_id::text,t.message_content_hash,t.test_recipient_id::text,t.gateway_pool_id::text,coalesce(t.gateway_pool_version,0),coalesce(t.sender_pool_id::text,''),t.provider,t.engine,coalesce(t.provider_adapter_version,''),coalesce(t.provider_capability_definition_id::text,''),coalesce(t.provider_capability_definition_version,0),t.sender_session_id::text,t.variable_values,t.status,t.provider_message_id,t.failure_code,t.created_by::text,t.reason,t.idempotency_key,t.attempt_count,t.lease_owner,t.lease_version,t.lease_expires_at,t.created_at,t.updated_at,t.completed_at`, now, limit, owner, now.Add(lease))
	if err != nil {
		return nil, fmt.Errorf("claim test-message sends: %w", err)
	}
	defer rows.Close()
	out := []Send{}
	for rows.Next() {
		v, err := scanSend(rows)
		if err != nil {
			return nil, fmt.Errorf("scan claimed test-message send: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed test-message sends: %w", err)
	}
	return out, nil
}

func (r *PostgreSQLRepository) CompleteSend(ctx context.Context, v Send, status SendStatus, providerID, code string, now time.Time) error {
	if r == nil || r.DB == nil {
		return errors.New("test-message database is required")
	}
	var completed any
	if status == SendAccepted || status == SendFailed || status == SendUnknown {
		completed = now
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE test_message_sends SET status=$4,provider_message_id=NULLIF($5,''),failure_code=NULLIF($6,''),lease_owner=NULL,lease_expires_at=NULL,updated_at=$7,completed_at=$8 WHERE id=$1::uuid AND lease_owner=$2 AND lease_version=$3`, v.ID, v.LeaseOwner, v.LeaseVersion, status, providerID, code, now, completed)
	if err != nil {
		return fmt.Errorf("complete test-message send: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect test-message completion: %w", err)
	}
	if n != 1 {
		return ErrLeaseConflict
	}
	return nil
}

func (r *PostgreSQLRepository) ValidateTestRoute(ctx context.Context, req RouteRequirements) (RouteEvidence, error) {
	if r == nil || r.DB == nil {
		return RouteEvidence{}, errors.New("test-message database is required")
	}
	req.CampaignID = strings.TrimSpace(req.CampaignID)
	req.GatewayPoolID = strings.TrimSpace(req.GatewayPoolID)
	req.SenderPoolID = strings.TrimSpace(req.SenderPoolID)
	req.Provider = strings.ToUpper(strings.TrimSpace(req.Provider))
	req.Engine = strings.ToUpper(strings.TrimSpace(req.Engine))
	req.SessionID = strings.TrimSpace(req.SessionID)
	if req.At.IsZero() {
		req.At = time.Now().UTC()
	}
	if req.CampaignID == "" || req.GatewayPoolID == "" || req.SessionID == "" || req.Provider == "" || req.Engine == "" {
		return RouteEvidence{}, ErrInvalid
	}

	var gatewayAdapter, definitionAdapter, definitionID, minimumGatewayVersion string
	var gatewayVersion, definitionVersion int64
	var gatewayCapsJSON, definitionCapsJSON []byte
	err := r.DB.QueryRowContext(ctx, `
SELECT g.version,g.adapter_version,g.capabilities,pd.adapter_version,pd.id::text,pd.version,
       coalesce(pd.minimum_gateway_version,''),to_json(pd.capabilities)
FROM campaigns c
JOIN sender_sessions s ON s.id=$1::uuid
JOIN gateway_pools g ON g.id=s.gateway_pool_id
LEFT JOIN sender_pools sp ON sp.id=s.sender_pool_id
LEFT JOIN LATERAL (
  SELECT pools.*
  FROM campaign_routing_plans plans
  JOIN campaign_routing_plan_pools pools ON pools.routing_plan_id=plans.id
  WHERE plans.campaign_id=c.id
    AND pools.gateway_pool_id=$3::uuid
    AND ($6='' OR pools.sender_pool_id=nullif($6,'')::uuid)
  ORDER BY plans.plan_version DESC
  LIMIT 1
) route ON true
JOIN provider_capability_definitions pd
  ON pd.id=CASE WHEN route.routing_plan_id IS NOT NULL THEN route.provider_capability_definition_id ELSE c.provider_capability_definition_id END
 AND pd.version=CASE WHEN route.routing_plan_id IS NOT NULL THEN route.provider_capability_definition_version ELSE c.provider_capability_definition_version END
WHERE c.id=$2::uuid
  AND s.gateway_pool_id=$3::uuid
  AND ($6='' OR s.sender_pool_id=nullif($6,'')::uuid)
  AND ($6='' OR sp.status='ACTIVE')
  AND g.provider=$4 AND g.engine=$5
  AND g.status='ACTIVE' AND s.status IN ('READY','BUSY')
  AND pd.provider=$4 AND pd.channel='WHATSAPP' AND pd.engine=$5
  AND pd.status='ACTIVE'
  AND pd.effective_from <= $7
  AND (pd.effective_to IS NULL OR pd.effective_to > $7)
  AND (
    (route.routing_plan_id IS NOT NULL
      AND route.provider=$4 AND route.engine=$5
      AND route.provider_adapter_version=g.adapter_version
      AND route.gateway_pool_version=g.version)
    OR
    (route.routing_plan_id IS NULL
      AND NOT EXISTS (SELECT 1 FROM campaign_routing_plans existing WHERE existing.campaign_id=c.id)
      AND c.transport_provider=$4 AND c.transport_engine=$5
      AND c.provider_adapter_version=g.adapter_version
      AND c.gateway_pool_version=g.version
      AND coalesce(c.gateway_pool_id,'')=$3)
  )
  AND pd.adapter_version=g.adapter_version
LIMIT 1`, req.SessionID, req.CampaignID, req.GatewayPoolID, req.Provider, req.Engine, req.SenderPoolID, req.At.UTC()).Scan(
		&gatewayVersion, &gatewayAdapter, &gatewayCapsJSON, &definitionAdapter, &definitionID, &definitionVersion,
		&minimumGatewayVersion, &definitionCapsJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RouteEvidence{}, ErrInvalid
	}
	if err != nil {
		return RouteEvidence{}, fmt.Errorf("validate test-message route: %w", err)
	}
	if gatewayVersion <= 0 || strings.TrimSpace(gatewayAdapter) == "" || gatewayAdapter != definitionAdapter || strings.TrimSpace(definitionID) == "" || definitionVersion <= 0 {
		return RouteEvidence{}, ErrInvalid
	}
	compatible, err := provider.VersionAtLeast(gatewayAdapter, minimumGatewayVersion)
	if err != nil || !compatible {
		return RouteEvidence{}, ErrInvalid
	}
	var gatewayCapabilities, definitionCapabilities []string
	if err := json.Unmarshal(gatewayCapsJSON, &gatewayCapabilities); err != nil {
		return RouteEvidence{}, fmt.Errorf("decode gateway capabilities: %w", err)
	}
	if err := json.Unmarshal(definitionCapsJSON, &definitionCapabilities); err != nil {
		return RouteEvidence{}, fmt.Errorf("decode provider capabilities: %w", err)
	}
	if !containsCapabilities(gatewayCapabilities, req.RequiredCapabilities) || !containsCapabilities(definitionCapabilities, req.RequiredCapabilities) {
		return RouteEvidence{}, ErrInvalid
	}
	return RouteEvidence{GatewayPoolVersion: gatewayVersion, AdapterVersion: gatewayAdapter, ProviderDefinitionID: definitionID, ProviderDefinitionVersion: definitionVersion}, nil
}

func containsCapabilities(available, required []string) bool {
	set := make(map[string]struct{}, len(available))
	for _, capability := range available {
		capability = strings.ToUpper(strings.TrimSpace(capability))
		if capability != "" {
			set[capability] = struct{}{}
		}
	}
	normalized := append([]string(nil), required...)
	sort.Strings(normalized)
	for _, capability := range normalized {
		if _, ok := set[strings.ToUpper(strings.TrimSpace(capability))]; !ok {
			return false
		}
	}
	return true
}
