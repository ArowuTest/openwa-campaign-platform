BEGIN;

-- The publisher claims outbox records before publishing them. Earlier schema versions
-- did not include PROCESSING in the status constraint, which made a real claim fail.
ALTER TABLE transactional_outbox
  DROP CONSTRAINT IF EXISTS transactional_outbox_status_check;

ALTER TABLE transactional_outbox
  ADD CONSTRAINT transactional_outbox_status_check
  CHECK (status IN ('PENDING','PROCESSING','PUBLISHED','FAILED'));

ALTER TABLE transactional_outbox
  ADD COLUMN IF NOT EXISTS max_attempts integer NOT NULL DEFAULT 20,
  ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE transactional_outbox
  DROP CONSTRAINT IF EXISTS transactional_outbox_max_attempts_check;
ALTER TABLE transactional_outbox
  ADD CONSTRAINT transactional_outbox_max_attempts_check
  CHECK (max_attempts > 0 AND max_attempts <= 100);

CREATE INDEX IF NOT EXISTS idx_transactional_outbox_reclaim
  ON transactional_outbox(status, lease_expires_at, available_at)
  WHERE status IN ('PENDING','PROCESSING');

COMMIT;
