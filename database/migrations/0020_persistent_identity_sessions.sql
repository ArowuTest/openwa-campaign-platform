BEGIN;

ALTER TABLE internal_users
  ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS internal_mfa_challenges (
  token_hash bytea PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES internal_users(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS idx_internal_mfa_challenges_expiry
  ON internal_mfa_challenges(expires_at)
  WHERE used_at IS NULL;

CREATE OR REPLACE FUNCTION prevent_identity_secret_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.user_id <> NEW.user_id THEN
    RAISE EXCEPTION 'identity credential owner is immutable';
  END IF;
  RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS trg_identity_credential_owner_immutable ON internal_user_credentials;
CREATE TRIGGER trg_identity_credential_owner_immutable
BEFORE UPDATE ON internal_user_credentials
FOR EACH ROW EXECUTE FUNCTION prevent_identity_secret_mutation();

CREATE INDEX IF NOT EXISTS idx_internal_users_status_email
  ON internal_users(status, email);
CREATE INDEX IF NOT EXISTS idx_internal_user_credentials_lock
  ON internal_user_credentials(locked_until)
  WHERE locked_until IS NOT NULL;

COMMIT;
