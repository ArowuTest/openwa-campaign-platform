BEGIN;
CREATE TABLE sender_hourly_submission_usage (
    sender_session_id uuid NOT NULL REFERENCES sender_sessions(id),
    hour_start timestamptz NOT NULL,
    submission_count bigint NOT NULL DEFAULT 0 CHECK (submission_count >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(sender_session_id,hour_start)
);
CREATE TABLE sender_daily_submission_usage (
    sender_session_id uuid NOT NULL REFERENCES sender_sessions(id),
    usage_date date NOT NULL,
    submission_count bigint NOT NULL DEFAULT 0 CHECK (submission_count >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(sender_session_id,usage_date)
);
CREATE TABLE sender_session_campaign_assignments (
    sender_session_id uuid NOT NULL REFERENCES sender_sessions(id),
    campaign_id uuid NOT NULL REFERENCES campaigns(id),
    status text NOT NULL CHECK (status IN ('ACTIVE','RELEASED')),
    assigned_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz NULL,
    PRIMARY KEY(sender_session_id,campaign_id)
);
CREATE INDEX idx_sender_campaign_assignments_active ON sender_session_campaign_assignments(sender_session_id,status,last_used_at);
CREATE TABLE sender_pacing_runtime (
    sender_session_id uuid PRIMARY KEY REFERENCES sender_sessions(id),
    next_allowed_at timestamptz NOT NULL DEFAULT now(),
    jitter_sequence bigint NOT NULL DEFAULT 0 CHECK (jitter_sequence >= 0),
    policy_id uuid NULL REFERENCES sender_pacing_policies(id),
    policy_version bigint NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMIT;
