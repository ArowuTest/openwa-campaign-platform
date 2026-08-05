BEGIN;

-- Fencing tokens prevent a stale process from completing or renewing work after
-- its lease has expired and the same logical owner name has reclaimed the row.
ALTER TABLE durable_jobs
  ADD COLUMN IF NOT EXISTS lease_version bigint NOT NULL DEFAULT 0;

ALTER TABLE transactional_outbox
  ADD COLUMN IF NOT EXISTS lease_version bigint NOT NULL DEFAULT 0;

ALTER TABLE delivery_events
  ADD COLUMN IF NOT EXISTS event_fingerprint text;

-- Profile observations have their own timestamp. Age-as-of cannot safely order
-- geography or gender updates because those fields may be supplied independently.
ALTER TABLE contacts
  ADD COLUMN IF NOT EXISTS profile_recorded_at timestamptz;
UPDATE contacts SET profile_recorded_at=updated_at WHERE profile_recorded_at IS NULL;
ALTER TABLE contacts ALTER COLUMN profile_recorded_at SET NOT NULL;

ALTER TABLE audience_import_staging
  ADD COLUMN IF NOT EXISTS profile_recorded_at timestamptz;
UPDATE audience_import_staging SET profile_recorded_at=staged_at WHERE profile_recorded_at IS NULL;
ALTER TABLE audience_import_staging ALTER COLUMN profile_recorded_at SET NOT NULL;

ALTER TABLE contact_profile_history
  ADD COLUMN IF NOT EXISTS profile_recorded_at timestamptz;
UPDATE contact_profile_history SET profile_recorded_at=recorded_at WHERE profile_recorded_at IS NULL;
ALTER TABLE contact_profile_history ALTER COLUMN profile_recorded_at SET NOT NULL;

-- Historic rows predate the canonical application fingerprint. Give each one
-- a unique legacy sentinel rather than pretending a differently-serialised SQL
-- digest is equivalent to the Go fingerprint. A replay against a historic key
-- therefore fails closed as a payload mismatch instead of applying twice.
UPDATE delivery_events
SET event_fingerprint = 'legacy:' || id::text
WHERE event_fingerprint IS NULL;

ALTER TABLE delivery_events
  ALTER COLUMN event_fingerprint SET NOT NULL;

-- The import merge result is stored separately so a replay returns the original
-- committed outcome without re-running the merge or inflating counters.
ALTER TABLE audience_imports
  ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS audience_import_merge_results (
  audience_import_id uuid PRIMARY KEY REFERENCES audience_imports(id) ON DELETE CASCADE,
  inserted_contacts bigint NOT NULL DEFAULT 0 CHECK (inserted_contacts >= 0),
  updated_contacts bigint NOT NULL DEFAULT 0 CHECK (updated_contacts >= 0),
  consent_grants bigint NOT NULL DEFAULT 0 CHECK (consent_grants >= 0),
  source_links bigint NOT NULL DEFAULT 0 CHECK (source_links >= 0),
  profile_history bigint NOT NULL DEFAULT 0 CHECK (profile_history >= 0),
  completed_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_administrative_areas_country_level_name_ci
  ON administrative_areas(country_id, level, lower(name), id)
  WHERE active=true;
CREATE INDEX IF NOT EXISTS idx_administrative_areas_parent_level_name_ci
  ON administrative_areas(parent_id, level, lower(name), id)
  WHERE active=true;
CREATE INDEX IF NOT EXISTS idx_administrative_areas_country_level_code_ci
  ON administrative_areas(country_id, level, upper(code), id)
  WHERE active=true AND code IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_administrative_areas_parent_level_code_ci
  ON administrative_areas(parent_id, level, upper(code), id)
  WHERE active=true AND code IS NOT NULL;

-- Common cohort: active contacts by geography and inclusive reported-age range.
-- The trailing id supports stable keyset pagination when materialising snapshots.
CREATE INDEX IF NOT EXISTS idx_contacts_active_geo_age_id
  ON contacts(country_id, state_id, lga_id, reported_age, id)
  WHERE status='ACTIVE' AND reported_age IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_contacts_active_geo_gender_age_id
  ON contacts(country_id, state_id, lga_id, gender_code, reported_age, id)
  WHERE status='ACTIVE' AND reported_age IS NOT NULL AND gender_code IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_contacts_active_language_id
  ON contacts(preferred_language_code, id)
  WHERE status='ACTIVE' AND preferred_language_code IS NOT NULL;

-- Final eligibility and contact-level consent lookup. The earlier eligibility
-- index remains useful for organisation-wide cohort queries; this index serves
-- the per-recipient final check without scanning an organisation's grants.
CREATE INDEX IF NOT EXISTS idx_consent_grants_contact_basis_active
  ON consent_grants(contact_id, organisation_id, purpose_id, channel, granted_at, expires_at)
  WHERE status='ACTIVE';

CREATE INDEX IF NOT EXISTS idx_suppressions_contact_window_active
  ON suppressions(contact_id, effective_at, expires_at, organisation_id, purpose_id, channel)
  WHERE active=true;

-- Stable message de-duplication within a campaign and fast approved-message lookup.
CREATE UNIQUE INDEX IF NOT EXISTS uq_message_versions_campaign_content_hash
  ON message_versions(campaign_id, content_hash);
CREATE INDEX IF NOT EXISTS idx_message_versions_campaign_status_version
  ON message_versions(campaign_id, status, version DESC);

-- Campaign release, dashboards, sender operations and reconciliation.
CREATE INDEX IF NOT EXISTS idx_campaigns_status_schedule
  ON campaigns(status, requested_start_at, completion_deadline_at, id)
  WHERE status IN ('SCHEDULED','DISPATCHING','PAUSED');
DROP INDEX IF EXISTS idx_campaign_recipients_dispatch;
DROP INDEX IF EXISTS idx_campaign_recipients_campaign_status;
CREATE INDEX IF NOT EXISTS idx_campaign_recipients_campaign_status_updated
  ON campaign_recipients(campaign_id, status, updated_at DESC, id);
CREATE INDEX IF NOT EXISTS idx_campaign_recipients_session_status
  ON campaign_recipients(assigned_session_id, status, updated_at, id)
  WHERE assigned_session_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_campaign_recipients_retry_due
  ON campaign_recipients(next_attempt_at, campaign_id, id)
  WHERE status='FAILED_RETRYABLE' AND next_attempt_at IS NOT NULL;

-- Provider event correlation and reporting.
CREATE INDEX IF NOT EXISTS idx_delivery_events_provider_message_time
  ON delivery_events(provider_event_id, occurred_at DESC)
  WHERE provider_event_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_delivery_events_received_time
  ON delivery_events(received_at, id);

-- Split pending and expired-lease paths. A combined OR index is substantially
-- less selective for large queues and does not match both sort orders well.
DROP INDEX IF EXISTS idx_durable_jobs_claim;
CREATE INDEX IF NOT EXISTS idx_durable_jobs_pending_claim
  ON durable_jobs(priority DESC, available_at, created_at, id)
  WHERE status='PENDING';
CREATE INDEX IF NOT EXISTS idx_durable_jobs_expired_lease_claim
  ON durable_jobs(lease_expires_at, priority DESC, created_at, id)
  WHERE status='PROCESSING';
CREATE INDEX IF NOT EXISTS idx_durable_jobs_owner_processing
  ON durable_jobs(lease_owner, lease_version, lease_expires_at, id)
  WHERE status='PROCESSING';

DROP INDEX IF EXISTS idx_transactional_outbox_pending;
DROP INDEX IF EXISTS idx_transactional_outbox_claim;
DROP INDEX IF EXISTS idx_transactional_outbox_reclaim;
CREATE INDEX IF NOT EXISTS idx_transactional_outbox_pending_claim
  ON transactional_outbox(available_at, created_at, id)
  WHERE status='PENDING';
CREATE INDEX IF NOT EXISTS idx_transactional_outbox_expired_lease_claim
  ON transactional_outbox(lease_expires_at, created_at, id)
  WHERE status='PROCESSING';
CREATE INDEX IF NOT EXISTS idx_transactional_outbox_owner_processing
  ON transactional_outbox(lease_owner, lease_version, lease_expires_at, id)
  WHERE status='PROCESSING';

-- Sender selection. The primary keys cover joins; these partial indexes narrow
-- candidate pools before the lease and node-health checks.
CREATE INDEX IF NOT EXISTS idx_sender_sessions_allocatable_pool
  ON sender_sessions(logical_sender_pool, id)
  WHERE status IN ('READY','BUSY');
CREATE INDEX IF NOT EXISTS idx_sender_nodes_ready
  ON sender_nodes(id)
  WHERE status='READY' AND draining=false;

-- Import validation and merge paths.
DROP INDEX IF EXISTS idx_audience_import_staging_lookup;
-- The staging primary key already provides the import_id scan used by merge and
-- validation. A second wide geography index would add substantial write cost to
-- multi-million-row imports without serving a selective lookup path.
DROP INDEX IF EXISTS idx_audience_import_staging_country_state_lga;
CREATE INDEX IF NOT EXISTS idx_audience_import_issues_import_code
  ON audience_import_issues(audience_import_id, issue_code, row_number);
CREATE INDEX IF NOT EXISTS idx_audience_imports_merge_state
  ON audience_imports(status, approved_at, created_at, id)
  WHERE status IN ('APPROVED','IMPORTING');

-- Remove indexes that duplicate a primary-key index exactly.
DROP INDEX IF EXISTS idx_audience_snapshot_members_ordered;

COMMIT;
