BEGIN;
ALTER TABLE inbound_content_reencryption_runs DROP CONSTRAINT IF EXISTS inbound_content_reencryption_runs_status_check;
ALTER TABLE inbound_content_reencryption_runs
  ADD COLUMN IF NOT EXISTS started_at timestamptz,
  ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS next_run_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS last_processed_id uuid,
  ADD COLUMN IF NOT EXISTS lease_owner text,
  ADD COLUMN IF NOT EXISTS lease_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS lease_version bigint NOT NULL DEFAULT 0;
ALTER TABLE inbound_content_reencryption_runs
  ADD CONSTRAINT inbound_content_reencryption_runs_status_check CHECK(status IN ('PENDING','RUNNING','COMPLETED','FAILED'));
CREATE INDEX IF NOT EXISTS idx_inbound_reencryption_run_claim
  ON inbound_content_reencryption_runs(next_run_at,requested_at)
  WHERE status IN ('PENDING','RUNNING','FAILED');
CREATE INDEX IF NOT EXISTS idx_inbound_reencryption_run_lease
  ON inbound_content_reencryption_runs(lease_expires_at)
  WHERE lease_owner IS NOT NULL;

CREATE TABLE IF NOT EXISTS inbound_retention_sweep_runs (
  id uuid PRIMARY KEY,
  worker_id text NOT NULL,
  started_at timestamptz NOT NULL,
  completed_at timestamptz,
  redacted_count bigint NOT NULL DEFAULT 0,
  status text NOT NULL CHECK(status IN ('RUNNING','COMPLETED','FAILED')),
  failure_reason text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_inbound_retention_sweep_started ON inbound_retention_sweep_runs(started_at DESC);
COMMIT;
