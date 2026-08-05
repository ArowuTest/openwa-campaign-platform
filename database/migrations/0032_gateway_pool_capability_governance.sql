BEGIN;

CREATE TABLE IF NOT EXISTS gateway_pools (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    provider text NOT NULL CHECK (provider = 'OPENWA'),
    engine text NOT NULL CHECK (engine IN ('WHATSAPP_WEB_JS','BAILEYS')),
    adapter_version text NOT NULL,
    status text NOT NULL CHECK (status IN ('ACTIVE','PAUSED','RETIRED')),
    capabilities jsonb NOT NULL CHECK (jsonb_typeof(capabilities)='array' AND jsonb_array_length(capabilities) > 0),
    minimum_healthy_nodes integer NOT NULL CHECK (minimum_healthy_nodes BETWEEN 1 AND 1000),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, engine, name)
);

CREATE TABLE IF NOT EXISTS gateway_pool_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    gateway_pool_id uuid NOT NULL REFERENCES gateway_pools(id),
    event_type text NOT NULL,
    version bigint NOT NULL,
    actor_id uuid,
    reason text NOT NULL,
    evidence jsonb NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE sender_nodes ADD COLUMN IF NOT EXISTS gateway_pool_id uuid REFERENCES gateway_pools(id);
ALTER TABLE sender_sessions ADD COLUMN IF NOT EXISTS gateway_pool_id uuid REFERENCES gateway_pools(id);

CREATE INDEX IF NOT EXISTS gateway_pools_active_engine_idx ON gateway_pools(provider, engine) WHERE status='ACTIVE';
CREATE INDEX IF NOT EXISTS gateway_pool_events_pool_time_idx ON gateway_pool_events(gateway_pool_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS sender_nodes_gateway_pool_idx ON sender_nodes(gateway_pool_id, status, last_heartbeat_at);
CREATE INDEX IF NOT EXISTS sender_sessions_gateway_pool_idx ON sender_sessions(gateway_pool_id, status, last_heartbeat_at);

COMMIT;
