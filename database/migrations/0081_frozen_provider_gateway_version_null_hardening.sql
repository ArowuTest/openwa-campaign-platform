BEGIN;

-- 0075 pair CHECKs used `version > 0` without explicit NULL tests. PostgreSQL
-- accepts CHECK UNKNOWN, so a bound route/campaign could carry a NULL frozen
-- provider or OpenWA gateway version. Fail-close approved routing plans and
-- unfreeze legacy campaign transport evidence rather than inventing versions.
CREATE TEMP TABLE invalid_frozen_version_plans ON COMMIT DROP AS
SELECT DISTINCT routing_plan_id
FROM campaign_routing_plan_pools
WHERE provider_capability_definition_id IS NOT NULL
  AND (
      provider_capability_definition_version IS NULL
      OR (provider='OPENWA' AND gateway_pool_version IS NULL)
  );

INSERT INTO campaign_routing_plan_quarantined_routes(
    routing_plan_id,campaign_id,sender_pool_id,provider,engine,meta_sender_id,meta_sender_version
)
SELECT route_row.routing_plan_id,plan.campaign_id,route_row.sender_pool_id,
       route_row.provider,route_row.engine,route_row.meta_sender_id,route_row.meta_sender_version
FROM campaign_routing_plan_pools route_row
JOIN campaign_routing_plans plan ON plan.id=route_row.routing_plan_id
JOIN invalid_frozen_version_plans invalid ON invalid.routing_plan_id=route_row.routing_plan_id
ON CONFLICT (routing_plan_id,sender_pool_id) DO NOTHING;
INSERT INTO campaign_routing_plan_quarantine_evidence(
    routing_plan_id,campaign_id,reason,route_evidence,reservation_evidence
)
SELECT plan.id,plan.campaign_id,'FROZEN_PROVIDER_OR_GATEWAY_VERSION_MISSING',
       coalesce((SELECT jsonb_agg(to_jsonb(route_row) ORDER BY route_row.sender_pool_id::text)
                 FROM campaign_routing_plan_pools route_row WHERE route_row.routing_plan_id=plan.id),'[]'::jsonb),
       coalesce((SELECT jsonb_agg(to_jsonb(reservation_row) ORDER BY reservation_row.id::text)
                 FROM campaign_pool_capacity_reservations reservation_row WHERE reservation_row.routing_plan_id=plan.id),'[]'::jsonb)
FROM campaign_routing_plans plan
JOIN invalid_frozen_version_plans invalid ON invalid.routing_plan_id=plan.id
ON CONFLICT (routing_plan_id) DO NOTHING;

UPDATE campaign_pool_capacity_reservations reservation
SET status='RELEASED', fencing_version=fencing_version+1, updated_at=now()
WHERE reservation.routing_plan_id IN (SELECT routing_plan_id FROM invalid_frozen_version_plans)
  AND reservation.status IN ('HELD','ACTIVE');

DELETE FROM campaign_routing_plan_pools route_row
WHERE route_row.routing_plan_id IN (SELECT routing_plan_id FROM invalid_frozen_version_plans);

-- The singular campaign transport record is legacy/base authority. If its
-- supposedly frozen pair is partial, reset it to the same fail-closed unfrozen
-- representation used by 0078/0079; routing-plan evidence is governed separately.
UPDATE campaigns
SET transport_provider=NULL,
    transport_engine=NULL,
    transport_routing_mode=NULL,
    gateway_pool_id=NULL,
    gateway_pool_version=NULL,
    transport_session_id=NULL,
    transport_sender_pool_id=NULL,
    meta_sender_id=NULL,
    provider_adapter_version=NULL,
    provider_capability_definition_id=NULL,
    provider_capability_definition_version=NULL,
    required_capabilities='[]'::jsonb,
    routing_policy_version=NULL,
    capacity_evidence_version=NULL
WHERE provider_capability_definition_id IS NOT NULL
  AND (
      provider_capability_definition_version IS NULL
      OR (transport_provider='OPENWA' AND gateway_pool_version IS NULL)
  );

ALTER TABLE campaign_routing_plan_pools
    DROP CONSTRAINT campaign_route_provider_binding_pair,
    ADD CONSTRAINT campaign_route_provider_binding_pair CHECK (
        (provider_capability_definition_id IS NULL AND provider_capability_definition_version IS NULL
         AND gateway_pool_version IS NULL AND coalesce(btrim(provider_adapter_version),'')='')
        OR
        (provider_capability_definition_id IS NOT NULL
         AND provider_capability_definition_version IS NOT NULL
         AND provider_capability_definition_version > 0
         AND coalesce(btrim(provider_adapter_version),'')<>''
         AND (
             (provider='OPENWA' AND gateway_pool_version IS NOT NULL AND gateway_pool_version > 0)
             OR (provider='META' AND gateway_pool_version IS NULL)
         ))
    ),
    ADD CONSTRAINT campaign_route_frozen_version_null_safe_check CHECK (
        provider_capability_definition_id IS NULL
        OR (
            provider_capability_definition_version IS NOT NULL
            AND (provider<>'OPENWA' OR gateway_pool_version IS NOT NULL)
        )
    );

ALTER TABLE campaigns
    DROP CONSTRAINT campaigns_provider_capability_binding_pair,
    ADD CONSTRAINT campaigns_provider_capability_binding_pair CHECK (
        (provider_capability_definition_id IS NULL AND provider_capability_definition_version IS NULL
         AND gateway_pool_version IS NULL)
        OR
        (provider_capability_definition_id IS NOT NULL
         AND provider_capability_definition_version IS NOT NULL
         AND provider_capability_definition_version > 0
         AND coalesce(btrim(provider_adapter_version),'')<>''
         AND (
             (transport_provider='OPENWA' AND gateway_pool_version IS NOT NULL AND gateway_pool_version > 0)
             OR (transport_provider='META' AND gateway_pool_version IS NULL)
         ))
    ),
    ADD CONSTRAINT campaigns_frozen_version_null_safe_check CHECK (
        provider_capability_definition_id IS NULL
        OR (
            provider_capability_definition_version IS NOT NULL
            AND (transport_provider<>'OPENWA' OR gateway_pool_version IS NOT NULL)
        )
    );

COMMIT;
