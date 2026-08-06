BEGIN;

CREATE TABLE trusted_assets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  purpose text NOT NULL CHECK (purpose IN ('MESSAGE_MEDIA','CONSENT_EVIDENCE','CAMPAIGN_EVIDENCE')),
  object_key text NOT NULL UNIQUE,
  original_filename text NOT NULL,
  media_type text NOT NULL,
  byte_size bigint NOT NULL CHECK (byte_size > 0),
  sha256_checksum text NOT NULL CHECK (sha256_checksum ~ '^[0-9a-f]{64}$'),
  status text NOT NULL CHECK (status IN ('SCANNING','CLEAN','INFECTED','SCAN_FAILED','REVOKED')),
  scan_signature text,
  created_by uuid NOT NULL REFERENCES internal_users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  UNIQUE (purpose, sha256_checksum, byte_size)
);
CREATE INDEX trusted_assets_purpose_status_created_idx ON trusted_assets(purpose,status,created_at DESC,id DESC);

CREATE TABLE trusted_asset_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  asset_id uuid NOT NULL REFERENCES trusted_assets(id),
  event_type text NOT NULL CHECK (event_type IN ('UPLOADED','SCAN_CLEAN','SCAN_INFECTED','SCAN_FAILED','REVOKED','ACCESSED')),
  actor_id uuid REFERENCES internal_users(id),
  reason text,
  asset_version bigint NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX trusted_asset_events_asset_time_idx ON trusted_asset_events(asset_id,occurred_at DESC,id DESC);

CREATE FUNCTION prevent_trusted_asset_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'trusted asset events are append-only'; END;
$$;
CREATE TRIGGER trusted_asset_events_append_only BEFORE UPDATE OR DELETE ON trusted_asset_events FOR EACH ROW EXECUTE FUNCTION prevent_trusted_asset_event_mutation();

ALTER TABLE message_versions ADD COLUMN IF NOT EXISTS media_asset_id uuid REFERENCES trusted_assets(id);
ALTER TABLE message_versions DROP CONSTRAINT IF EXISTS message_versions_trusted_media_check;
ALTER TABLE message_versions ADD CONSTRAINT message_versions_trusted_media_check CHECK (
  (message_type='TEXT' AND media_asset_id IS NULL)
  OR
  (message_type<>'TEXT' AND media_asset_id IS NOT NULL AND media_object_key IS NOT NULL AND media_sha256 IS NOT NULL AND media_scan_status='CLEAN')
);

ALTER TABLE consent_reviews
  ADD COLUMN IF NOT EXISTS created_by uuid REFERENCES internal_users(id),
  ADD COLUMN IF NOT EXISTS review_scope text NOT NULL DEFAULT 'ORGANISATION' CHECK (review_scope IN ('ORGANISATION','SOURCE','CAMPAIGN')),
  ADD COLUMN IF NOT EXISTS campaign_id uuid REFERENCES campaigns(id),
  ADD COLUMN IF NOT EXISTS source_system text,
  ADD COLUMN IF NOT EXISTS collection_method text,
  ADD COLUMN IF NOT EXISTS collection_period_from date,
  ADD COLUMN IF NOT EXISTS collection_period_to date,
  ADD COLUMN IF NOT EXISTS controller_role text,
  ADD COLUMN IF NOT EXISTS permitted_purpose_code text,
  ADD COLUMN IF NOT EXISTS permitted_partner_organisations jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS exact_consent_wording text,
  ADD COLUMN IF NOT EXISTS privacy_notice_version text,
  ADD COLUMN IF NOT EXISTS external_evidence_references jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS sample_review_notes text,
  ADD COLUMN IF NOT EXISTS outcome text NOT NULL DEFAULT 'PENDING' CHECK (outcome IN ('PENDING','APPROVED','APPROVED_WITH_RESTRICTIONS','REJECTED')),
  ADD COLUMN IF NOT EXISTS parent_review_id uuid REFERENCES consent_reviews(id),
  ADD COLUMN IF NOT EXISTS superseded_by_id uuid REFERENCES consent_reviews(id),
  ADD COLUMN IF NOT EXISTS revoked_at timestamptz,
  ADD COLUMN IF NOT EXISTS revoked_by uuid REFERENCES internal_users(id),
  ADD COLUMN IF NOT EXISTS revoke_reason text,
  ADD COLUMN IF NOT EXISTS next_review_at timestamptz;


ALTER TABLE consent_reviews DROP CONSTRAINT IF EXISTS consent_reviews_status_check;
ALTER TABLE consent_reviews ADD CONSTRAINT consent_reviews_status_check CHECK (
  status IN ('DRAFT','PENDING_REVIEW','APPROVED','REJECTED','EXPIRED','SUSPENDED','REVOKED','SUPERSEDED')
);

ALTER TABLE consent_reviews DROP CONSTRAINT IF EXISTS consent_reviews_sample_review_notes_check;
ALTER TABLE consent_reviews ADD CONSTRAINT consent_reviews_sample_review_notes_check CHECK (sample_review_notes IS NULL OR length(sample_review_notes)<=2000);
ALTER TABLE consent_reviews DROP CONSTRAINT IF EXISTS consent_reviews_scope_check;
ALTER TABLE consent_reviews ADD CONSTRAINT consent_reviews_scope_check CHECK (
  (review_scope='ORGANISATION' AND campaign_id IS NULL)
  OR (review_scope='SOURCE' AND source_system IS NOT NULL AND campaign_id IS NULL)
  OR (review_scope='CAMPAIGN' AND campaign_id IS NOT NULL)
);
ALTER TABLE consent_reviews ADD CONSTRAINT consent_reviews_collection_period_check CHECK (collection_period_to IS NULL OR collection_period_from IS NULL OR collection_period_to>=collection_period_from);
ALTER TABLE consent_review_evidence ADD COLUMN IF NOT EXISTS trusted_asset_id uuid REFERENCES trusted_assets(id);
CREATE INDEX consent_reviews_scope_resolution_idx ON consent_reviews(organisation_id,review_scope,source_system,campaign_id,status,expires_at);

CREATE TABLE consent_review_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  consent_review_id uuid NOT NULL REFERENCES consent_reviews(id),
  event_type text NOT NULL CHECK (event_type IN ('CREATED','SUBMITTED','APPROVED','APPROVED_WITH_RESTRICTIONS','REJECTED','REVOKED','SUPERSEDED','EXPIRED')),
  actor_id uuid REFERENCES internal_users(id),
  reason text,
  review_version bigint NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX consent_review_events_review_time_idx ON consent_review_events(consent_review_id,occurred_at DESC,id DESC);
CREATE FUNCTION prevent_consent_review_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'consent review events are append-only'; END;
$$;
CREATE TRIGGER consent_review_events_append_only BEFORE UPDATE OR DELETE ON consent_review_events FOR EACH ROW EXECUTE FUNCTION prevent_consent_review_event_mutation();

COMMIT;
