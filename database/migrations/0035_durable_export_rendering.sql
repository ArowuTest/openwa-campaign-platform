BEGIN;
ALTER TABLE export_requests
  ADD COLUMN IF NOT EXISTS object_key text,
  ADD COLUMN IF NOT EXISTS content_type text,
  ADD COLUMN IF NOT EXISTS sha256 text,
  ADD COLUMN IF NOT EXISTS size_bytes bigint NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS failure_code text,
  ADD COLUMN IF NOT EXISTS failure_detail text,
  ADD COLUMN IF NOT EXISTS generated_at timestamptz,
  ADD COLUMN IF NOT EXISTS lease_owner text,
  ADD COLUMN IF NOT EXISTS lease_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS attempt_count integer NOT NULL DEFAULT 0;

ALTER TABLE export_requests DROP CONSTRAINT IF EXISTS export_requests_status_check;
ALTER TABLE export_requests ADD CONSTRAINT export_requests_status_check CHECK (status IN ('DRAFT','PENDING_APPROVAL','APPROVED','PROCESSING','REJECTED','READY','EXPIRED','FAILED'));
CREATE INDEX IF NOT EXISTS export_requests_render_claim_idx ON export_requests(status,created_at) WHERE status IN ('APPROVED','PROCESSING');
CREATE INDEX IF NOT EXISTS export_requests_expiry_idx ON export_requests(expires_at) WHERE status='READY';

COMMIT;
