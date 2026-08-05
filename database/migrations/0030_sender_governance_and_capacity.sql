BEGIN;
CREATE TABLE IF NOT EXISTS sender_pools (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 name text NOT NULL UNIQUE,
 organisation_id uuid REFERENCES organisations(id),
 status text NOT NULL CHECK(status IN('ACTIVE','PAUSED','RETIRED')),
 max_messages_per_minute integer NOT NULL CHECK(max_messages_per_minute>0),
 daily_capacity bigint NOT NULL CHECK(daily_capacity>0),
 reserved_capacity bigint NOT NULL DEFAULT 0 CHECK(reserved_capacity>=0 AND reserved_capacity<=daily_capacity),
 version bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE sender_nodes ADD COLUMN IF NOT EXISTS governance_version bigint NOT NULL DEFAULT 1;
ALTER TABLE sender_sessions
 ADD COLUMN IF NOT EXISTS sender_pool_id uuid REFERENCES sender_pools(id),
 ADD COLUMN IF NOT EXISTS governance_version bigint NOT NULL DEFAULT 1,
 ADD COLUMN IF NOT EXISTS sent_today bigint NOT NULL DEFAULT 0 CHECK(sent_today>=0);
CREATE INDEX IF NOT EXISTS idx_sender_sessions_governed_capacity ON sender_sessions(sender_pool_id,status,last_heartbeat_at) WHERE status IN('READY','BUSY');
CREATE TABLE IF NOT EXISTS sender_governance_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), object_type text NOT NULL CHECK(object_type IN('POOL','NODE','SESSION')),
 object_id uuid NOT NULL, action text NOT NULL, actor_id uuid NOT NULL REFERENCES internal_users(id), reason text NOT NULL,
 object_version bigint NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_sender_governance_events_object ON sender_governance_events(object_type,object_id,created_at DESC);
COMMIT;
