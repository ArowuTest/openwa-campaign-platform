BEGIN;

-- Transactional delivery updates maintain dashboard counters. This separate
-- ledger check detects drift without overwriting counters while provider events
-- are concurrently arriving.
CREATE TABLE campaign_metric_reconciliations (
  campaign_id uuid PRIMARY KEY REFERENCES campaigns(id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'IDLE'
    CHECK (status IN ('IDLE','PROCESSING','MATCH','DRIFT','FAILED')),
  next_run_at timestamptz NOT NULL DEFAULT now(),
  lease_owner text,
  lease_expires_at timestamptz,
  lease_version bigint NOT NULL DEFAULT 0,
  consecutive_drift_count integer NOT NULL DEFAULT 0 CHECK (consecutive_drift_count >= 0),
  last_checked_at timestamptz,
  last_canonical jsonb,
  last_stored jsonb,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((status='PROCESSING') = (lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL))
);

CREATE INDEX idx_campaign_metric_reconciliations_claim
  ON campaign_metric_reconciliations(next_run_at,campaign_id)
  WHERE status<>'PROCESSING';
CREATE INDEX idx_campaign_metric_reconciliations_reclaim
  ON campaign_metric_reconciliations(lease_expires_at,campaign_id)
  WHERE status='PROCESSING';
CREATE INDEX idx_campaign_metric_reconciliations_drift
  ON campaign_metric_reconciliations(consecutive_drift_count DESC,last_checked_at,campaign_id)
  WHERE status='DRIFT';

COMMIT;
