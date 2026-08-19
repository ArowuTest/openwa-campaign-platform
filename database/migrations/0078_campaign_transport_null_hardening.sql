BEGIN;

-- Earlier transport checks could evaluate to SQL UNKNOWN and therefore admit
-- partially frozen routes. Do not guess missing provider evidence during upgrade:
-- normalize only incoherent route evidence back to the safe unfrozen state.
UPDATE campaigns
SET transport_provider = NULL,
    transport_engine = NULL,
    transport_routing_mode = NULL,
    gateway_pool_id = NULL,
    gateway_pool_version = NULL,
    transport_session_id = NULL,
    transport_sender_pool_id = NULL,
    meta_sender_id = NULL,
    provider_adapter_version = NULL,
    provider_capability_definition_id = NULL,
    provider_capability_definition_version = NULL,
    required_capabilities = '[]'::jsonb,
    routing_policy_version = NULL,
    capacity_evidence_version = NULL
WHERE NOT (
    (transport_provider IS NULL AND transport_engine IS NULL AND transport_routing_mode IS NULL
     AND transport_session_id IS NULL AND transport_sender_pool_id IS NULL
     AND coalesce(btrim(gateway_pool_id),'')='' AND meta_sender_id IS NULL)
    OR
    (coalesce(transport_provider,'')='OPENWA' AND coalesce(transport_engine,'') IN ('WHATSAPP_WEB_JS','BAILEYS')
     AND coalesce(transport_routing_mode,'')='SPECIFIC_SESSION' AND transport_session_id IS NOT NULL
     AND transport_sender_pool_id IS NULL AND coalesce(btrim(gateway_pool_id),'')<>'' AND meta_sender_id IS NULL)
    OR
    (coalesce(transport_provider,'')='OPENWA' AND coalesce(transport_engine,'') IN ('WHATSAPP_WEB_JS','BAILEYS')
     AND coalesce(transport_routing_mode,'')='SENDER_POOL' AND transport_sender_pool_id IS NOT NULL
     AND transport_session_id IS NULL AND coalesce(btrim(gateway_pool_id),'')<>'' AND meta_sender_id IS NULL)
    OR
    (coalesce(transport_provider,'')='META' AND coalesce(transport_engine,'')='CLOUD_API'
     AND coalesce(transport_routing_mode,'')='SENDER_POOL' AND transport_sender_pool_id IS NOT NULL
     AND transport_session_id IS NULL AND coalesce(btrim(gateway_pool_id),'')='' AND meta_sender_id IS NOT NULL)
);
ALTER TABLE campaigns
    DROP CONSTRAINT campaigns_transport_provider_engine_check,
    DROP CONSTRAINT campaigns_transport_route_check,
    ADD CONSTRAINT campaigns_transport_provider_engine_check CHECK (
        (transport_provider IS NULL AND transport_engine IS NULL)
        OR
        (coalesce(transport_provider,'')='OPENWA' AND coalesce(transport_engine,'') IN ('WHATSAPP_WEB_JS','BAILEYS'))
        OR
        (coalesce(transport_provider,'')='META' AND coalesce(transport_engine,'')='CLOUD_API')
    ),
    ADD CONSTRAINT campaigns_transport_route_check CHECK (
        (
            transport_routing_mode IS NULL
            AND transport_provider IS NULL
            AND transport_engine IS NULL
            AND transport_session_id IS NULL
            AND transport_sender_pool_id IS NULL
            AND coalesce(btrim(gateway_pool_id),'')=''
            AND meta_sender_id IS NULL
        )
        OR
        (
            coalesce(transport_provider,'')='OPENWA'
            AND coalesce(transport_routing_mode,'')='SPECIFIC_SESSION'
            AND transport_session_id IS NOT NULL
            AND transport_sender_pool_id IS NULL
            AND coalesce(btrim(gateway_pool_id),'')<>''
            AND meta_sender_id IS NULL
        )
        OR
        (
            coalesce(transport_provider,'')='OPENWA'
            AND coalesce(transport_routing_mode,'')='SENDER_POOL'
            AND transport_sender_pool_id IS NOT NULL
            AND transport_session_id IS NULL
            AND coalesce(btrim(gateway_pool_id),'')<>''
            AND meta_sender_id IS NULL
        )
        OR
        (
            coalesce(transport_provider,'')='META'
            AND coalesce(transport_routing_mode,'')='SENDER_POOL'
            AND transport_sender_pool_id IS NOT NULL
            AND transport_session_id IS NULL
            AND coalesce(btrim(gateway_pool_id),'')=''
            AND meta_sender_id IS NOT NULL
        )
    );

COMMIT;
