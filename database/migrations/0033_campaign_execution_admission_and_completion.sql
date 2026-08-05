BEGIN;
CREATE TABLE campaign_capacity_assessments (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), campaign_id uuid NOT NULL REFERENCES campaigns(id), capacity_reference_id uuid NOT NULL, evidence_version text NOT NULL,
 remaining_recipients bigint NOT NULL CHECK(remaining_recipients>=0), available_messages_per_minute integer NOT NULL CHECK(available_messages_per_minute>=0), available_daily_capacity bigint NOT NULL CHECK(available_daily_capacity>=0),
 safety_margin_percent integer NOT NULL CHECK(safety_margin_percent BETWEEN 0 AND 90), required_messages_per_minute numeric NOT NULL, effective_messages_per_minute numeric NOT NULL,
 forecast_completion_at timestamptz, deadline_at timestamptz NOT NULL, decision text NOT NULL CHECK(decision IN('ADMIT','HOLD','REJECT')), reasons jsonb NOT NULL DEFAULT '[]'::jsonb, evaluated_at timestamptz NOT NULL
);
CREATE INDEX idx_campaign_capacity_assessments_campaign_time ON campaign_capacity_assessments(campaign_id,evaluated_at DESC);
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS execution_started_at timestamptz, ADD COLUMN IF NOT EXISTS execution_completed_at timestamptz, ADD COLUMN IF NOT EXISTS execution_last_assessed_at timestamptz;
CREATE TABLE campaign_execution_events(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),campaign_id uuid NOT NULL REFERENCES campaigns(id),event_type text NOT NULL,actor_id text NOT NULL,reason text,details jsonb NOT NULL DEFAULT '{}'::jsonb,created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX idx_campaign_execution_events_campaign_time ON campaign_execution_events(campaign_id,created_at DESC);
CREATE TABLE campaign_execution_leases(
 campaign_id uuid PRIMARY KEY REFERENCES campaigns(id) ON DELETE CASCADE, owner text NOT NULL, fence_token bigint NOT NULL DEFAULT 1, expires_at timestamptz NOT NULL, updated_at timestamptz NOT NULL
);
CREATE INDEX idx_campaign_execution_leases_expiry ON campaign_execution_leases(expires_at);
COMMIT;
