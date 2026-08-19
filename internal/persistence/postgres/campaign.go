package postgres

import (
	"campaign-platform/internal/campaign"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type CampaignRepository struct{ DB *sql.DB }

func (r *CampaignRepository) Create(ctx context.Context, v campaign.Campaign) error {
	if r.DB == nil {
		return errors.New("database is required")
	}
	requiredCapabilities := v.Transport.RequiredCapabilities
	if requiredCapabilities == nil {
		requiredCapabilities = []string{}
	}
	capabilities, err := json.Marshal(requiredCapabilities)
	if err != nil {
		return fmt.Errorf("marshal campaign capabilities: %w", err)
	}
	const q = `INSERT INTO campaigns(
 id,organisation_id,name,purpose_id,consent_review_id,status,requested_start_at,completion_deadline_at,
 campaign_timezone,quiet_hours_start,quiet_hours_end,maximum_unique_recipients,maximum_messages_per_recipient,
 sender_pool,transport_channel,transport_provider,transport_engine,transport_routing_mode,gateway_pool_id,gateway_pool_version,
 transport_session_id,transport_sender_pool_id,provider_adapter_version,provider_capability_definition_id,
 provider_capability_definition_version,required_capabilities,fallback_mode,routing_policy_version,
 capacity_evidence_version,created_by,created_at,updated_at,version,meta_sender_id
) VALUES(
 $1::uuid,$2::uuid,$3,$4::uuid,$5::uuid,$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),$12,$13,
 NULLIF($14,''),NULLIF($15,''),NULLIF($16,''),NULLIF($17,''),NULLIF($18,''),NULLIF($19,''),NULLIF($20,0),NULLIF($21,'')::uuid,NULLIF($22,'')::uuid,NULLIF($23,''),
 NULLIF($24,'')::uuid,NULLIF($25,0),$26,coalesce(NULLIF($27,''),'NONE'),NULLIF($28,''),NULLIF($29,''),NULLIF($30,'')::uuid,$31,$31,$32,NULLIF($33,'')::uuid
)`
	_, err = r.DB.ExecContext(ctx, q,
		v.ID, v.OrganisationID, v.Name, v.PurposeID, v.ConsentReviewID, v.Status,
		v.RequestedStartAt, v.CompletionDeadlineAt, v.Timezone, v.QuietHoursStart, v.QuietHoursEnd,
		v.MaximumUniqueRecipients, v.MaximumMessagesPerRecipient, v.SenderPool, v.Transport.Channel,
		v.Transport.Provider, v.Transport.Engine, v.Transport.RoutingMode, v.Transport.GatewayPoolID,
		v.Transport.GatewayPoolVersion, v.Transport.SessionID, v.Transport.SenderPoolID, v.Transport.AdapterVersion,
		v.Transport.ProviderDefinitionID, v.Transport.ProviderDefinitionVersion, string(capabilities),
		v.Transport.FallbackMode, v.Transport.RoutingPolicyVersion, v.Transport.CapacityEvidenceVersion,
		v.CreatedBy, v.CreatedAt, v.Version, v.Transport.MetaSenderID,
	)
	if err != nil {
		return fmt.Errorf("create campaign: %w", err)
	}
	return nil
}

type campaignExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (r *CampaignRepository) CompareAndSwap(ctx context.Context, v campaign.Campaign, expected int64) error {
	if r == nil || r.DB == nil {
		return errors.New("database is required")
	}
	return r.compareAndSwapWith(ctx, r.DB, v, expected)
}

func (r *CampaignRepository) CompareAndSwapInTx(
	ctx context.Context,
	tx *sql.Tx,
	v campaign.Campaign,
	expected int64,
) error {
	if r == nil || r.DB == nil || tx == nil {
		return errors.New("database transaction is required")
	}
	return r.compareAndSwapWith(ctx, tx, v, expected)
}

func (r *CampaignRepository) compareAndSwapWith(
	ctx context.Context,
	executor campaignExecutor,
	v campaign.Campaign,
	expected int64,
) error {
	if executor == nil {
		return errors.New("database executor is required")
	}
	requiredCapabilities := v.Transport.RequiredCapabilities
	if requiredCapabilities == nil {
		requiredCapabilities = []string{}
	}
	capabilities, err := json.Marshal(requiredCapabilities)
	if err != nil {
		return fmt.Errorf("marshal campaign capabilities: %w", err)
	}
	const q = `UPDATE campaigns SET
 status=$2,requested_start_at=$3,completion_deadline_at=$4,campaign_timezone=$5,
 quiet_hours_start=NULLIF($6,''),quiet_hours_end=NULLIF($7,''),maximum_unique_recipients=$8,
 audience_snapshot_id=NULLIF($9,'')::uuid,approved_message_version_id=NULLIF($10,'')::uuid,
 final_approved_by=NULLIF($11,'')::uuid,
 final_approved_at=CASE WHEN NULLIF($11,'') IS NULL THEN NULL ELSE coalesce(final_approved_at,$12) END,
 sender_pool=NULLIF($13,''),pause_reason=NULLIF($14,''),commercial_approval_id=NULLIF($15,'')::uuid,
 transport_channel=NULLIF($16,''),transport_provider=NULLIF($17,''),transport_engine=NULLIF($18,''),transport_routing_mode=NULLIF($19,''),gateway_pool_id=NULLIF($20,''),gateway_pool_version=NULLIF($21,0),
 transport_session_id=NULLIF($22,'')::uuid,transport_sender_pool_id=NULLIF($23,'')::uuid,
 provider_adapter_version=NULLIF($24,''),provider_capability_definition_id=NULLIF($25,'')::uuid,
 provider_capability_definition_version=NULLIF($26,0),required_capabilities=$27,fallback_mode=coalesce(NULLIF($28,''),'NONE'),
 routing_policy_version=NULLIF($29,''),capacity_evidence_version=NULLIF($30,''),updated_at=$12,version=$31,meta_sender_id=NULLIF($33,'')::uuid,maximum_messages_per_recipient=$34
WHERE id=$1::uuid AND version=$32`
	res, err := executor.ExecContext(ctx, q,
		v.ID, v.Status, v.RequestedStartAt, v.CompletionDeadlineAt, v.Timezone, v.QuietHoursStart,
		v.QuietHoursEnd, v.MaximumUniqueRecipients, v.AudienceSnapshotID, v.MessageVersionID,
		v.FinalApprovedBy, v.UpdatedAt, v.SenderPool, v.PauseReason, v.CommercialApprovalID,
		v.Transport.Channel, v.Transport.Provider, v.Transport.Engine, v.Transport.RoutingMode,
		v.Transport.GatewayPoolID, v.Transport.GatewayPoolVersion, v.Transport.SessionID, v.Transport.SenderPoolID,
		v.Transport.AdapterVersion, v.Transport.ProviderDefinitionID, v.Transport.ProviderDefinitionVersion,
		string(capabilities), v.Transport.FallbackMode, v.Transport.RoutingPolicyVersion,
		v.Transport.CapacityEvidenceVersion, v.Version, expected, v.Transport.MetaSenderID, v.MaximumMessagesPerRecipient,
	)
	if err != nil {
		return fmt.Errorf("update campaign: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect campaign update: %w", err)
	}
	if n == 0 {
		return campaign.ErrConflict
	}
	return nil
}

func (r *CampaignRepository) ListPage(ctx context.Context, limit int, before *time.Time, beforeID string) ([]campaign.Campaign, error) {
	if r.DB == nil {
		return nil, errors.New("database is required")
	}
	if limit <= 0 || limit > 501 {
		limit = 101
	}
	query := campaignSelect
	args := []any{}
	if before != nil {
		query += ` WHERE (campaigns.created_at,campaigns.id)<($1,$2::uuid)`
		args = append(args, before.UTC(), beforeID)
	}
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY campaigns.created_at DESC,campaigns.id DESC LIMIT $%d`, len(args))
	rows, err := r.DB.QueryContext(ctx, query, args...)
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
	v, err := scanCampaign(r.DB.QueryRowContext(ctx, campaignSelect+` WHERE campaigns.id=$1::uuid`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return campaign.Campaign{}, campaign.ErrNotFound
	}
	return v, err
}

const campaignSelect = `SELECT
 campaigns.id::text,campaigns.organisation_id::text,campaigns.name,campaigns.purpose_id::text,coalesce(campaigns.consent_review_id::text,''),campaigns.status,
 campaigns.requested_start_at,campaigns.completion_deadline_at,coalesce(campaigns.campaign_timezone,'UTC'),coalesce(campaigns.quiet_hours_start,''),
 coalesce(campaigns.quiet_hours_end,''),campaigns.maximum_unique_recipients,campaigns.maximum_messages_per_recipient,
 coalesce(campaigns.audience_snapshot_id::text,''),coalesce(snapshot.snapshot_hash,''),coalesce(snapshot.eligible_count,0),
 coalesce(campaigns.approved_message_version_id::text,''),coalesce(message.content_hash,''),
 coalesce(campaigns.sender_pool,''),coalesce(campaigns.transport_channel,''),coalesce(campaigns.transport_provider,''),coalesce(campaigns.transport_engine,''),
 coalesce(campaigns.transport_routing_mode,''),coalesce(campaigns.gateway_pool_id,''),coalesce(campaigns.gateway_pool_version,0),coalesce(campaigns.transport_session_id::text,''),
 coalesce(campaigns.transport_sender_pool_id::text,''),coalesce(campaigns.provider_adapter_version,''),
 coalesce(campaigns.provider_capability_definition_id::text,''),coalesce(campaigns.provider_capability_definition_version,0),
 coalesce(campaigns.required_capabilities,'[]'::jsonb),coalesce(campaigns.fallback_mode,'NONE'),coalesce(campaigns.routing_policy_version,''),
 coalesce(campaigns.capacity_evidence_version,''),coalesce(campaigns.created_by::text,''),coalesce(campaigns.final_approved_by::text,''),
 coalesce(campaigns.commercial_approval_id::text,''),coalesce(campaigns.pause_reason,''),campaigns.created_at,campaigns.updated_at,campaigns.version,coalesce(campaigns.meta_sender_id::text,'')
FROM campaigns
LEFT JOIN audience_snapshots snapshot ON snapshot.id=campaigns.audience_snapshot_id
LEFT JOIN message_versions message ON message.id=campaigns.approved_message_version_id`

func scanCampaign(row scanner) (campaign.Campaign, error) {
	var v campaign.Campaign
	var status string
	var start, deadline sql.NullTime
	var caps []byte
	err := row.Scan(
		&v.ID, &v.OrganisationID, &v.Name, &v.PurposeID, &v.ConsentReviewID, &status, &start, &deadline,
		&v.Timezone, &v.QuietHoursStart, &v.QuietHoursEnd, &v.MaximumUniqueRecipients,
		&v.MaximumMessagesPerRecipient, &v.AudienceSnapshotID, &v.AudienceSnapshotHash,
		&v.EligibleAudienceCount, &v.MessageVersionID, &v.MessageContentHash, &v.SenderPool,
		&v.Transport.Channel, &v.Transport.Provider, &v.Transport.Engine, &v.Transport.RoutingMode,
		&v.Transport.GatewayPoolID, &v.Transport.GatewayPoolVersion, &v.Transport.SessionID, &v.Transport.SenderPoolID,
		&v.Transport.AdapterVersion, &v.Transport.ProviderDefinitionID, &v.Transport.ProviderDefinitionVersion,
		&caps, &v.Transport.FallbackMode, &v.Transport.RoutingPolicyVersion,
		&v.Transport.CapacityEvidenceVersion, &v.CreatedBy, &v.FinalApprovedBy,
		&v.CommercialApprovalID, &v.PauseReason, &v.CreatedAt, &v.UpdatedAt, &v.Version, &v.Transport.MetaSenderID,
	)
	if err != nil {
		return campaign.Campaign{}, err
	}
	v.Status = campaign.Status(status)
	if err := json.Unmarshal(caps, &v.Transport.RequiredCapabilities); err != nil {
		return campaign.Campaign{}, fmt.Errorf("decode campaign required capabilities: %w", err)
	}
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

func (r *CampaignRepository) AmendMaterial(ctx context.Context, v campaign.Campaign, event campaign.MaterialChangeEvent, expected int64) error {
	if r.DB == nil {
		return errors.New("database is required")
	}
	requiredCapabilities := v.Transport.RequiredCapabilities
	if requiredCapabilities == nil {
		requiredCapabilities = []string{}
	}
	capabilities, err := json.Marshal(requiredCapabilities)
	if err != nil {
		return fmt.Errorf("marshal campaign capabilities: %w", err)
	}
	changedFields, err := json.Marshal(event.ChangedFields)
	if err != nil {
		return fmt.Errorf("marshal campaign material fields: %w", err)
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const update = `UPDATE campaigns SET
 status=$2,requested_start_at=$3,completion_deadline_at=$4,campaign_timezone=$5,
 quiet_hours_start=NULLIF($6,''),quiet_hours_end=NULLIF($7,''),maximum_unique_recipients=$8,
 audience_snapshot_id=NULLIF($9,'')::uuid,approved_message_version_id=NULLIF($10,'')::uuid,
 final_approved_by=NULL,final_approved_at=NULL,sender_pool=NULLIF($11,''),commercial_approval_id=NULLIF($12,'')::uuid,
 transport_channel=NULLIF($13,''),transport_provider=NULLIF($14,''),transport_engine=NULLIF($15,''),transport_routing_mode=NULLIF($16,''),gateway_pool_id=NULLIF($17,''),gateway_pool_version=NULLIF($18,0),
 transport_session_id=NULLIF($19,'')::uuid,transport_sender_pool_id=NULLIF($20,'')::uuid,
 provider_adapter_version=NULLIF($21,''),provider_capability_definition_id=NULLIF($22,'')::uuid,
 provider_capability_definition_version=NULLIF($23,0),required_capabilities=$24,fallback_mode=coalesce(NULLIF($25,''),'NONE'),
 routing_policy_version=NULLIF($26,''),capacity_evidence_version=NULLIF($27,''),updated_at=$28,version=$29,meta_sender_id=NULLIF($31,'')::uuid,maximum_messages_per_recipient=$32
WHERE id=$1::uuid AND version=$30`
	res, err := tx.ExecContext(ctx, update,
		v.ID, v.Status, v.RequestedStartAt, v.CompletionDeadlineAt, v.Timezone, v.QuietHoursStart,
		v.QuietHoursEnd, v.MaximumUniqueRecipients, v.AudienceSnapshotID, v.MessageVersionID,
		v.SenderPool, v.CommercialApprovalID, v.Transport.Channel, v.Transport.Provider,
		v.Transport.Engine, v.Transport.RoutingMode, v.Transport.GatewayPoolID, v.Transport.GatewayPoolVersion, v.Transport.SessionID,
		v.Transport.SenderPoolID, v.Transport.AdapterVersion, v.Transport.ProviderDefinitionID,
		v.Transport.ProviderDefinitionVersion, string(capabilities), v.Transport.FallbackMode,
		v.Transport.RoutingPolicyVersion, v.Transport.CapacityEvidenceVersion, v.UpdatedAt,
		v.Version, expected, v.Transport.MetaSenderID, v.MaximumMessagesPerRecipient,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return campaign.ErrConflict
	}
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(sequence),0)+1 FROM campaign_material_change_events WHERE campaign_id=$1::uuid`, v.ID).Scan(&seq); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO campaign_material_change_events(id,campaign_id,sequence,actor_id,reason,changed_fields,previous_status,new_status,previous_version,new_version,created_at) VALUES($1::uuid,$2::uuid,$3,$4::uuid,$5,$6,$7,$8,$9,$10,$11)`, event.ID, event.CampaignID, seq, event.ActorID, event.Reason, string(changedFields), event.PreviousStatus, event.NewStatus, event.PreviousVersion, event.NewVersion, event.CreatedAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *CampaignRepository) ListMaterialChangePage(ctx context.Context, campaignID string, limit int, afterSequence int64) ([]campaign.MaterialChangeEvent, error) {
	if r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,campaign_id::text,sequence,actor_id::text,reason,changed_fields,previous_status,new_status,previous_version,new_version,created_at FROM campaign_material_change_events WHERE campaign_id=$1::uuid AND sequence>$2 ORDER BY sequence ASC LIMIT $3`, campaignID, afterSequence, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []campaign.MaterialChangeEvent{}
	for rows.Next() {
		var event campaign.MaterialChangeEvent
		var fields []byte
		var previous, next string
		if err := rows.Scan(&event.ID, &event.CampaignID, &event.Sequence, &event.ActorID, &event.Reason, &fields, &previous, &next, &event.PreviousVersion, &event.NewVersion, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.PreviousStatus, event.NewStatus = campaign.Status(previous), campaign.Status(next)
		if err := json.Unmarshal(fields, &event.ChangedFields); err != nil {
			return nil, fmt.Errorf("decode campaign material fields: %w", err)
		}
		items = append(items, event)
	}
	return items, rows.Err()
}

func (r *CampaignRepository) ListMaterialChanges(ctx context.Context, campaignID string) ([]campaign.MaterialChangeEvent, error) {
	if r.DB == nil {
		return nil, errors.New("database is required")
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id::text,campaign_id::text,sequence,actor_id::text,reason,changed_fields,previous_status,new_status,previous_version,new_version,created_at FROM campaign_material_change_events WHERE campaign_id=$1::uuid ORDER BY sequence`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []campaign.MaterialChangeEvent
	for rows.Next() {
		var e campaign.MaterialChangeEvent
		var fields []byte
		var prev, next string
		if err := rows.Scan(&e.ID, &e.CampaignID, &e.Sequence, &e.ActorID, &e.Reason, &fields, &prev, &next, &e.PreviousVersion, &e.NewVersion, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.PreviousStatus, e.NewStatus = campaign.Status(prev), campaign.Status(next)
		if err := json.Unmarshal(fields, &e.ChangedFields); err != nil {
			return nil, fmt.Errorf("decode campaign material fields: %w", err)
		}
		items = append(items, e)
	}
	return items, rows.Err()
}
