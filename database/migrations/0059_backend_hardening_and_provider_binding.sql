BEGIN;

CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE campaigns
  ADD COLUMN IF NOT EXISTS provider_capability_definition_id uuid REFERENCES provider_capability_definitions(id),
  ADD COLUMN IF NOT EXISTS provider_capability_definition_version bigint,
  ADD COLUMN IF NOT EXISTS gateway_pool_version bigint;


ALTER TABLE campaign_routing_plans
  ADD COLUMN IF NOT EXISTS idempotency_key text,
  ADD COLUMN IF NOT EXISTS request_hash text;

UPDATE campaign_routing_plans
SET idempotency_key='legacy:' || id::text,
    request_hash='legacy-unavailable'
WHERE idempotency_key IS NULL OR request_hash IS NULL;

ALTER TABLE campaign_routing_plans
  ALTER COLUMN idempotency_key SET NOT NULL,
  ALTER COLUMN request_hash SET NOT NULL,
  DROP CONSTRAINT IF EXISTS campaign_routing_plan_idempotency_nonempty,
  ADD CONSTRAINT campaign_routing_plan_idempotency_nonempty CHECK (
    length(btrim(idempotency_key)) BETWEEN 1 AND 200
    AND length(btrim(request_hash)) >= 16
  );

CREATE UNIQUE INDEX IF NOT EXISTS uq_campaign_routing_plan_idempotency
  ON campaign_routing_plans(campaign_id,idempotency_key);

ALTER TABLE campaign_routing_plan_pools
  ADD COLUMN IF NOT EXISTS provider_adapter_version text,
  ADD COLUMN IF NOT EXISTS provider_capability_definition_id uuid REFERENCES provider_capability_definitions(id),
  ADD COLUMN IF NOT EXISTS provider_capability_definition_version bigint,
  ADD COLUMN IF NOT EXISTS gateway_pool_version bigint;

ALTER TABLE campaign_routing_plan_pools
  DROP CONSTRAINT IF EXISTS campaign_route_provider_binding_pair,
  ADD CONSTRAINT campaign_route_provider_binding_pair CHECK (
    (provider_capability_definition_id IS NULL AND provider_capability_definition_version IS NULL AND gateway_pool_version IS NULL)
    OR (provider_capability_definition_id IS NOT NULL AND provider_capability_definition_version > 0
        AND gateway_pool_version > 0 AND coalesce(btrim(provider_adapter_version),'') <> '')
  );

ALTER TABLE test_message_sends
  ADD COLUMN IF NOT EXISTS gateway_pool_version bigint,
  ADD COLUMN IF NOT EXISTS provider_adapter_version text,
  ADD COLUMN IF NOT EXISTS provider_capability_definition_id uuid REFERENCES provider_capability_definitions(id),
  ADD COLUMN IF NOT EXISTS provider_capability_definition_version bigint;

ALTER TABLE campaigns
  DROP CONSTRAINT IF EXISTS campaigns_provider_capability_binding_pair,
  ADD CONSTRAINT campaigns_provider_capability_binding_pair CHECK (
    (provider_capability_definition_id IS NULL AND provider_capability_definition_version IS NULL AND gateway_pool_version IS NULL)
    OR (provider_capability_definition_id IS NOT NULL AND provider_capability_definition_version > 0 AND gateway_pool_version > 0 AND coalesce(btrim(provider_adapter_version),'') <> '')
  );

ALTER TABLE test_message_sends
  DROP CONSTRAINT IF EXISTS test_message_provider_capability_binding_pair,
  ADD CONSTRAINT test_message_provider_capability_binding_pair CHECK (
    (provider_capability_definition_id IS NULL AND provider_capability_definition_version IS NULL AND gateway_pool_version IS NULL)
    OR (provider_capability_definition_id IS NOT NULL AND provider_capability_definition_version > 0 AND gateway_pool_version > 0 AND coalesce(btrim(provider_adapter_version),'') <> '')
  );

DROP INDEX IF EXISTS provider_capability_one_active_route;

ALTER TABLE provider_capability_definitions
  DROP CONSTRAINT IF EXISTS provider_capability_active_period_exclusion;

ALTER TABLE provider_capability_definitions
  ADD CONSTRAINT provider_capability_active_period_exclusion
  EXCLUDE USING gist (
    provider WITH =,
    channel WITH =,
    engine WITH =,
    tstzrange(effective_from, coalesce(effective_to, 'infinity'::timestamptz), '[)') WITH &&
  ) WHERE (status = 'ACTIVE');

CREATE INDEX IF NOT EXISTS idx_campaigns_provider_capability_binding
  ON campaigns(provider_capability_definition_id, provider_capability_definition_version)
  WHERE provider_capability_definition_id IS NOT NULL;


CREATE INDEX IF NOT EXISTS idx_campaign_route_provider_binding
  ON campaign_routing_plan_pools(provider_capability_definition_id, provider_capability_definition_version, gateway_pool_id)
  WHERE provider_capability_definition_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_test_message_provider_capability_binding
  ON test_message_sends(provider_capability_definition_id, provider_capability_definition_version)
  WHERE provider_capability_definition_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_campaign_recipients_campaign_contact_keyset
  ON campaign_recipients(campaign_id, contact_id, id);

CREATE INDEX IF NOT EXISTS idx_transactional_outbox_evidence_keyset
  ON transactional_outbox(created_at, id);

CREATE INDEX IF NOT EXISTS idx_campaign_recipients_queue_oldest
  ON campaign_recipients(updated_at, id)
  WHERE status IN ('AUTHORISED','QUEUED','CLAIMED','SUBMITTING');

CREATE INDEX IF NOT EXISTS idx_test_message_sends_created_keyset
  ON test_message_sends(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_test_recipients_created_keyset
  ON approved_test_recipients(created_at DESC, id DESC);

COMMIT;
