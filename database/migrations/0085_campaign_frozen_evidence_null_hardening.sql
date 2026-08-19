BEGIN;

-- 0081 left two PostgreSQL CHECK UNKNOWN/partial-evidence holes on campaigns.
-- Fail-close existing mixed frozen evidence instead of guessing authority.
UPDATE campaigns
SET gateway_pool_version=NULL,
    provider_adapter_version=NULL,
    provider_capability_definition_id=NULL,
    provider_capability_definition_version=NULL
WHERE (
    provider_capability_definition_id IS NOT NULL
    AND (
        provider_capability_definition_version IS NULL
        OR provider_capability_definition_version <= 0
        OR coalesce(btrim(provider_adapter_version),'')=''
        OR transport_provider IS NULL
        OR transport_provider NOT IN ('OPENWA','META')
        OR (transport_provider='OPENWA' AND (gateway_pool_version IS NULL OR gateway_pool_version <= 0))
        OR (transport_provider='META' AND gateway_pool_version IS NOT NULL)
    )
) OR (
    provider_capability_definition_id IS NULL
    AND (
        provider_capability_definition_version IS NOT NULL
        OR gateway_pool_version IS NOT NULL
        OR coalesce(btrim(provider_adapter_version),'')<>''
    )
);

ALTER TABLE campaigns
    DROP CONSTRAINT campaigns_provider_capability_binding_pair,
    ADD CONSTRAINT campaigns_provider_capability_binding_pair CHECK (
        (provider_capability_definition_id IS NULL
         AND provider_capability_definition_version IS NULL
         AND gateway_pool_version IS NULL
         AND coalesce(btrim(provider_adapter_version),'')='')
        OR
        (provider_capability_definition_id IS NOT NULL
         AND provider_capability_definition_version IS NOT NULL
         AND provider_capability_definition_version > 0
         AND coalesce(btrim(provider_adapter_version),'')<>''
         AND transport_provider IS NOT NULL
         AND (
             (transport_provider='OPENWA' AND gateway_pool_version IS NOT NULL AND gateway_pool_version > 0)
             OR (transport_provider='META' AND gateway_pool_version IS NULL)
         ))
    );

ALTER TABLE campaigns
    DROP CONSTRAINT campaigns_frozen_version_null_safe_check,
    ADD CONSTRAINT campaigns_frozen_version_null_safe_check CHECK (
        provider_capability_definition_id IS NULL
        OR (
            provider_capability_definition_version IS NOT NULL
            AND transport_provider IS NOT NULL
            AND (transport_provider<>'OPENWA' OR gateway_pool_version IS NOT NULL)
        )
    );

COMMIT;
