BEGIN;

-- Every message draft mutation is replay-safe. Historic rows receive a unique
-- fail-closed key and all new application writes must provide a client key.
ALTER TABLE message_versions
  ADD COLUMN IF NOT EXISTS client_request_id text;

UPDATE message_versions
SET client_request_id = 'legacy:' || id::text
WHERE client_request_id IS NULL;

ALTER TABLE message_versions
  ALTER COLUMN client_request_id SET NOT NULL;

ALTER TABLE message_versions
  DROP CONSTRAINT IF EXISTS message_versions_client_request_id_check;
ALTER TABLE message_versions
  ADD CONSTRAINT message_versions_client_request_id_check
  CHECK (length(client_request_id) BETWEEN 16 AND 128 AND client_request_id ~ '^[A-Za-z0-9_.:-]+$');

CREATE UNIQUE INDEX IF NOT EXISTS uq_message_versions_campaign_request
  ON message_versions(campaign_id, client_request_id);

-- Audit evidence is append-only even if a future role is accidentally granted
-- UPDATE or DELETE. Privilege restrictions remain the first control; this
-- trigger is defence in depth.
CREATE OR REPLACE FUNCTION prevent_audit_event_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit events are append-only';
END;
$$;

DROP TRIGGER IF EXISTS trg_audit_events_append_only ON audit_events;
CREATE TRIGGER trg_audit_events_append_only
BEFORE UPDATE OR DELETE ON audit_events
FOR EACH ROW EXECUTE FUNCTION prevent_audit_event_mutation();

COMMIT;
