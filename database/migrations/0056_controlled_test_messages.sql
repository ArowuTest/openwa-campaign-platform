BEGIN;

CREATE TABLE IF NOT EXISTS approved_test_recipients (
    id uuid PRIMARY KEY,
    label text NOT NULL CHECK (length(btrim(label)) > 0),
    msisdn_encrypted bytea NOT NULL,
    msisdn_lookup_hash bytea NOT NULL,
    masked_msisdn text NOT NULL,
    status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','REVOKED')),
    created_by uuid NOT NULL REFERENCES internal_users(id),
    submitted_by uuid NULL REFERENCES internal_users(id),
    approved_by uuid NULL REFERENCES internal_users(id),
    reason text NOT NULL CHECK (length(btrim(reason)) >= 8),
    version bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_active_test_recipient_lookup ON approved_test_recipients(msisdn_lookup_hash) WHERE status IN ('DRAFT','PENDING_APPROVAL','ACTIVE');
CREATE INDEX IF NOT EXISTS idx_test_recipients_status ON approved_test_recipients(status,created_at DESC);

CREATE TABLE IF NOT EXISTS test_message_sends (
    id uuid PRIMARY KEY,
    campaign_id uuid NOT NULL REFERENCES campaigns(id),
    message_version_id uuid NOT NULL REFERENCES message_versions(id),
    message_content_hash text NOT NULL CHECK (length(message_content_hash)=64),
    test_recipient_id uuid NOT NULL REFERENCES approved_test_recipients(id),
    gateway_pool_id uuid NOT NULL REFERENCES gateway_pools(id),
    sender_pool_id uuid NULL REFERENCES sender_pools(id),
    provider text NOT NULL CHECK (provider='OPENWA'),
    engine text NOT NULL CHECK (engine IN ('WHATSAPP_WEB_JS','BAILEYS')),
    sender_session_id uuid NOT NULL REFERENCES sender_sessions(id),
    variable_values jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(variable_values)='object'),
    status text NOT NULL CHECK (status IN ('PENDING','PROCESSING','ACCEPTED','FAILED','UNKNOWN')),
    provider_message_id text NULL,
    failure_code text NULL,
    created_by uuid NOT NULL REFERENCES internal_users(id),
    reason text NOT NULL CHECK (length(btrim(reason)) >= 8),
    idempotency_key text NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 16 AND 128),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    lease_owner text NULL,
    lease_version bigint NOT NULL DEFAULT 0 CHECK (lease_version >= 0),
    lease_expires_at timestamptz NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    completed_at timestamptz NULL,
    CHECK ((status='PROCESSING') = (lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)),
    CHECK ((status IN ('ACCEPTED','FAILED','UNKNOWN')) = (completed_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS idx_test_message_claim ON test_message_sends(status,lease_expires_at,created_at) WHERE status IN ('PENDING','PROCESSING');
CREATE INDEX IF NOT EXISTS idx_test_message_campaign ON test_message_sends(campaign_id,created_at DESC);
CREATE INDEX IF NOT EXISTS idx_test_message_session ON test_message_sends(sender_session_id,created_at DESC);

COMMIT;
