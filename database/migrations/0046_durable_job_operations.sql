BEGIN;

CREATE TABLE IF NOT EXISTS durable_job_administration_events (
  id bigserial PRIMARY KEY,
  job_id uuid NOT NULL REFERENCES durable_jobs(id) ON DELETE RESTRICT,
  action text NOT NULL CHECK (action IN ('RETRY_DEAD_LETTER','CANCEL_PENDING')),
  actor_id uuid NOT NULL REFERENCES internal_users(id) ON DELETE RESTRICT,
  reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 8 AND 1000),
  previous_status text NOT NULL,
  current_status text NOT NULL,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_durable_job_admin_events_job
  ON durable_job_administration_events(job_id, occurred_at DESC, id DESC);

CREATE OR REPLACE FUNCTION prevent_durable_job_admin_event_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'durable job administration events are append-only';
END;
$$;
DROP TRIGGER IF EXISTS trg_durable_job_admin_events_append_only ON durable_job_administration_events;
CREATE TRIGGER trg_durable_job_admin_events_append_only
BEFORE UPDATE OR DELETE ON durable_job_administration_events
FOR EACH ROW EXECUTE FUNCTION prevent_durable_job_admin_event_mutation();

CREATE INDEX IF NOT EXISTS idx_durable_jobs_dead_letter_operations
  ON durable_jobs(updated_at DESC, id DESC)
  WHERE status='DEAD_LETTER';
CREATE INDEX IF NOT EXISTS idx_durable_jobs_pending_operations
  ON durable_jobs(updated_at DESC, id DESC)
  WHERE status='PENDING';

COMMIT;
