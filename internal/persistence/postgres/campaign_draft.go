package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"campaign-platform/internal/campaign"
)

// SaveDraft owns the aggregate row lock even for a no-op. The bounded update and
// append-only material event share one transaction; execution CAS is unchanged.
func (r *CampaignRepository) SaveDraft(ctx context.Context, next campaign.Campaign, event campaign.MaterialChangeEvent, expected int64) (campaign.Campaign, error) {
	if r == nil || r.DB == nil {
		return campaign.Campaign{}, errors.New("database is required")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return campaign.Campaign{}, err
	}
	defer tx.Rollback()
	current, err := scanCampaign(tx.QueryRowContext(ctx, campaignSelect+` WHERE campaigns.id=$1::uuid FOR UPDATE OF campaigns`, next.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return campaign.Campaign{}, campaign.ErrNotFound
	}
	if err != nil {
		return campaign.Campaign{}, err
	}
	if err = campaign.GuardDraftSave(current, next, event, expected); err != nil {
		return campaign.Campaign{}, err
	}
	if next.Version == expected {
		if err = tx.Commit(); err != nil {
			return campaign.Campaign{}, err
		}
		current.UpdatedAt = current.UpdatedAt.UTC()
		return current, nil
	}
	capabilities := next.Transport.RequiredCapabilities
	if capabilities == nil {
		capabilities = []string{}
	}
	caps, err := json.Marshal(capabilities)
	if err != nil {
		return campaign.Campaign{}, err
	}
	fields, err := json.Marshal(event.ChangedFields)
	if err != nil {
		return campaign.Campaign{}, err
	}
	const update = `UPDATE campaigns SET
 name=$2,requested_start_at=$3,completion_deadline_at=$4,campaign_timezone=$5,
 quiet_hours_start=NULLIF($6,''),quiet_hours_end=NULLIF($7,''),
 maximum_unique_recipients=$8,maximum_messages_per_recipient=$9,sender_pool=NULLIF($10,''),
 transport_channel=$11,transport_provider=$12,transport_engine=$13,transport_routing_mode=$14,
 gateway_pool_id=$15,gateway_pool_version=$16,transport_sender_pool_id=$17::uuid,
 provider_adapter_version=$18,provider_capability_definition_id=$19::uuid,provider_capability_definition_version=$20,
 required_capabilities=$21,fallback_mode=$22,routing_policy_version=$23,capacity_evidence_version=$24,
 transport_session_id=NULL,meta_sender_id=NULL,updated_at=$25,version=$26
 WHERE id=$1::uuid AND version=$27 AND status='DRAFT'`
	t := next.Transport
	result, err := tx.ExecContext(ctx, update, next.ID, next.Name, next.RequestedStartAt, next.CompletionDeadlineAt, next.Timezone,
		next.QuietHoursStart, next.QuietHoursEnd, next.MaximumUniqueRecipients, next.MaximumMessagesPerRecipient, next.SenderPool,
		t.Channel, t.Provider, t.Engine, t.RoutingMode, t.GatewayPoolID, t.GatewayPoolVersion, t.SenderPoolID, t.AdapterVersion,
		t.ProviderDefinitionID, t.ProviderDefinitionVersion, string(caps), t.FallbackMode, t.RoutingPolicyVersion, t.CapacityEvidenceVersion,
		next.UpdatedAt.UTC(), next.Version, expected)
	if err != nil {
		return campaign.Campaign{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return campaign.Campaign{}, err
	}
	if affected != 1 {
		return campaign.Campaign{}, campaign.ErrConflict
	}
	var sequence int64
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(max(sequence),0)+1 FROM campaign_material_change_events WHERE campaign_id=$1::uuid`, next.ID).Scan(&sequence); err != nil {
		return campaign.Campaign{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO campaign_material_change_events(id,campaign_id,sequence,actor_id,reason,changed_fields,previous_status,new_status,previous_version,new_version,created_at)
 VALUES($1::uuid,$2::uuid,$3,$4::uuid,$5,$6,$7,$8,$9,$10,$11)`, event.ID, event.CampaignID, sequence, event.ActorID, event.Reason, string(fields), event.PreviousStatus, event.NewStatus, event.PreviousVersion, event.NewVersion, event.CreatedAt.UTC())
	if err != nil {
		return campaign.Campaign{}, err
	}
	// Read the trigger-authoritative row while the same row lock is held.
	saved, err := scanCampaign(tx.QueryRowContext(ctx, campaignSelect+` WHERE campaigns.id=$1::uuid`, next.ID))
	if err != nil {
		return campaign.Campaign{}, err
	}
	saved.UpdatedAt = saved.UpdatedAt.UTC()
	if err = tx.Commit(); err != nil {
		return campaign.Campaign{}, err
	}
	return saved, nil
}
