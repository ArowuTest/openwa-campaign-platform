BEGIN;

-- Supports bounded SKIP LOCKED claims by the audience validation workers.
CREATE INDEX IF NOT EXISTS idx_audience_imports_validation_claim
  ON audience_imports(created_at,id)
  WHERE status='VALIDATING'
    AND malware_scan_status='CLEAN'
    AND content_signature_valid=true;

-- Supports approval-to-merge polling without indexing high-churn counters.
CREATE INDEX IF NOT EXISTS idx_audience_imports_merge_claim
  ON audience_imports(approved_at,id)
  WHERE status='APPROVED';

COMMIT;
