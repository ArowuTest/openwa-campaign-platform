BEGIN;

-- The original audience foundation created consent_grants and suppressions in
-- migration 0002. This migration upgrades those tables in place to the durable,
-- idempotent individual ledger model. Recreating the tables here would make a
-- clean 0001..N migration chain fail before any application could start.

ALTER TABLE consent_grants DROP CONSTRAINT IF EXISTS consent_grants_purpose_id_fkey;
ALTER TABLE consent_grants ALTER COLUMN purpose_id TYPE text USING purpose_id::text;
ALTER TABLE consent_grants ALTER COLUMN organisation_id SET NOT NULL;
ALTER TABLE consent_grants
  ADD COLUMN IF NOT EXISTS consent_review_id uuid REFERENCES consent_reviews(id),
  ADD COLUMN IF NOT EXISTS effective_from timestamptz,
  ADD COLUMN IF NOT EXISTS created_by uuid,
  ADD COLUMN IF NOT EXISTS client_request_id text,
  ADD COLUMN IF NOT EXISTS request_fingerprint text,
  ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;

UPDATE consent_grants
SET effective_from=coalesce(effective_from,granted_at),
    created_by=coalesce(created_by,reviewed_by,'00000000-0000-0000-0000-000000000000'::uuid),
    client_request_id=coalesce(client_request_id,'legacy:'||id::text),
    request_fingerprint=coalesce(request_fingerprint,'legacy:'||id::text),
    evidence_checksum=coalesce(evidence_checksum,'legacy:'||id::text),
    updated_at=coalesce(updated_at,created_at)
WHERE effective_from IS NULL OR created_by IS NULL OR client_request_id IS NULL
   OR request_fingerprint IS NULL OR evidence_checksum IS NULL;

ALTER TABLE consent_grants ALTER COLUMN effective_from SET NOT NULL;
ALTER TABLE consent_grants ALTER COLUMN created_by SET NOT NULL;
ALTER TABLE consent_grants ALTER COLUMN client_request_id SET NOT NULL;
ALTER TABLE consent_grants ALTER COLUMN request_fingerprint SET NOT NULL;
ALTER TABLE consent_grants ALTER COLUMN evidence_checksum SET NOT NULL;
ALTER TABLE consent_grants DROP CONSTRAINT IF EXISTS consent_grants_status_check;
ALTER TABLE consent_grants ADD CONSTRAINT consent_grants_status_check CHECK (
  status IN ('PENDING_VERIFICATION','ACTIVE','EXPIRED','WITHDRAWN','REVOKED','SUPERSEDED','DISPUTED')
);
ALTER TABLE consent_grants DROP CONSTRAINT IF EXISTS consent_grants_expires_after_effective;
ALTER TABLE consent_grants ADD CONSTRAINT consent_grants_expires_after_effective CHECK (expires_at IS NULL OR expires_at > effective_from);
CREATE UNIQUE INDEX IF NOT EXISTS consent_grants_request_idempotency_idx ON consent_grants(created_by,client_request_id);
CREATE INDEX IF NOT EXISTS consent_grants_eligibility_idx ON consent_grants(contact_id,organisation_id,purpose_id,channel,status,effective_from,expires_at);

ALTER TABLE suppressions DROP CONSTRAINT IF EXISTS suppressions_purpose_id_fkey;
ALTER TABLE suppressions ALTER COLUMN purpose_id TYPE text USING purpose_id::text;
ALTER TABLE suppressions ALTER COLUMN contact_id DROP NOT NULL;
ALTER TABLE suppressions
  ADD COLUMN IF NOT EXISTS msisdn_lookup_hmac bytea,
  ADD COLUMN IF NOT EXISTS created_by uuid,
  ADD COLUMN IF NOT EXISTS client_request_id text,
  ADD COLUMN IF NOT EXISTS request_fingerprint text,
  ADD COLUMN IF NOT EXISTS revoked_by uuid,
  ADD COLUMN IF NOT EXISTS revoked_at timestamptz,
  ADD COLUMN IF NOT EXISTS revoke_reason text,
  ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;

UPDATE suppressions
SET created_by=coalesce(created_by,'00000000-0000-0000-0000-000000000000'::uuid),
    client_request_id=coalesce(client_request_id,'legacy:'||id::text),
    request_fingerprint=coalesce(request_fingerprint,'legacy:'||id::text)
WHERE created_by IS NULL OR client_request_id IS NULL OR request_fingerprint IS NULL;

ALTER TABLE suppressions ALTER COLUMN created_by SET NOT NULL;
ALTER TABLE suppressions ALTER COLUMN client_request_id SET NOT NULL;
ALTER TABLE suppressions ALTER COLUMN request_fingerprint SET NOT NULL;
ALTER TABLE suppressions DROP CONSTRAINT IF EXISTS suppressions_subject_required;
ALTER TABLE suppressions ADD CONSTRAINT suppressions_subject_required CHECK (contact_id IS NOT NULL OR msisdn_lookup_hmac IS NOT NULL);
ALTER TABLE suppressions DROP CONSTRAINT IF EXISTS suppressions_expires_after_effective;
ALTER TABLE suppressions ADD CONSTRAINT suppressions_expires_after_effective CHECK (expires_at IS NULL OR expires_at > effective_at);
CREATE UNIQUE INDEX IF NOT EXISTS suppressions_request_idempotency_idx ON suppressions(created_by,client_request_id);
CREATE INDEX IF NOT EXISTS suppressions_contact_active_idx ON suppressions(contact_id,active,effective_at,expires_at);
CREATE INDEX IF NOT EXISTS suppressions_hmac_active_idx ON suppressions(msisdn_lookup_hmac,active,effective_at,expires_at) WHERE msisdn_lookup_hmac IS NOT NULL;

CREATE TABLE consent_events (
  id uuid PRIMARY KEY,
  contact_id uuid,
  grant_id uuid REFERENCES consent_grants(id),
  suppression_id uuid REFERENCES suppressions(id),
  event_type text NOT NULL,
  actor_id uuid,
  reason text,
  source_reference text,
  occurred_at timestamptz NOT NULL
);
CREATE INDEX consent_events_contact_time_idx ON consent_events(contact_id,occurred_at DESC);

CREATE OR REPLACE FUNCTION prevent_consent_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'consent events are append-only';
END $$;
CREATE TRIGGER consent_events_immutable BEFORE UPDATE OR DELETE ON consent_events FOR EACH ROW EXECUTE FUNCTION prevent_consent_event_mutation();

COMMIT;
