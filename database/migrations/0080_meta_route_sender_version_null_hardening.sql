BEGIN;

-- Migration 0075 allowed META route rows with a NULL meta_sender_version because
-- PostgreSQL CHECK constraints accept UNKNOWN. Quarantine any such legacy plan
-- before installing a NULL-safe durable route-evidence fence.
CREATE TEMP TABLE invalid_meta_sender_version_plans ON COMMIT DROP AS
SELECT DISTINCT routing_plan_id
FROM campaign_routing_plan_pools
WHERE provider='META' AND meta_sender_version IS NULL;

INSERT INTO campaign_routing_plan_quarantined_routes(
    routing_plan_id,campaign_id,sender_pool_id,provider,engine,meta_sender_id,meta_sender_version
)
SELECT route_row.routing_plan_id,plan.campaign_id,route_row.sender_pool_id,
       route_row.provider,route_row.engine,route_row.meta_sender_id,route_row.meta_sender_version
FROM campaign_routing_plan_pools route_row
JOIN campaign_routing_plans plan ON plan.id=route_row.routing_plan_id
JOIN invalid_meta_sender_version_plans invalid ON invalid.routing_plan_id=route_row.routing_plan_id
ON CONFLICT (routing_plan_id,sender_pool_id) DO NOTHING;

INSERT INTO campaign_routing_plan_quarantine_evidence(
    routing_plan_id,campaign_id,reason,route_evidence,reservation_evidence
)
SELECT plan.id,plan.campaign_id,'META_SENDER_VERSION_MISSING',
       coalesce((SELECT jsonb_agg(to_jsonb(route_row) ORDER BY route_row.sender_pool_id::text)
                 FROM campaign_routing_plan_pools route_row WHERE route_row.routing_plan_id=plan.id),'[]'::jsonb),
       coalesce((SELECT jsonb_agg(to_jsonb(reservation_row) ORDER BY reservation_row.id::text)
                 FROM campaign_pool_capacity_reservations reservation_row WHERE reservation_row.routing_plan_id=plan.id),'[]'::jsonb)
FROM campaign_routing_plans plan
JOIN invalid_meta_sender_version_plans invalid ON invalid.routing_plan_id=plan.id
ON CONFLICT (routing_plan_id) DO NOTHING;

UPDATE campaign_pool_capacity_reservations reservation
SET status='RELEASED', fencing_version=fencing_version+1, updated_at=now()
WHERE reservation.routing_plan_id IN (SELECT routing_plan_id FROM invalid_meta_sender_version_plans)
  AND reservation.status IN ('HELD','ACTIVE');

DELETE FROM campaign_routing_plan_pools route_row
WHERE route_row.routing_plan_id IN (SELECT routing_plan_id FROM invalid_meta_sender_version_plans);

ALTER TABLE campaign_routing_plan_pools
    DROP CONSTRAINT campaign_routing_plan_pools_endpoint_check,
    ADD CONSTRAINT campaign_routing_plan_pools_endpoint_check CHECK (
        (provider='OPENWA' AND gateway_pool_id IS NOT NULL AND meta_sender_id IS NULL AND meta_sender_version IS NULL) OR
        (provider='META' AND gateway_pool_id IS NULL AND meta_sender_id IS NOT NULL
         AND meta_sender_version IS NOT NULL AND meta_sender_version > 0)
    ),
    ADD CONSTRAINT campaign_routing_plan_pools_meta_sender_version_frozen_check CHECK (
        provider <> 'META' OR meta_sender_version IS NOT NULL
    );

COMMIT;
