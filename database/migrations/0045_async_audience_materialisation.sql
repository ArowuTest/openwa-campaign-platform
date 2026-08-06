BEGIN;

CREATE TABLE audience_materialisation_jobs (
    id uuid PRIMARY KEY,
    campaign_id uuid NOT NULL REFERENCES campaigns(id),
    segment_id uuid NULL REFERENCES segments(id),
    segment_definition jsonb NOT NULL,
    definition_version bigint NOT NULL CHECK (definition_version > 0),
    eligibility_context jsonb NOT NULL,
    consent_policy_version text NOT NULL,
    configuration_version text NOT NULL,
    requested_by uuid NOT NULL REFERENCES internal_users(id),
    request_fingerprint text NOT NULL CHECK (length(request_fingerprint)=64),
    status text NOT NULL CHECK (status IN ('PENDING','RUNNING','COMPLETED','FAILED','CANCELLED')),
    processed_count bigint NOT NULL DEFAULT 0 CHECK (processed_count >= 0),
    expected_count bigint NOT NULL CHECK (expected_count > 0),
    last_contact_id uuid NULL,
    rolling_hash text NOT NULL CHECK (length(rolling_hash)=64),
    snapshot_id uuid NULL REFERENCES audience_snapshots(id),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at timestamptz NOT NULL,
    failure_code text NULL,
    failure_reference text NULL,
    lease_owner text NULL,
    lease_token bigint NOT NULL DEFAULT 0 CHECK (lease_token >= 0),
    lease_expires_at timestamptz NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    requested_at timestamptz NOT NULL,
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    cancelled_by uuid NULL REFERENCES internal_users(id),
    cancellation_reason text NULL,
    updated_at timestamptz NOT NULL,
    CHECK (processed_count <= expected_count),
    CHECK ((status='COMPLETED' AND snapshot_id IS NOT NULL AND completed_at IS NOT NULL) OR status<>'COMPLETED'),
    CHECK ((status IN ('FAILED','CANCELLED') AND completed_at IS NOT NULL) OR status NOT IN ('FAILED','CANCELLED')),
    CHECK (status<>'RUNNING' OR (lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)),
    CHECK (status<>'CANCELLED' OR (cancelled_by IS NOT NULL AND length(trim(cancellation_reason)) >= 5))
);

CREATE TABLE audience_materialisation_members (
    job_id uuid NOT NULL REFERENCES audience_materialisation_jobs(id) ON DELETE CASCADE,
    contact_id uuid NOT NULL REFERENCES contacts(id),
    eligibility_evidence_hash text NOT NULL CHECK (length(eligibility_evidence_hash)=64),
    created_at timestamptz NOT NULL,
    PRIMARY KEY(job_id,contact_id)
);

CREATE UNIQUE INDEX audience_materialisation_jobs_request_fingerprint_uq
ON audience_materialisation_jobs(campaign_id,request_fingerprint);
CREATE UNIQUE INDEX audience_materialisation_jobs_one_active_campaign_uq
ON audience_materialisation_jobs(campaign_id)
WHERE status IN ('PENDING','RUNNING');

CREATE INDEX audience_materialisation_jobs_claim_idx
ON audience_materialisation_jobs(status,next_attempt_at,lease_expires_at,requested_at)
WHERE status IN ('PENDING','RUNNING');
CREATE INDEX audience_materialisation_jobs_campaign_idx
ON audience_materialisation_jobs(campaign_id,requested_at DESC);
CREATE INDEX audience_materialisation_members_order_idx
ON audience_materialisation_members(job_id,contact_id);

CREATE OR REPLACE FUNCTION prevent_materialisation_member_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audience materialisation members are append-only';
END $$;
CREATE TRIGGER audience_materialisation_members_no_update
BEFORE UPDATE OR DELETE ON audience_materialisation_members
FOR EACH ROW EXECUTE FUNCTION prevent_materialisation_member_mutation();

COMMIT;
