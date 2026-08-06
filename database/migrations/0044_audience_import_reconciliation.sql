BEGIN;

CREATE TABLE IF NOT EXISTS audience_import_reconciliations (
  id uuid PRIMARY KEY,
  audience_import_id uuid NOT NULL UNIQUE REFERENCES audience_imports(id) ON DELETE RESTRICT,
  evidence jsonb NOT NULL,
  evidence_hash text NOT NULL CHECK (evidence_hash ~ '^[a-f0-9]{64}$'),
  reason text NOT NULL CHECK (length(btrim(reason)) >= 8),
  performed_by uuid NOT NULL REFERENCES internal_users(id) ON DELETE RESTRICT,
  created_at timestamptz NOT NULL,
  CHECK (jsonb_typeof(evidence) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_audience_import_reconciliations_created
  ON audience_import_reconciliations(created_at DESC,id);

CREATE OR REPLACE FUNCTION prevent_audience_import_reconciliation_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audience import reconciliation evidence is append-only';
END;
$$;

DROP TRIGGER IF EXISTS trg_prevent_audience_import_reconciliation_update ON audience_import_reconciliations;
CREATE TRIGGER trg_prevent_audience_import_reconciliation_update
BEFORE UPDATE OR DELETE ON audience_import_reconciliations
FOR EACH ROW EXECUTE FUNCTION prevent_audience_import_reconciliation_mutation();

COMMIT;
