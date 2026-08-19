package metacloud

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"campaign-platform/internal/delivery"
	sharedcrypto "campaign-platform/internal/shared/crypto"
)

var (
	ErrWebhookSenderMismatch          = errors.New("Meta webhook provider message is bound to a different sender")
	errWebhookDeliveryResolverInvalid = errors.New("Meta webhook delivery resolver is not fully configured")
)

type PostgreSQLWebhookDeliveryResolver struct {
	DB         *sql.DB
	Deliveries *delivery.Service
	Protector  *sharedcrypto.MSISDNProtector
}

func (r *PostgreSQLWebhookDeliveryResolver) ResolveMetaWebhookRecipient(ctx context.Context, sender Sender, providerMessageID string) (delivery.Recipient, error) {
	if r == nil || r.DB == nil || r.Deliveries == nil || strings.TrimSpace(sender.ID) == "" || strings.TrimSpace(sender.OrganisationID) == "" {
		return delivery.Recipient{}, errWebhookDeliveryResolverInvalid
	}
	if strings.TrimSpace(providerMessageID) == "" {
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	const query = `
SELECT cr.id::text
FROM campaign_recipients cr
JOIN campaigns cp ON cp.id=cr.campaign_id
LEFT JOIN campaign_dispatch_shards sh ON sh.id=cr.dispatch_shard_id
LEFT JOIN campaign_routing_plan_pools rp
  ON rp.routing_plan_id=sh.routing_plan_id
 AND rp.sender_pool_id=sh.assigned_sender_pool_id
LEFT JOIN campaign_routing_plan_quarantined_routes qrp
  ON qrp.routing_plan_id=sh.routing_plan_id
 AND qrp.sender_pool_id=sh.assigned_sender_pool_id
WHERE cr.provider_message_id=$1
  AND cp.organisation_id=$2::uuid
  AND (
    (sh.id IS NOT NULL AND sh.routing_plan_id IS NOT NULL AND (
      (rp.provider='META' AND rp.engine='CLOUD_API' AND rp.meta_sender_id=$3::uuid)
      OR
      (rp.routing_plan_id IS NULL AND qrp.provider='META' AND qrp.engine='CLOUD_API' AND qrp.meta_sender_id=$3::uuid)
    ))
    OR
    (cr.dispatch_shard_id IS NULL AND (
      EXISTS (
        SELECT 1
        FROM campaign_routing_plans frozen_plan
        JOIN campaign_routing_plan_pools frozen_route ON frozen_route.routing_plan_id=frozen_plan.id
        WHERE frozen_plan.campaign_id=cp.id
          AND frozen_route.provider='META' AND frozen_route.engine='CLOUD_API'
          AND frozen_route.meta_sender_id=$3::uuid
      )
      OR EXISTS (
        SELECT 1
        FROM campaign_routing_plan_quarantined_routes frozen_route
        WHERE frozen_route.campaign_id=cp.id
          AND frozen_route.provider='META' AND frozen_route.engine='CLOUD_API'
          AND frozen_route.meta_sender_id=$3::uuid
      )
      OR (
        NOT EXISTS (SELECT 1 FROM campaign_routing_plans frozen_plan WHERE frozen_plan.campaign_id=cp.id)
        AND cp.transport_provider='META' AND cp.transport_engine='CLOUD_API' AND cp.meta_sender_id=$3::uuid
      )
    ))
  )
LIMIT 2`
	rows, err := r.DB.QueryContext(ctx, query, strings.TrimSpace(providerMessageID), strings.TrimSpace(sender.OrganisationID), strings.TrimSpace(sender.ID))
	if err != nil {
		return delivery.Recipient{}, err
	}
	defer rows.Close()
	ids := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return delivery.Recipient{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return delivery.Recipient{}, err
	}
	if len(ids) == 0 {
		var providerMessageExists bool
		if err := r.DB.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM campaign_recipients cr
    WHERE cr.provider_message_id=$1
)`, strings.TrimSpace(providerMessageID)).Scan(&providerMessageExists); err != nil {
			return delivery.Recipient{}, err
		}
		if providerMessageExists {
			return delivery.Recipient{}, ErrWebhookSenderMismatch
		}
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	if len(ids) != 1 {
		return delivery.Recipient{}, errWebhookDeliveryResolverInvalid
	}
	item, err := r.Deliveries.Get(ctx, ids[0])
	if errors.Is(err, delivery.ErrRecipientNotFound) {
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	return item, err
}

func (r *PostgreSQLWebhookDeliveryResolver) ResolveMetaWebhookInboundRecipient(ctx context.Context, sender Sender, senderMSISDN string) (delivery.Recipient, error) {
	if r == nil || r.DB == nil || r.Deliveries == nil || r.Protector == nil || strings.TrimSpace(sender.ID) == "" || strings.TrimSpace(sender.OrganisationID) == "" {
		return delivery.Recipient{}, errWebhookDeliveryResolverInvalid
	}
	e164, err := sharedcrypto.NormalizeE164(senderMSISDN)
	if err != nil {
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	const query = `
SELECT cr.id::text
FROM contacts c
JOIN campaign_recipients cr ON cr.contact_id=c.id
JOIN campaigns cp ON cp.id=cr.campaign_id
LEFT JOIN campaign_dispatch_shards sh ON sh.id=cr.dispatch_shard_id
LEFT JOIN campaign_routing_plan_pools rp
  ON rp.routing_plan_id=sh.routing_plan_id
 AND rp.sender_pool_id=sh.assigned_sender_pool_id
LEFT JOIN campaign_routing_plan_quarantined_routes qrp
  ON qrp.routing_plan_id=sh.routing_plan_id
 AND qrp.sender_pool_id=sh.assigned_sender_pool_id
WHERE c.msisdn_lookup_hmac=$1
  AND cp.organisation_id=$2::uuid
  AND (
    (sh.id IS NOT NULL AND sh.routing_plan_id IS NOT NULL AND (
      (rp.provider='META' AND rp.engine='CLOUD_API' AND rp.meta_sender_id=$3::uuid)
      OR
      (rp.routing_plan_id IS NULL AND qrp.provider='META' AND qrp.engine='CLOUD_API' AND qrp.meta_sender_id=$3::uuid)
    ))
    OR
    (cr.dispatch_shard_id IS NULL AND (
      EXISTS (
        SELECT 1
        FROM campaign_routing_plans frozen_plan
        JOIN campaign_routing_plan_pools frozen_route ON frozen_route.routing_plan_id=frozen_plan.id
        WHERE frozen_plan.campaign_id=cp.id
          AND frozen_route.provider='META' AND frozen_route.engine='CLOUD_API'
          AND frozen_route.meta_sender_id=$3::uuid
      )
      OR EXISTS (
        SELECT 1
        FROM campaign_routing_plan_quarantined_routes frozen_route
        WHERE frozen_route.campaign_id=cp.id
          AND frozen_route.provider='META' AND frozen_route.engine='CLOUD_API'
          AND frozen_route.meta_sender_id=$3::uuid
      )
      OR (
        NOT EXISTS (SELECT 1 FROM campaign_routing_plans frozen_plan WHERE frozen_plan.campaign_id=cp.id)
        AND cp.transport_provider='META' AND cp.transport_engine='CLOUD_API' AND cp.meta_sender_id=$3::uuid
      )
    ))
  )
ORDER BY CASE WHEN cr.provider_message_id IS NOT NULL OR cr.submitted_at IS NOT NULL THEN 1 ELSE 0 END DESC,
         coalesce(cr.last_event_at,cr.submitted_at,cr.updated_at) DESC,cr.id DESC
LIMIT 1`
	var recipientID string
	err = r.DB.QueryRowContext(ctx, query, r.Protector.LookupHMAC(e164), strings.TrimSpace(sender.OrganisationID), strings.TrimSpace(sender.ID)).Scan(&recipientID)
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	if err != nil {
		return delivery.Recipient{}, err
	}
	item, err := r.Deliveries.Get(ctx, recipientID)
	if errors.Is(err, delivery.ErrRecipientNotFound) {
		return delivery.Recipient{}, delivery.ErrRecipientNotFound
	}
	return item, err
}
