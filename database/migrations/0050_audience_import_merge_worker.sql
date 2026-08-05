BEGIN;
ALTER TABLE audience_imports
  ADD COLUMN IF NOT EXISTS merge_lease_owner text,
  ADD COLUMN IF NOT EXISTS merge_lease_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS merge_lease_version bigint NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS merge_attempt_count integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS merge_next_attempt_at timestamptz,
  ADD COLUMN IF NOT EXISTS merge_last_error text;
ALTER TABLE audience_imports DROP CONSTRAINT IF EXISTS audience_imports_merge_lease_check;
ALTER TABLE audience_imports ADD CONSTRAINT audience_imports_merge_lease_check CHECK (
 (merge_lease_owner IS NULL AND merge_lease_expires_at IS NULL) OR
 (merge_lease_owner IS NOT NULL AND merge_lease_expires_at IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS idx_audience_imports_merge_queue ON audience_imports(coalesce(merge_next_attempt_at,approved_at,created_at),id)
 WHERE status IN('APPROVED','IMPORTING');
COMMIT;
