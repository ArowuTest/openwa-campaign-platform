BEGIN;

ALTER TABLE internal_users
  ADD COLUMN IF NOT EXISTS created_by uuid REFERENCES internal_users(id),
  ADD COLUMN IF NOT EXISTS updated_by uuid REFERENCES internal_users(id);

CREATE TABLE IF NOT EXISTS internal_user_change_history (
  id bigserial PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES internal_users(id),
  user_version bigint NOT NULL,
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  reason text NOT NULL CHECK (char_length(reason) BETWEEN 5 AND 1000),
  snapshot jsonb NOT NULL,
  changed_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_id,user_version)
);
CREATE INDEX IF NOT EXISTS idx_internal_user_change_history_user
  ON internal_user_change_history(user_id,user_version DESC);

CREATE OR REPLACE FUNCTION prevent_internal_user_history_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'internal user change history is append-only';
END $$;
DROP TRIGGER IF EXISTS trg_internal_user_history_immutable ON internal_user_change_history;
CREATE TRIGGER trg_internal_user_history_immutable
BEFORE UPDATE OR DELETE ON internal_user_change_history
FOR EACH ROW EXECUTE FUNCTION prevent_internal_user_history_mutation();

COMMIT;
