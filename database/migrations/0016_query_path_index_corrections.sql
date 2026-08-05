BEGIN;

-- Lagos 18-35 and equivalent state-wide age cohorts do not constrain LGA.
-- In a B-tree ordered (country,state,lga,age), the unconstrained LGA column
-- prevents an efficient age range. Keep the LGA-specific index and add the
-- state-wide path used by the mandatory SRS cohort.
CREATE INDEX IF NOT EXISTS idx_contacts_active_country_state_age_id
  ON contacts(country_id,state_id,reported_age,id)
  WHERE status='ACTIVE' AND reported_age IS NOT NULL;

-- Gender can be combined by PostgreSQL with the existing selective gender index.
-- Removing this very wide index reduces write amplification on 10M-profile imports.
DROP INDEX IF EXISTS idx_contacts_active_geo_gender_age_id;

-- Per-recipient final consent checks constrain contact, controller, purpose and
-- channel by equality, then evaluate expiry. granted_at is evidence returned to
-- the policy evaluator, not the leading range key.
DROP INDEX IF EXISTS idx_consent_grants_contact_basis_active;
CREATE INDEX IF NOT EXISTS idx_consent_grants_contact_basis_active
  ON consent_grants(contact_id,organisation_id,purpose_id,channel,expires_at)
  INCLUDE (granted_at,wording_version)
  WHERE status='ACTIVE';

-- Workers normally claim one or a small set of job types. Retain the untyped
-- claim indexes for administrative/all-type consumers and add type-first paths
-- so a worker does not scan unrelated obligations before applying LIMIT.
CREATE INDEX IF NOT EXISTS idx_durable_jobs_type_pending_claim
  ON durable_jobs(job_type,priority DESC,available_at,created_at,id)
  WHERE status='PENDING';
CREATE INDEX IF NOT EXISTS idx_durable_jobs_type_expired_lease_claim
  ON durable_jobs(job_type,lease_expires_at,priority DESC,created_at,id)
  WHERE status='PROCESSING';

-- event_deduplication_key remains the internal immutable command identity.
-- provider_event_id is independent external evidence and must also identify one
-- callback globally; the former composite constraint allowed one provider event
-- identifier to be reused under a different event type.
ALTER TABLE delivery_events
  DROP CONSTRAINT IF EXISTS delivery_events_provider_event_id_event_type_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_delivery_events_provider_event_id
  ON delivery_events(provider_event_id)
  WHERE provider_event_id IS NOT NULL;

COMMIT;
