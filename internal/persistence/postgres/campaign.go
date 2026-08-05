package postgres

import (
	"campaign-platform/internal/campaign"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type CampaignRepository struct{ DB *sql.DB }

func (r *CampaignRepository) Create(ctx context.Context, v campaign.Campaign) error {
	if r.DB == nil {
		return errors.New("database is required")
	}
	const q = `INSERT INTO campaigns(id,organisation_id,name,purpose_id,consent_review_id,status,requested_start_at,completion_deadline_at,maximum_unique_recipients,maximum_messages_per_recipient,sender_pool,transport_channel,transport_provider,transport_engine,transport_routing_mode,gateway_pool_id,transport_session_id,transport_sender_pool_id,provider_adapter_version,required_capabilities,fallback_mode,routing_policy_version,capacity_evidence_version,created_by,created_at,updated_at,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13,$14,$15,$16,NULLIF($17,'')::uuid,NULLIF($18,'')::uuid,$19,$20,$21,$22,$23,NULLIF($24,'')::uuid,$25,$25,$26)`
	_, err := r.DB.ExecContext(ctx, q, v.ID, v.OrganisationID, v.Name, v.PurposeID, v.ConsentReviewID, v.Status, v.RequestedStartAt, v.CompletionDeadlineAt, v.MaximumUniqueRecipients, v.MaximumMessagesPerRecipient, v.SenderPool, v.Transport.Channel, v.Transport.Provider, v.Transport.Engine, v.Transport.RoutingMode, v.Transport.GatewayPoolID, v.Transport.SessionID, v.Transport.SenderPoolID, v.Transport.AdapterVersion, pqStringArrayJSON(v.Transport.RequiredCapabilities), v.Transport.FallbackMode, v.Transport.RoutingPolicyVersion, v.Transport.CapacityEvidenceVersion, v.CreatedBy, v.CreatedAt, v.Version)
	return err
}
func (r *CampaignRepository) CompareAndSwap(ctx context.Context, v campaign.Campaign, expected int64) error {
	if r.DB == nil {
		return errors.New("database is required")
	}
	const q = `UPDATE campaigns SET status=$2,audience_snapshot_id=NULLIF($3,'')::uuid,approved_message_version_id=NULLIF($4,'')::uuid,final_approved_by=NULLIF($5,'')::uuid,final_approved_at=CASE WHEN NULLIF($5,'') IS NULL THEN final_approved_at ELSE coalesce(final_approved_at,$6) END,sender_pool=NULLIF($7,''),pause_reason=NULLIF($8,''),transport_channel=$9,transport_provider=$10,transport_engine=$11,transport_routing_mode=$12,gateway_pool_id=$13,transport_session_id=NULLIF($14,'')::uuid,transport_sender_pool_id=NULLIF($15,'')::uuid,provider_adapter_version=$16,required_capabilities=$17,fallback_mode=$18,routing_policy_version=$19,capacity_evidence_version=$20,updated_at=$6,version=$21 WHERE id=$1 AND version=$22`
	res, err := r.DB.ExecContext(ctx, q, v.ID, v.Status, v.AudienceSnapshotID, v.MessageVersionID, v.FinalApprovedBy, v.UpdatedAt, v.SenderPool, v.PauseReason, v.Transport.Channel, v.Transport.Provider, v.Transport.Engine, v.Transport.RoutingMode, v.Transport.GatewayPoolID, v.Transport.SessionID, v.Transport.SenderPoolID, v.Transport.AdapterVersion, pqStringArrayJSON(v.Transport.RequiredCapabilities), v.Transport.FallbackMode, v.Transport.RoutingPolicyVersion, v.Transport.CapacityEvidenceVersion, v.Version, expected)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return campaign.ErrConflict
	}
	return nil
}
func (r *CampaignRepository) List(ctx context.Context) ([]campaign.Campaign, error) {
	if r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, campaignSelect+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []campaign.Campaign{}
	for rows.Next() {
		v, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (r *CampaignRepository) Get(ctx context.Context, id string) (campaign.Campaign, error) {
	if r.DB == nil {
		return campaign.Campaign{}, errors.New("database is required")
	}
	v, err := scanCampaign(r.DB.QueryRowContext(ctx, campaignSelect+` WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return campaign.Campaign{}, campaign.ErrNotFound
	}
	return v, err
}

const campaignSelect = `SELECT id,organisation_id,name,purpose_id,coalesce(consent_review_id::text,''),status,requested_start_at,completion_deadline_at,maximum_unique_recipients,maximum_messages_per_recipient,coalesce(audience_snapshot_id::text,''),coalesce((SELECT snapshot_hash FROM audience_snapshots s WHERE s.id=campaigns.audience_snapshot_id),''),coalesce((SELECT eligible_count FROM audience_snapshots s WHERE s.id=campaigns.audience_snapshot_id),0),coalesce(approved_message_version_id::text,''),coalesce((SELECT content_hash FROM message_versions m WHERE m.id=campaigns.approved_message_version_id),''),coalesce(sender_pool,''),coalesce(transport_channel,''),coalesce(transport_provider,''),coalesce(transport_engine,''),coalesce(transport_routing_mode,''),coalesce(gateway_pool_id,''),coalesce(transport_session_id::text,''),coalesce(transport_sender_pool_id::text,''),coalesce(provider_adapter_version,''),coalesce(required_capabilities,'[]'::jsonb),coalesce(fallback_mode,'NONE'),coalesce(routing_policy_version,''),coalesce(capacity_evidence_version,''),coalesce(created_by::text,''),coalesce(final_approved_by::text,''),created_at,updated_at,version FROM campaigns`

func scanCampaign(row scanner) (campaign.Campaign, error) {
	var v campaign.Campaign
	var status string
	var start, deadline sql.NullTime
	var caps []byte
	err := row.Scan(&v.ID, &v.OrganisationID, &v.Name, &v.PurposeID, &v.ConsentReviewID, &status, &start, &deadline, &v.MaximumUniqueRecipients, &v.MaximumMessagesPerRecipient, &v.AudienceSnapshotID, &v.AudienceSnapshotHash, &v.EligibleAudienceCount, &v.MessageVersionID, &v.MessageContentHash, &v.SenderPool, &v.Transport.Channel, &v.Transport.Provider, &v.Transport.Engine, &v.Transport.RoutingMode, &v.Transport.GatewayPoolID, &v.Transport.SessionID, &v.Transport.SenderPoolID, &v.Transport.AdapterVersion, &caps, &v.Transport.FallbackMode, &v.Transport.RoutingPolicyVersion, &v.Transport.CapacityEvidenceVersion, &v.CreatedBy, &v.FinalApprovedBy, &v.CreatedAt, &v.UpdatedAt, &v.Version)
	if err != nil {
		return campaign.Campaign{}, err
	}
	v.Status = campaign.Status(status)
	_ = json.Unmarshal(caps, &v.Transport.RequiredCapabilities)
	if start.Valid {
		t := start.Time
		v.RequestedStartAt = &t
	}
	if deadline.Valid {
		t := deadline.Time
		v.CompletionDeadlineAt = &t
	}
	return v, nil
}

func pqStringArrayJSON(v []string) []byte { b, _ := json.Marshal(v); return b }
