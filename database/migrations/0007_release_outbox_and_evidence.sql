BEGIN;

ALTER TABLE campaign_recipients
  ADD COLUMN IF NOT EXISTS eligibility_evidence_hash text;

ALTER TABLE transactional_outbox
  ADD COLUMN IF NOT EXISTS deduplication_key text,
  ADD COLUMN IF NOT EXISTS lease_owner text,
  ADD COLUMN IF NOT EXISTS lease_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS last_error_code text,
  ADD COLUMN IF NOT EXISTS last_error_detail text;

UPDATE transactional_outbox
SET deduplication_key = event_type || ':' || aggregate_id::text
WHERE deduplication_key IS NULL;

ALTER TABLE transactional_outbox
  ALTER COLUMN deduplication_key SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_transactional_outbox_deduplication_key
  ON transactional_outbox(deduplication_key);

CREATE INDEX IF NOT EXISTS idx_transactional_outbox_claim
  ON transactional_outbox(available_at, created_at)
  WHERE status = 'PENDING';

ALTER TABLE message_versions
  ADD COLUMN IF NOT EXISTS created_by uuid REFERENCES internal_users(id),
  ADD COLUMN IF NOT EXISTS media_sha256 text,
  ADD COLUMN IF NOT EXISTS media_type text,
  ADD COLUMN IF NOT EXISTS media_size bigint CHECK (media_size IS NULL OR media_size >= 0),
  ADD COLUMN IF NOT EXISTS media_scan_status text,
  ADD COLUMN IF NOT EXISTS variables jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE audience_snapshots
  ADD COLUMN IF NOT EXISTS definition_version bigint NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS configuration_version text NOT NULL DEFAULT 'bootstrap-v1';

CREATE INDEX IF NOT EXISTS idx_audience_snapshot_members_ordered
  ON audience_snapshot_members(snapshot_id, contact_id);

COMMIT;
