BEGIN;
ALTER TABLE campaigns
  ADD COLUMN IF NOT EXISTS transport_channel text,
  ADD COLUMN IF NOT EXISTS transport_provider text,
  ADD COLUMN IF NOT EXISTS transport_engine text,
  ADD COLUMN IF NOT EXISTS transport_routing_mode text,
  ADD COLUMN IF NOT EXISTS gateway_pool_id text,
  ADD COLUMN IF NOT EXISTS transport_session_id uuid REFERENCES sender_sessions(id),
  ADD COLUMN IF NOT EXISTS transport_sender_pool_id uuid REFERENCES sender_pools(id),
  ADD COLUMN IF NOT EXISTS provider_adapter_version text,
  ADD COLUMN IF NOT EXISTS required_capabilities jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS fallback_mode text NOT NULL DEFAULT 'NONE',
  ADD COLUMN IF NOT EXISTS routing_policy_version text,
  ADD COLUMN IF NOT EXISTS capacity_evidence_version text;
ALTER TABLE campaigns ADD CONSTRAINT campaigns_transport_provider_check CHECK (transport_provider IS NULL OR transport_provider='OPENWA');
ALTER TABLE campaigns ADD CONSTRAINT campaigns_transport_engine_check CHECK (transport_engine IS NULL OR transport_engine IN ('WHATSAPP_WEB_JS','BAILEYS'));
ALTER TABLE campaigns ADD CONSTRAINT campaigns_transport_route_check CHECK (
 transport_routing_mode IS NULL OR
 (transport_routing_mode='SPECIFIC_SESSION' AND transport_session_id IS NOT NULL AND transport_sender_pool_id IS NULL) OR
 (transport_routing_mode='SENDER_POOL' AND transport_sender_pool_id IS NOT NULL AND transport_session_id IS NULL)
);
ALTER TABLE campaigns ADD CONSTRAINT campaigns_transport_fallback_check CHECK (fallback_mode='NONE');
COMMIT;
