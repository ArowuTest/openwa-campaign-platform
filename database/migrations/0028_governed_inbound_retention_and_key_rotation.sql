BEGIN;
CREATE TABLE IF NOT EXISTS inbound_retention_policies (
  id text PRIMARY KEY,
  retention_days integer NOT NULL CHECK (retention_days BETWEEN 1 AND 3650),
  status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','RETIRED')),
  effective_from timestamptz NOT NULL,
  effective_to timestamptz,
  version bigint NOT NULL CHECK (version > 0),
  created_by uuid NOT NULL,
  submitted_by uuid,
  approved_by uuid,
  reason text NOT NULL CHECK (length(reason) >= 5),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CHECK (effective_to IS NULL OR effective_to > effective_from),
  CHECK (status <> 'ACTIVE' OR approved_by IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS idx_inbound_retention_policy_active ON inbound_retention_policies(status,effective_from DESC) WHERE status='ACTIVE';
CREATE TABLE IF NOT EXISTS inbound_content_reencryption_runs (
  id uuid PRIMARY KEY,
  requested_by uuid NOT NULL,
  target_key_version text NOT NULL,
  requested_at timestamptz NOT NULL,
  completed_at timestamptz,
  processed_count bigint NOT NULL DEFAULT 0,
  failed_count bigint NOT NULL DEFAULT 0,
  status text NOT NULL CHECK(status IN ('RUNNING','COMPLETED','FAILED')),
  failure_reason text
);
COMMIT;
