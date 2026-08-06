BEGIN;

CREATE TABLE IF NOT EXISTS delivery_exception_resolutions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_recipient_id uuid NOT NULL REFERENCES campaign_recipients(id),
  action text NOT NULL CHECK (action IN ('CONFIRM_SENT','CONFIRM_DELIVERED','CONFIRM_READ','MARK_FAILED_PERMANENT','CONFIRM_NOT_SUBMITTED')),
  evidence_reference text NOT NULL CHECK (length(trim(evidence_reference)) >= 6),
  reason text NOT NULL CHECK (length(trim(reason)) >= 8),
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  resolved_at timestamptz NOT NULL,
  result_status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS delivery_exception_resolution_evidence_uq
  ON delivery_exception_resolutions(campaign_recipient_id, action, evidence_reference);
CREATE INDEX IF NOT EXISTS delivery_exception_resolution_history_idx
  ON delivery_exception_resolutions(campaign_recipient_id, resolved_at DESC);

CREATE OR REPLACE FUNCTION prevent_delivery_exception_resolution_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'delivery exception resolution evidence is append-only';
END $$;
DROP TRIGGER IF EXISTS trg_delivery_exception_resolution_immutable ON delivery_exception_resolutions;
CREATE TRIGGER trg_delivery_exception_resolution_immutable BEFORE UPDATE OR DELETE ON delivery_exception_resolutions FOR EACH ROW EXECUTE FUNCTION prevent_delivery_exception_resolution_mutation();

COMMIT;
