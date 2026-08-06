BEGIN;

ALTER TABLE audit_events
  ADD COLUMN IF NOT EXISTS outcome text,
  ADD COLUMN IF NOT EXISTS sensitivity text;

ALTER TABLE export_requests
  ADD COLUMN IF NOT EXISTS criteria jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS template_version text NOT NULL DEFAULT 'EXPORT-V1',
  ADD COLUMN IF NOT EXISTS as_of timestamptz,
  ADD COLUMN IF NOT EXISTS frozen_payload jsonb,
  ADD COLUMN IF NOT EXISTS audit_head_sequence bigint,
  ADD COLUMN IF NOT EXISTS audit_head_hash text,
  ADD COLUMN IF NOT EXISTS watermark_text text,
  ADD COLUMN IF NOT EXISTS download_count bigint NOT NULL DEFAULT 0 CHECK (download_count >= 0),
  ADD COLUMN IF NOT EXISTS last_downloaded_at timestamptz,
  ADD COLUMN IF NOT EXISTS revoked_at timestamptz,
  ADD COLUMN IF NOT EXISTS revoked_by uuid REFERENCES internal_users(id),
  ADD COLUMN IF NOT EXISTS revocation_reason text,
  ADD COLUMN IF NOT EXISTS attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  ADD COLUMN IF NOT EXISTS lease_owner text,
  ADD COLUMN IF NOT EXISTS lease_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS object_key text,
  ADD COLUMN IF NOT EXISTS content_type text,
  ADD COLUMN IF NOT EXISTS sha256 text,
  ADD COLUMN IF NOT EXISTS size_bytes bigint,
  ADD COLUMN IF NOT EXISTS failure_code text,
  ADD COLUMN IF NOT EXISTS failure_detail text,
  ADD COLUMN IF NOT EXISTS generated_at timestamptz;

UPDATE export_requests
SET object_key = coalesce(object_key, storage_object_key),
    sha256 = coalesce(sha256, content_hash)
WHERE object_key IS NULL OR sha256 IS NULL;

ALTER TABLE export_requests DROP CONSTRAINT IF EXISTS export_requests_kind_check;
ALTER TABLE export_requests ADD CONSTRAINT export_requests_kind_check CHECK (kind IN ('CAMPAIGN_REPORT','AUDIT_LOG','PRIVACY_PACKAGE'));
ALTER TABLE export_requests DROP CONSTRAINT IF EXISTS export_requests_status_check;
ALTER TABLE export_requests ADD CONSTRAINT export_requests_status_check CHECK (
  status IN ('DRAFT','PENDING_APPROVAL','APPROVED','PROCESSING','REJECTED','READY','EXPIRING','EXPIRED','FAILED','REVOKED')
);
ALTER TABLE export_requests ADD CONSTRAINT export_requests_revoke_check CHECK (
  status <> 'REVOKED' OR (revoked_at IS NOT NULL AND revoked_by IS NOT NULL AND length(coalesce(revocation_reason,'')) >= 8)
);
ALTER TABLE export_requests ADD CONSTRAINT export_requests_ready_evidence_check CHECK (
  status <> 'READY' OR (object_key IS NOT NULL AND content_type IS NOT NULL AND sha256 ~ '^[0-9a-f]{64}$' AND size_bytes > 0 AND generated_at IS NOT NULL AND expires_at IS NOT NULL)
);

CREATE TABLE export_download_grants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  export_id uuid NOT NULL REFERENCES export_requests(id) ON DELETE CASCADE,
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  token_hash text NOT NULL UNIQUE CHECK (token_hash ~ '^[0-9a-f]{64}$'),
  request_id text NOT NULL,
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (expires_at > created_at),
  CHECK (used_at IS NULL OR used_at >= created_at),
  CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);
CREATE INDEX export_download_grants_export_actor_idx ON export_download_grants(export_id,actor_id,created_at DESC);
CREATE INDEX export_download_grants_active_idx ON export_download_grants(token_hash,expires_at) WHERE used_at IS NULL AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS export_requests_requested_page_idx ON export_requests(requested_by,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS export_requests_status_page_idx ON export_requests(status,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS export_requests_object_idx ON export_requests(kind,object_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS audit_events_organisation_time_idx ON audit_events(organisation_id,created_at DESC,sequence DESC) WHERE organisation_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS audit_events_sensitivity_time_idx ON audit_events(sensitivity,created_at DESC,sequence DESC) WHERE sensitivity IS NOT NULL;
CREATE INDEX IF NOT EXISTS audit_events_outcome_time_idx ON audit_events(outcome,created_at DESC,sequence DESC) WHERE outcome IS NOT NULL;
CREATE INDEX IF NOT EXISTS audit_events_actor_time_idx ON audit_events(actor_id,created_at DESC,sequence DESC) WHERE actor_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS audit_events_action_time_idx ON audit_events(action,created_at DESC,sequence DESC);
CREATE INDEX IF NOT EXISTS audit_events_entity_time_idx ON audit_events(entity_type,entity_id,created_at DESC,sequence DESC);
CREATE INDEX IF NOT EXISTS audit_events_correlation_idx ON audit_events(correlation_id,sequence DESC) WHERE correlation_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS audit_events_source_ip_idx ON audit_events(source_ip,created_at DESC,sequence DESC) WHERE source_ip IS NOT NULL;

COMMIT;
