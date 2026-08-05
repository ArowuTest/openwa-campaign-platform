BEGIN;

-- Optimistic concurrency is mandatory for every operator-controlled aggregate.
ALTER TABLE campaigns
  ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS consent_review_id uuid REFERENCES consent_reviews(id),
  ADD COLUMN IF NOT EXISTS audience_snapshot_id uuid REFERENCES audience_snapshots(id),
  ADD COLUMN IF NOT EXISTS approved_message_version_id uuid REFERENCES message_versions(id),
  ADD COLUMN IF NOT EXISTS final_approved_by uuid REFERENCES internal_users(id),
  ADD COLUMN IF NOT EXISTS final_approved_at timestamptz,
  ADD COLUMN IF NOT EXISTS configuration_versions jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE consent_reviews
  ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;

ALTER TABLE organisations DROP CONSTRAINT IF EXISTS organisations_status_check;
ALTER TABLE organisations ADD CONSTRAINT organisations_status_check
  CHECK (status IN ('UNDER_REVIEW','ACTIVE','SUSPENDED','CLOSED'));

ALTER TABLE organisations
  ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS registration_reference text,
  ADD COLUMN IF NOT EXISTS industry text,
  ADD COLUMN IF NOT EXISTS controller_role text,
  ADD COLUMN IF NOT EXISTS restrictions jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS retention_policy jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS report_branding jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE UNIQUE INDEX IF NOT EXISTS uq_organisations_normalised_legal_country
  ON organisations (lower(regexp_replace(legal_name, '\s+', ' ', 'g')), coalesce(country_id::text, ''))
  WHERE status <> 'CLOSED';

-- Credentials and sessions are separated from profile records and never returned by ordinary queries.
CREATE TABLE IF NOT EXISTS internal_user_credentials (
  user_id uuid PRIMARY KEY REFERENCES internal_users(id) ON DELETE CASCADE,
  password_hash text NOT NULL,
  totp_secret_ciphertext bytea,
  credential_version bigint NOT NULL DEFAULT 1,
  failed_login_count integer NOT NULL DEFAULT 0 CHECK (failed_login_count >= 0),
  locked_until timestamptz,
  password_changed_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS internal_user_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES internal_users(id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE,
  csrf_hash bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  idle_expires_at timestamptz NOT NULL,
  absolute_expires_at timestamptz NOT NULL,
  mfa_verified_at timestamptz,
  source_ip inet,
  user_agent text,
  revoked_at timestamptz,
  revoke_reason text,
  CHECK (idle_expires_at <= absolute_expires_at)
);
CREATE INDEX IF NOT EXISTS idx_internal_user_sessions_active
  ON internal_user_sessions(user_id, idle_expires_at, absolute_expires_at)
  WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS authentication_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid REFERENCES internal_users(id),
  email_hash bytea,
  event_type text NOT NULL CHECK (event_type IN (
    'LOGIN_SUCCEEDED','LOGIN_FAILED','MFA_SUCCEEDED','MFA_FAILED','ACCOUNT_LOCKED',
    'SESSION_REVOKED','ALL_SESSIONS_REVOKED','PERMISSION_DENIED','STEP_UP_REQUIRED'
  )),
  source_ip inet,
  user_agent text,
  correlation_id text NOT NULL,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_authentication_events_user_time
  ON authentication_events(user_id, occurred_at DESC);

-- Tamper-evident audit sequence. The database writer role shall have INSERT only.
ALTER TABLE audit_events
  ADD COLUMN IF NOT EXISTS sequence bigint,
  ADD COLUMN IF NOT EXISTS previous_hash text,
  ADD COLUMN IF NOT EXISTS event_hash text,
  ADD COLUMN IF NOT EXISTS reason_code text,
  ADD COLUMN IF NOT EXISTS reason text,
  ADD COLUMN IF NOT EXISTS correlation_id text;

CREATE UNIQUE INDEX IF NOT EXISTS uq_audit_events_sequence ON audit_events(sequence) WHERE sequence IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_audit_events_hash ON audit_events(event_hash) WHERE event_hash IS NOT NULL;

CREATE TABLE IF NOT EXISTS audit_chain_head (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  sequence bigint NOT NULL DEFAULT 0,
  event_hash text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO audit_chain_head(singleton) VALUES (true) ON CONFLICT (singleton) DO NOTHING;

-- Authoritative durable jobs. Redis may accelerate notification but is never the source of obligation.
CREATE TABLE IF NOT EXISTS durable_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  job_type text NOT NULL,
  deduplication_key text NOT NULL UNIQUE,
  payload jsonb NOT NULL,
  status text NOT NULL DEFAULT 'PENDING' CHECK (status IN (
    'PENDING','PROCESSING','COMPLETED','FAILED','DEAD_LETTER','CANCELLED'
  )),
  priority integer NOT NULL DEFAULT 0,
  attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
  available_at timestamptz NOT NULL DEFAULT now(),
  lease_owner text,
  lease_expires_at timestamptz,
  last_error_code text,
  last_error_detail text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  CHECK ((status = 'PROCESSING') = (lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS idx_durable_jobs_claim
  ON durable_jobs(priority DESC, available_at, created_at)
  WHERE status IN ('PENDING','PROCESSING');
CREATE INDEX IF NOT EXISTS idx_durable_jobs_type_status
  ON durable_jobs(job_type, status, available_at);

-- Exactly one current worker may own a live provider session.
CREATE TABLE IF NOT EXISTS sender_session_leases (
  session_id uuid PRIMARY KEY REFERENCES sender_sessions(id) ON DELETE CASCADE,
  worker_node_id uuid NOT NULL REFERENCES sender_nodes(id),
  lease_token_hash bytea NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  acquired_at timestamptz NOT NULL DEFAULT now(),
  renewed_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  CHECK (expires_at > acquired_at)
);
CREATE INDEX IF NOT EXISTS idx_sender_session_leases_expiry ON sender_session_leases(expires_at);

ALTER TABLE sender_nodes
  ADD COLUMN IF NOT EXISTS build_version text,
  ADD COLUMN IF NOT EXISTS capacity integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS queue_depth bigint NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS draining boolean NOT NULL DEFAULT false;

ALTER TABLE sender_sessions
  ADD COLUMN IF NOT EXISTS in_flight_limit integer NOT NULL DEFAULT 1 CHECK (in_flight_limit > 0 AND in_flight_limit <= 100),
  ADD COLUMN IF NOT EXISTS proxy_configuration_ciphertext bytea,
  ADD COLUMN IF NOT EXISTS engine_version text,
  ADD COLUMN IF NOT EXISTS capabilities jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE campaign_recipients
  ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS last_event_at timestamptz,
  ADD COLUMN IF NOT EXISTS reconciliation_required boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS contradictory_event_count integer NOT NULL DEFAULT 0 CHECK (contradictory_event_count >= 0),
  ADD COLUMN IF NOT EXISTS highest_acknowledgement text CHECK (highest_acknowledgement IN ('GATEWAY_ACCEPTED','SENT','DELIVERED','READ'));

ALTER TABLE delivery_events
  ALTER COLUMN event_deduplication_key SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_campaign_recipients_reconciliation
  ON campaign_recipients(campaign_id, reconciliation_required)
  WHERE reconciliation_required;

-- Immutable snapshots and approved message versions cannot be updated or deleted by application roles.
CREATE OR REPLACE FUNCTION prevent_immutable_campaign_evidence_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION '% records are immutable', TG_TABLE_NAME;
  END IF;
  IF TG_TABLE_NAME = 'audience_snapshots' THEN
    RAISE EXCEPTION 'audience snapshots are immutable';
  END IF;
  IF TG_TABLE_NAME = 'message_versions' AND OLD.status = 'APPROVED' THEN
    RAISE EXCEPTION 'approved message versions are immutable';
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_audience_snapshots_immutable ON audience_snapshots;
CREATE TRIGGER trg_audience_snapshots_immutable
BEFORE UPDATE OR DELETE ON audience_snapshots
FOR EACH ROW EXECUTE FUNCTION prevent_immutable_campaign_evidence_mutation();

DROP TRIGGER IF EXISTS trg_message_versions_immutable ON message_versions;
CREATE TRIGGER trg_message_versions_immutable
BEFORE UPDATE OR DELETE ON message_versions
FOR EACH ROW EXECUTE FUNCTION prevent_immutable_campaign_evidence_mutation();

COMMIT;
