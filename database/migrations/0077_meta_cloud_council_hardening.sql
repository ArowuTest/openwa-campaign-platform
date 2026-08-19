BEGIN;

CREATE TABLE meta_cloud_template_sync_state (
    organisation_id uuid NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
    waba_id text NOT NULL CHECK (btrim(waba_id) <> ''),
    last_synced_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organisation_id, waba_id)
);

-- 0075's transport checks could evaluate to SQL UNKNOWN when routing mode was
-- NULL. If such a partial row also carried Meta sender evidence, adding the
-- stricter 0077 coherence check would abort the upgrade. Do not guess missing
-- route authority: return only those incoherent rows to the safe unfrozen state.
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
WHERE meta_sender_id IS NOT NULL
  AND NOT (
      coalesce(transport_provider,'')='META'
      AND coalesce(transport_engine,'')='CLOUD_API'
      AND coalesce(transport_routing_mode,'')='SENDER_POOL'
      AND transport_sender_pool_id IS NOT NULL
      AND transport_session_id IS NULL
      AND coalesce(btrim(gateway_pool_id),'')=''
  );
ALTER TABLE campaigns
    ADD CONSTRAINT campaigns_meta_sender_route_coherence CHECK (
        meta_sender_id IS NULL OR (
            coalesce(transport_provider,'')='META'
            AND coalesce(transport_engine,'')='CLOUD_API'
            AND coalesce(transport_routing_mode,'')='SENDER_POOL'
            AND transport_sender_pool_id IS NOT NULL
            AND transport_session_id IS NULL
            AND coalesce(btrim(gateway_pool_id),'')=''
        )
    );

COMMIT;
