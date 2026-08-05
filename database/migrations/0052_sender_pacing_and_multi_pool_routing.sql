BEGIN;

CREATE TABLE sender_pacing_policies (
    id uuid PRIMARY KEY,
    scope text NOT NULL CHECK (scope IN ('PLATFORM','PROVIDER','ENGINE','GATEWAY_POOL','SENDER_POOL','SESSION','CAMPAIGN')),
    scope_id uuid NULL,
    provider text NULL,
    engine text NULL CHECK (engine IS NULL OR engine IN ('WHATSAPP_WEB_JS','BAILEYS')),
    minimum_delay_ms bigint NOT NULL CHECK (minimum_delay_ms >= 0 AND minimum_delay_ms <= 300000),
    maximum_delay_ms bigint NOT NULL CHECK (maximum_delay_ms >= minimum_delay_ms AND maximum_delay_ms <= 300000),
    jitter_mode text NOT NULL CHECK (jitter_mode IN ('NONE','UNIFORM')),
    max_in_flight integer NOT NULL CHECK (max_in_flight BETWEEN 1 AND 100),
    messages_per_minute integer NOT NULL CHECK (messages_per_minute BETWEEN 1 AND 100000),
    hourly_allowance bigint NOT NULL CHECK (hourly_allowance > 0),
    daily_allowance bigint NOT NULL CHECK (daily_allowance >= hourly_allowance),
    max_active_campaigns integer NOT NULL CHECK (max_active_campaigns BETWEEN 1 AND 1000),
    burst_size integer NOT NULL CHECK (burst_size BETWEEN 1 AND 1000),
    cooldown_seconds integer NOT NULL CHECK (cooldown_seconds BETWEEN 0 AND 86400),
    recovery_ramp_minutes integer NOT NULL CHECK (recovery_ramp_minutes BETWEEN 0 AND 1440),
    failure_threshold_bps integer NOT NULL CHECK (failure_threshold_bps BETWEEN 0 AND 10000),
    disconnect_threshold integer NOT NULL CHECK (disconnect_threshold BETWEEN 0 AND 1000),
    auto_quarantine boolean NOT NULL DEFAULT false,
    message_type_overrides jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(message_type_overrides)='array'),
    status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','RETIRED')),
    effective_from timestamptz NOT NULL,
    effective_to timestamptz NULL,
    version bigint NOT NULL CHECK (version > 0),
    created_by uuid NOT NULL,
    submitted_by uuid NULL,
    approved_by uuid NULL,
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((scope='PLATFORM' AND scope_id IS NULL) OR (scope<>'PLATFORM' AND scope_id IS NOT NULL))
);
CREATE UNIQUE INDEX uq_sender_pacing_active_scope ON sender_pacing_policies(scope, coalesce(scope_id,'00000000-0000-0000-0000-000000000000'::uuid)) WHERE status='ACTIVE' AND effective_to IS NULL;
CREATE INDEX idx_sender_pacing_resolution ON sender_pacing_policies(scope,scope_id,status,effective_from,effective_to);

CREATE TABLE campaign_routing_plans (
    id uuid PRIMARY KEY,
    campaign_id uuid NOT NULL REFERENCES campaigns(id),
    plan_version bigint NOT NULL CHECK (plan_version > 0),
    routing_policy_version text NOT NULL,
    capacity_evidence_version text NOT NULL,
    pacing_policy_version text NOT NULL,
    fallback_mode text NOT NULL CHECK (fallback_mode='NONE'),
    approved_at timestamptz NOT NULL,
    approved_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(campaign_id,plan_version)
);
CREATE TABLE campaign_routing_plan_pools (
    routing_plan_id uuid NOT NULL REFERENCES campaign_routing_plans(id) ON DELETE RESTRICT,
    sender_pool_id uuid NOT NULL REFERENCES sender_pools(id),
    gateway_pool_id uuid NOT NULL REFERENCES gateway_pools(id),
    provider text NOT NULL CHECK (provider='OPENWA'),
    engine text NOT NULL CHECK (engine IN ('WHATSAPP_WEB_JS','BAILEYS')),
    allocation_weight integer NOT NULL CHECK (allocation_weight BETWEEN 1 AND 10000),
    maximum_recipients bigint NOT NULL CHECK (maximum_recipients > 0),
    reserved_messages_per_minute integer NOT NULL CHECK (reserved_messages_per_minute > 0),
    reserved_hourly_units bigint NOT NULL CHECK (reserved_hourly_units > 0),
    reserved_daily_units bigint NOT NULL CHECK (reserved_daily_units >= reserved_hourly_units),
    allow_reallocation_in boolean NOT NULL DEFAULT false,
    allow_reallocation_out boolean NOT NULL DEFAULT false,
    PRIMARY KEY(routing_plan_id,sender_pool_id)
);
CREATE INDEX idx_campaign_route_pool_lookup ON campaign_routing_plan_pools(sender_pool_id,routing_plan_id);

CREATE TABLE campaign_pool_capacity_reservations (
    id uuid PRIMARY KEY,
    campaign_id uuid NOT NULL REFERENCES campaigns(id),
    routing_plan_id uuid NOT NULL REFERENCES campaign_routing_plans(id),
    sender_pool_id uuid NOT NULL REFERENCES sender_pools(id),
    reservation_start timestamptz NOT NULL,
    reservation_end timestamptz NOT NULL CHECK (reservation_end > reservation_start),
    reserved_messages_per_minute integer NOT NULL CHECK (reserved_messages_per_minute > 0),
    reserved_hourly_units bigint NOT NULL CHECK (reserved_hourly_units > 0),
    reserved_daily_units bigint NOT NULL CHECK (reserved_daily_units >= reserved_hourly_units),
    status text NOT NULL CHECK (status IN ('HELD','ACTIVE','RELEASED','EXPIRED')),
    fencing_version bigint NOT NULL DEFAULT 1 CHECK (fencing_version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_pool_capacity_reservation_window ON campaign_pool_capacity_reservations(sender_pool_id,status,reservation_start,reservation_end);

ALTER TABLE campaign_dispatch_shards ADD COLUMN IF NOT EXISTS assigned_sender_pool_id uuid NULL REFERENCES sender_pools(id);
ALTER TABLE campaign_dispatch_shards ADD COLUMN IF NOT EXISTS routing_plan_id uuid NULL REFERENCES campaign_routing_plans(id);
CREATE INDEX idx_dispatch_shards_pool_status ON campaign_dispatch_shards(assigned_sender_pool_id,status,ordinal);

COMMIT;
