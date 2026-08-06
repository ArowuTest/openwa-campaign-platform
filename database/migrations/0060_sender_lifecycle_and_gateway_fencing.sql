BEGIN;

ALTER TABLE sender_nodes
  ADD COLUMN IF NOT EXISTS internal_url text,
  ADD COLUMN IF NOT EXISTS provider text,
  ADD COLUMN IF NOT EXISTS engine text,
  ADD COLUMN IF NOT EXISTS adapter_version text,
  ADD COLUMN IF NOT EXISTS boot_id text;

ALTER TABLE sender_nodes DROP CONSTRAINT IF EXISTS sender_nodes_runtime_identity_check;
ALTER TABLE sender_nodes ADD CONSTRAINT sender_nodes_runtime_identity_check CHECK (
  (provider IS NULL AND engine IS NULL)
  OR
  (provider = 'OPENWA' AND engine IN ('WHATSAPP_WEB_JS','BAILEYS')
   AND gateway_pool_id IS NOT NULL AND internal_url IS NOT NULL AND adapter_version IS NOT NULL)
);

ALTER TABLE sender_sessions
  ADD COLUMN IF NOT EXISTS state_volume_reference text;

ALTER TABLE sender_sessions DROP CONSTRAINT IF EXISTS sender_sessions_status_check;
ALTER TABLE sender_sessions
  ADD CONSTRAINT sender_sessions_status_check
  CHECK (status IN (
    'NEW','PAIRING','CONNECTING','READY','BUSY','DRAINING','PAUSED',
    'DISCONNECTED','RECOVERING','FAILED_RECOVERY','RESTRICTED','QUARANTINED','RETIRED'
  ));

CREATE OR REPLACE FUNCTION sender_session_transition_allowed(current_status text, target_status text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
SELECT current_status = target_status OR CASE current_status
  WHEN 'NEW' THEN target_status IN ('PAIRING','RETIRED')
  WHEN 'PAIRING' THEN target_status IN ('CONNECTING','PAUSED','DISCONNECTED','RESTRICTED','RETIRED')
  WHEN 'CONNECTING' THEN target_status IN ('READY','DISCONNECTED','RESTRICTED','RECOVERING')
  WHEN 'READY' THEN target_status IN ('BUSY','DRAINING','PAUSED','DISCONNECTED','RESTRICTED','QUARANTINED','RETIRED')
  WHEN 'BUSY' THEN target_status IN ('READY','DRAINING','PAUSED','DISCONNECTED','RESTRICTED','QUARANTINED')
  WHEN 'DRAINING' THEN target_status IN ('PAUSED','READY','DISCONNECTED','RETIRED')
  WHEN 'PAUSED' THEN target_status IN ('CONNECTING','READY','RECOVERING','DISCONNECTED','RETIRED')
  WHEN 'DISCONNECTED' THEN target_status IN ('CONNECTING','RECOVERING','PAIRING','RESTRICTED','RETIRED')
  WHEN 'RECOVERING' THEN target_status IN ('CONNECTING','READY','FAILED_RECOVERY','RESTRICTED')
  WHEN 'FAILED_RECOVERY' THEN target_status IN ('RECOVERING','PAIRING','RETIRED')
  WHEN 'RESTRICTED' THEN target_status IN ('RECOVERING','PAUSED','RETIRED')
  WHEN 'QUARANTINED' THEN target_status IN ('READY','RETIRED')
  WHEN 'RETIRED' THEN false
  ELSE false
END;
$$;

CREATE TABLE IF NOT EXISTS gateway_session_authorities (
  session_id uuid PRIMARY KEY REFERENCES sender_sessions(id),
  gateway_pool_id uuid NOT NULL REFERENCES gateway_pools(id),
  owner_node_id uuid NOT NULL REFERENCES sender_nodes(id),
  session_lease_version bigint NOT NULL CHECK (session_lease_version > 0),
  session_configuration_version bigint NOT NULL CHECK (session_configuration_version > 0),
  gateway_node_version bigint NOT NULL CHECK (gateway_node_version > 0),
  authority_expires_at timestamptz NOT NULL,
  route_reference text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (length(trim(route_reference)) BETWEEN 1 AND 200)
);

CREATE INDEX IF NOT EXISTS gateway_session_authorities_expiry_idx
  ON gateway_session_authorities(authority_expires_at);

CREATE TABLE IF NOT EXISTS gateway_session_authority_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES sender_sessions(id),
  owner_node_id uuid NOT NULL REFERENCES sender_nodes(id),
  session_lease_version bigint NOT NULL,
  session_configuration_version bigint NOT NULL,
  gateway_node_version bigint NOT NULL,
  authority_expires_at timestamptz NOT NULL,
  route_reference text NOT NULL,
  event_type text NOT NULL CHECK (event_type IN ('ISSUED','REJECTED','EXPIRED','SUPERSEDED')),
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS gateway_session_authority_events_session_time_idx
  ON gateway_session_authority_events(session_id, occurred_at DESC);

ALTER TABLE test_message_sends
  ADD COLUMN IF NOT EXISTS gateway_node_id uuid REFERENCES sender_nodes(id),
  ADD COLUMN IF NOT EXISTS gateway_node_version bigint CHECK (gateway_node_version > 0),
  ADD COLUMN IF NOT EXISTS session_lease_version bigint CHECK (session_lease_version > 0),
  ADD COLUMN IF NOT EXISTS session_configuration_version bigint CHECK (session_configuration_version > 0),
  ADD COLUMN IF NOT EXISTS authority_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS route_reference text;

ALTER TABLE test_message_sends DROP CONSTRAINT IF EXISTS test_message_sends_gateway_authority_check;
ALTER TABLE test_message_sends ADD CONSTRAINT test_message_sends_gateway_authority_check CHECK (
  (gateway_node_id IS NULL AND gateway_node_version IS NULL AND session_lease_version IS NULL
   AND session_configuration_version IS NULL AND authority_expires_at IS NULL AND route_reference IS NULL)
  OR
  (gateway_node_id IS NOT NULL AND gateway_node_version > 0 AND session_lease_version > 0
   AND session_configuration_version > 0 AND authority_expires_at IS NOT NULL
   AND length(trim(route_reference)) BETWEEN 1 AND 200)
);

CREATE OR REPLACE FUNCTION prevent_gateway_session_authority_event_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'gateway session authority events are append-only';
END;
$$;

DROP TRIGGER IF EXISTS trg_gateway_session_authority_events_append_only ON gateway_session_authority_events;
CREATE TRIGGER trg_gateway_session_authority_events_append_only
BEFORE UPDATE OR DELETE ON gateway_session_authority_events
FOR EACH ROW EXECUTE FUNCTION prevent_gateway_session_authority_event_mutation();

COMMIT;
