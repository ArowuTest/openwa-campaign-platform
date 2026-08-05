BEGIN;

CREATE TABLE consent_grants (
  id uuid PRIMARY KEY,
  contact_id uuid NOT NULL REFERENCES contacts(id),
  organisation_id uuid NOT NULL REFERENCES organisations(id),
  purpose_id text NOT NULL,
  channel text NOT NULL,
  consent_review_id uuid NOT NULL REFERENCES consent_reviews(id),
  wording_version text NOT NULL,
  source_type text NOT NULL,
  source_reference text,
  evidence_object_key text,
  evidence_checksum text NOT NULL,
  effective_from timestamptz NOT NULL,
  granted_at timestamptz NOT NULL,
  expires_at timestamptz,
  status text NOT NULL CHECK (status IN ('PENDING_VERIFICATION','ACTIVE','EXPIRED','WITHDRAWN','REVOKED','SUPERSEDED')),
  created_by uuid NOT NULL,
  client_request_id text NOT NULL,
  request_fingerprint text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  UNIQUE(created_by, client_request_id),
  CHECK (expires_at IS NULL OR expires_at > effective_from)
);

CREATE INDEX consent_grants_eligibility_idx ON consent_grants(contact_id, organisation_id, purpose_id, channel, status, effective_from, expires_at);

CREATE TABLE suppressions (
  id uuid PRIMARY KEY,
  contact_id uuid REFERENCES contacts(id),
  msisdn_lookup_hmac bytea,
  organisation_id uuid REFERENCES organisations(id),
  purpose_id text,
  channel text,
  scope text NOT NULL CHECK (scope IN ('GLOBAL','ORGANISATION','PURPOSE','CHANNEL','TEMPORARY')),
  reason text NOT NULL,
  effective_at timestamptz NOT NULL,
  expires_at timestamptz,
  active boolean NOT NULL DEFAULT true,
  source_reference text,
  created_by uuid NOT NULL,
  client_request_id text NOT NULL,
  request_fingerprint text NOT NULL,
  revoked_by uuid,
  revoked_at timestamptz,
  revoke_reason text,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL,
  UNIQUE(created_by, client_request_id),
  CHECK (contact_id IS NOT NULL OR msisdn_lookup_hmac IS NOT NULL),
  CHECK (expires_at IS NULL OR expires_at > effective_at)
);

CREATE INDEX suppressions_contact_active_idx ON suppressions(contact_id, active, effective_at, expires_at);
CREATE INDEX suppressions_hmac_active_idx ON suppressions(msisdn_lookup_hmac, active, effective_at, expires_at) WHERE msisdn_lookup_hmac IS NOT NULL;

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
CREATE INDEX consent_events_contact_time_idx ON consent_events(contact_id, occurred_at DESC);

CREATE OR REPLACE FUNCTION prevent_consent_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'consent events are append-only';
END $$;
CREATE TRIGGER consent_events_immutable BEFORE UPDATE OR DELETE ON consent_events FOR EACH ROW EXECUTE FUNCTION prevent_consent_event_mutation();

COMMIT;
