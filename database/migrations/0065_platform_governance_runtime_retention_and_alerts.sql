BEGIN;

-- Gateway pools now follow an explicit governance lifecycle. Existing active
-- pools remain active; new administration flows create drafts and require an
-- independent approval before runtime admission.
ALTER TABLE gateway_pools
  ADD COLUMN IF NOT EXISTS created_by uuid,
  ADD COLUMN IF NOT EXISTS submitted_by uuid,
  ADD COLUMN IF NOT EXISTS approved_by uuid,
  ADD COLUMN IF NOT EXISTS effective_from timestamptz,
  ADD COLUMN IF NOT EXISTS effective_to timestamptz,
  ADD COLUMN IF NOT EXISTS approval_reason text;

ALTER TABLE gateway_pools DROP CONSTRAINT IF EXISTS gateway_pools_status_check;
ALTER TABLE gateway_pools ADD CONSTRAINT gateway_pools_status_check
  CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','PAUSED','REJECTED','RETIRED'));
ALTER TABLE gateway_pools DROP CONSTRAINT IF EXISTS gateway_pools_effective_period_check;
ALTER TABLE gateway_pools ADD CONSTRAINT gateway_pools_effective_period_check
  CHECK (effective_to IS NULL OR effective_from IS NULL OR effective_to > effective_from);
UPDATE gateway_pools SET effective_from=coalesce(effective_from,created_at) WHERE status IN ('ACTIVE','PAUSED');
CREATE INDEX IF NOT EXISTS gateway_pools_effective_route_idx
  ON gateway_pools(provider,engine,effective_from,effective_to)
  WHERE status IN ('ACTIVE','PAUSED');

ALTER TABLE sender_nodes DROP CONSTRAINT IF EXISTS sender_nodes_status_check;
ALTER TABLE sender_nodes ADD CONSTRAINT sender_nodes_status_check
  CHECK (status IN ('READY','DRAINING','UNHEALTHY','OFFLINE','RETIRED'));

ALTER TABLE sender_nodes
  ADD COLUMN IF NOT EXISTS gateway_version text,
  ADD COLUMN IF NOT EXISTS worker_version text,
  ADD COLUMN IF NOT EXISTS configuration_version text,
  ADD COLUMN IF NOT EXISTS runtime_capabilities jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN IF NOT EXISTS runtime_state text,
  ADD COLUMN IF NOT EXISTS session_count integer NOT NULL DEFAULT 0 CHECK (session_count >= 0),
  ADD COLUMN IF NOT EXISTS cpu_percent numeric(7,3) NOT NULL DEFAULT 0 CHECK (cpu_percent >= 0 AND cpu_percent <= 100),
  ADD COLUMN IF NOT EXISTS memory_bytes bigint NOT NULL DEFAULT 0 CHECK (memory_bytes >= 0),
  ADD COLUMN IF NOT EXISTS registered_at timestamptz;

ALTER TABLE sender_nodes DROP CONSTRAINT IF EXISTS sender_nodes_runtime_capabilities_shape_check;
ALTER TABLE sender_nodes ADD CONSTRAINT sender_nodes_runtime_capabilities_shape_check
  CHECK (jsonb_typeof(runtime_capabilities)='array');
ALTER TABLE sender_nodes DROP CONSTRAINT IF EXISTS sender_nodes_runtime_state_check;
ALTER TABLE sender_nodes ADD CONSTRAINT sender_nodes_runtime_state_check
  CHECK (runtime_state IS NULL OR runtime_state IN ('READY','DEGRADED','UNAVAILABLE','DRAINING'));

CREATE TABLE IF NOT EXISTS gateway_runtime_nonces (
  nonce text PRIMARY KEY,
  node_id uuid NOT NULL REFERENCES sender_nodes(id),
  request_hash text NOT NULL,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (length(nonce) BETWEEN 16 AND 200),
  CHECK (length(request_hash)=64)
);
CREATE INDEX IF NOT EXISTS gateway_runtime_nonces_expiry_idx ON gateway_runtime_nonces(expires_at);

CREATE TABLE IF NOT EXISTS gateway_runtime_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id uuid NOT NULL REFERENCES sender_nodes(id),
  gateway_pool_id uuid NOT NULL REFERENCES gateway_pools(id),
  event_type text NOT NULL CHECK (event_type IN ('REGISTERED','HEARTBEAT','REJECTED','DRAINING','OFFLINE','RETIRED')),
  node_version bigint NOT NULL CHECK (node_version > 0),
  boot_id text NOT NULL,
  runtime_identity jsonb NOT NULL,
  request_hash text,
  reason text NOT NULL,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  CHECK (jsonb_typeof(runtime_identity)='object'),
  CHECK (request_hash IS NULL OR length(request_hash)=64)
);
CREATE INDEX IF NOT EXISTS gateway_runtime_events_node_time_idx
  ON gateway_runtime_events(node_id,occurred_at DESC);

CREATE TABLE IF NOT EXISTS platform_configurations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  configuration_key text NOT NULL,
  scope_type text NOT NULL CHECK (scope_type IN ('PLATFORM','ENVIRONMENT','ORGANISATION','CAMPAIGN','PROVIDER','GATEWAY_POOL','SENDER_POOL')),
  scope_id text NOT NULL DEFAULT '',
  value jsonb NOT NULL,
  value_checksum text NOT NULL CHECK (length(value_checksum)=64),
  status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','SUPERSEDED','RETIRED')),
  effective_from timestamptz NOT NULL,
  effective_to timestamptz,
  created_by uuid NOT NULL,
  submitted_by uuid,
  approved_by uuid,
  supersedes_id uuid REFERENCES platform_configurations(id),
  rollback_of_id uuid REFERENCES platform_configurations(id),
  reason text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (length(trim(configuration_key)) BETWEEN 3 AND 120),
  CHECK (effective_to IS NULL OR effective_to > effective_from),
  CHECK (octet_length(value::text) <= 65536)
);
CREATE INDEX IF NOT EXISTS platform_configurations_resolve_idx
  ON platform_configurations(configuration_key,scope_type,scope_id,effective_from DESC)
  WHERE status IN ('ACTIVE','SUPERSEDED');
CREATE INDEX IF NOT EXISTS platform_configurations_status_idx
  ON platform_configurations(status,updated_at DESC);

CREATE TABLE IF NOT EXISTS platform_configuration_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  configuration_id uuid NOT NULL REFERENCES platform_configurations(id),
  event_type text NOT NULL,
  version bigint NOT NULL,
  actor_id uuid NOT NULL,
  reason text NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS platform_configuration_events_object_time_idx
  ON platform_configuration_events(configuration_id,occurred_at DESC);

CREATE TABLE IF NOT EXISTS maintenance_windows (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  mode text NOT NULL CHECK (mode IN ('READ_ONLY','ADMISSION_FROZEN','DRAINING','EMERGENCY_STOP')),
  scope_type text NOT NULL CHECK (scope_type IN ('PLATFORM','PROVIDER','GATEWAY_POOL','SENDER_POOL')),
  scope_id text NOT NULL DEFAULT '',
  starts_at timestamptz NOT NULL,
  ends_at timestamptz,
  allow_active_dispatch boolean NOT NULL DEFAULT false,
  status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','ENDED','CANCELLED')),
  created_by uuid NOT NULL,
  submitted_by uuid,
  approved_by uuid,
  ended_by uuid,
  reason text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (ends_at IS NULL OR ends_at > starts_at)
);
CREATE INDEX IF NOT EXISTS maintenance_windows_active_idx
  ON maintenance_windows(scope_type,scope_id,starts_at,ends_at)
  WHERE status='ACTIVE';

CREATE TABLE IF NOT EXISTS maintenance_window_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  maintenance_window_id uuid NOT NULL REFERENCES maintenance_windows(id),
  event_type text NOT NULL,
  version bigint NOT NULL,
  actor_id uuid NOT NULL,
  reason text NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS maintenance_window_events_object_time_idx
  ON maintenance_window_events(maintenance_window_id,occurred_at DESC);

CREATE TABLE IF NOT EXISTS retention_policies (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  object_type text NOT NULL CHECK (object_type IN ('INBOUND_CONTENT','AUDIENCE_IMPORT_SOURCE','EXPORT_OBJECT','PROVIDER_EVENT','DELIVERY_EVENT','AUDIT_EVENT','INCIDENT','PRIVACY_CASE')),
  action text NOT NULL CHECK (action IN ('ARCHIVE','DELETE','ANONYMISE','REVIEW_REQUIRED','RETAIN_INDEFINITELY')),
  scope_type text NOT NULL CHECK (scope_type IN ('PLATFORM','ORGANISATION')),
  scope_id text NOT NULL DEFAULT '',
  retention_days integer CHECK (retention_days BETWEEN 1 AND 36500),
  respect_legal_holds boolean NOT NULL DEFAULT true,
  status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','RETIRED')),
  effective_from timestamptz NOT NULL,
  effective_to timestamptz,
  created_by uuid NOT NULL,
  submitted_by uuid,
  approved_by uuid,
  reason text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (action='RETAIN_INDEFINITELY' OR retention_days IS NOT NULL),
  CHECK (effective_to IS NULL OR effective_to > effective_from)
);
CREATE INDEX IF NOT EXISTS retention_policies_resolve_idx
  ON retention_policies(object_type,scope_type,scope_id,effective_from DESC)
  WHERE status='ACTIVE';

CREATE TABLE IF NOT EXISTS retention_policy_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  retention_policy_id uuid NOT NULL REFERENCES retention_policies(id),
  event_type text NOT NULL,
  version bigint NOT NULL,
  actor_id uuid NOT NULL,
  reason text NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS retention_policy_events_object_time_idx
  ON retention_policy_events(retention_policy_id,occurred_at DESC);

CREATE TABLE IF NOT EXISTS retention_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  retention_policy_id uuid NOT NULL REFERENCES retention_policies(id),
  object_type text NOT NULL,
  object_id text NOT NULL,
  object_key text,
  action text NOT NULL,
  status text NOT NULL CHECK (status IN ('PENDING','CLAIMED','COMPLETED','FAILED','HELD_REVIEW')),
  available_at timestamptz NOT NULL DEFAULT now(),
  lease_owner text,
  lease_version bigint NOT NULL DEFAULT 0,
  lease_expires_at timestamptz,
  attempt_count integer NOT NULL DEFAULT 0,
  last_error_code text,
  last_error_reference text,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(retention_policy_id,object_type,object_id,action),
  CHECK (object_type IN ('INBOUND_CONTENT','AUDIENCE_IMPORT_SOURCE','EXPORT_OBJECT','PROVIDER_EVENT','DELIVERY_EVENT','AUDIT_EVENT','INCIDENT','PRIVACY_CASE')),
  CHECK (action IN ('ARCHIVE','DELETE','ANONYMISE','REVIEW_REQUIRED','RETAIN_INDEFINITELY')),
  CHECK (status<>'CLAIMED' OR (lease_owner IS NOT NULL AND lease_version>0 AND lease_expires_at IS NOT NULL)),
  CHECK (status<>'COMPLETED' OR completed_at IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS retention_jobs_claim_idx
  ON retention_jobs(status,available_at,lease_expires_at)
  WHERE status IN ('PENDING','FAILED','CLAIMED');

ALTER TABLE operations_incidents DROP CONSTRAINT IF EXISTS operations_incidents_status_check;
ALTER TABLE operations_incidents ADD CONSTRAINT operations_incidents_status_check
  CHECK (status IN ('OPEN','ACKNOWLEDGED','INVESTIGATING','MITIGATED','RESOLVED','CLOSED'));

CREATE TABLE IF NOT EXISTS operational_incident_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  incident_id uuid NOT NULL REFERENCES operations_incidents(id),
  event_type text NOT NULL CHECK (event_type IN ('CREATED','ASSIGNED','ACKNOWLEDGED','INVESTIGATION_NOTE','MITIGATION','RESOLVED','CLOSED','REOPENED','ALERT_LINKED')),
  actor_id uuid,
  detail text NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS operational_incident_events_incident_time_idx
  ON operational_incident_events(incident_id,occurred_at ASC,id ASC);

CREATE TABLE IF NOT EXISTS operational_alert_policies (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  metric text NOT NULL CHECK (metric IN ('QUEUE_DEPTH','UNKNOWN_OUTCOMES','STALE_WORKER_NODES','OPEN_INCIDENTS','CRITICAL_INCIDENTS','UNAVAILABLE_GATEWAY_NODES','UNHEALTHY_SENDER_SESSIONS','CAMPAIGNS_AT_RISK','CAPACITY_SHORTFALL_POOLS','RECONCILIATION_BACKLOG')),
  comparison text NOT NULL CHECK (comparison IN ('GT','GTE','LT','LTE','EQ')),
  threshold numeric NOT NULL,
  severity text NOT NULL CHECK (severity IN ('INFO','WARNING','CRITICAL')),
  consecutive_evaluations integer NOT NULL DEFAULT 1 CHECK (consecutive_evaluations BETWEEN 1 AND 1000),
  cooldown_seconds integer NOT NULL DEFAULT 300 CHECK (cooldown_seconds BETWEEN 0 AND 86400),
  auto_incident boolean NOT NULL DEFAULT false,
  escalation_steps jsonb NOT NULL DEFAULT '[]'::jsonb,
  status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','RETIRED')),
  effective_from timestamptz NOT NULL,
  effective_to timestamptz,
  created_by uuid NOT NULL,
  submitted_by uuid,
  approved_by uuid,
  reason text NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  current_breach_count integer NOT NULL DEFAULT 0 CHECK (current_breach_count >= 0),
  last_evaluated_at timestamptz,
  cooldown_until timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (jsonb_typeof(escalation_steps)='array'),
  CHECK (effective_to IS NULL OR effective_to > effective_from)
);
CREATE INDEX IF NOT EXISTS operational_alert_policies_active_idx
  ON operational_alert_policies(metric,effective_from DESC)
  WHERE status='ACTIVE';

CREATE TABLE IF NOT EXISTS operational_alert_policy_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  alert_policy_id uuid NOT NULL REFERENCES operational_alert_policies(id),
  event_type text NOT NULL,
  version bigint NOT NULL,
  actor_id uuid NOT NULL,
  reason text NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS operational_alert_policy_events_policy_time_idx
  ON operational_alert_policy_events(alert_policy_id,occurred_at DESC,id DESC);

CREATE TABLE IF NOT EXISTS operational_alerts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  policy_id uuid NOT NULL REFERENCES operational_alert_policies(id),
  status text NOT NULL CHECK (status IN ('ACTIVE','ACKNOWLEDGED','RESOLVED')),
  severity text NOT NULL CHECK (severity IN ('INFO','WARNING','CRITICAL')),
  observed_value numeric NOT NULL,
  threshold numeric NOT NULL,
  occurrence_count integer NOT NULL DEFAULT 1,
  first_triggered_at timestamptz NOT NULL,
  last_observed_at timestamptz NOT NULL,
  acknowledged_by uuid,
  acknowledged_at timestamptz,
  resolved_at timestamptz,
  linked_incident_id uuid REFERENCES operations_incidents(id),
  escalation_index integer NOT NULL DEFAULT 0,
  next_escalation_at timestamptz,
  escalation_lease_owner text,
  escalation_lease_version bigint NOT NULL DEFAULT 0 CHECK (escalation_lease_version >= 0),
  escalation_lease_expires_at timestamptz,
  version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS operational_alerts_one_unresolved_per_policy_idx
  ON operational_alerts(policy_id) WHERE status IN ('ACTIVE','ACKNOWLEDGED');
CREATE INDEX IF NOT EXISTS operational_alerts_escalation_idx
  ON operational_alerts(next_escalation_at,escalation_lease_expires_at) WHERE status='ACTIVE' AND next_escalation_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS operational_alert_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  alert_id uuid NOT NULL REFERENCES operational_alerts(id),
  event_type text NOT NULL CHECK (event_type IN ('TRIGGERED','OBSERVED','ACKNOWLEDGED','ESCALATED','RESOLVED','INCIDENT_CREATED')),
  actor_id uuid,
  observed_value numeric,
  detail text NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS operational_alert_events_alert_time_idx
  ON operational_alert_events(alert_id,occurred_at ASC,id ASC);

CREATE TABLE IF NOT EXISTS operational_notifications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  alert_id uuid REFERENCES operational_alerts(id),
  incident_id uuid REFERENCES operations_incidents(id),
  channel text NOT NULL CHECK (channel IN ('PORTAL','EMAIL','WEBHOOK')),
  target text NOT NULL,
  status text NOT NULL CHECK (status IN ('PENDING','DELIVERED','FAILED','CANCELLED')),
  payload jsonb NOT NULL,
  attempt_count integer NOT NULL DEFAULT 0,
  delivered_at timestamptz,
  last_error_code text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS operational_notifications_pending_idx
  ON operational_notifications(status,created_at) WHERE status='PENDING';
CREATE INDEX IF NOT EXISTS operational_notifications_inbox_idx
  ON operational_notifications(channel,target,created_at DESC,id DESC)
  WHERE status='DELIVERED';

CREATE OR REPLACE FUNCTION prevent_platform_governance_event_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'platform governance evidence is append-only';
END;
$$;

DO $$
DECLARE table_name text;
BEGIN
  FOREACH table_name IN ARRAY ARRAY[
    'gateway_runtime_events','platform_configuration_events','maintenance_window_events',
    'retention_policy_events','operational_incident_events','operational_alert_policy_events',
    'operational_alert_events'
  ] LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS trg_%I_append_only ON %I',table_name,table_name);
    EXECUTE format('CREATE TRIGGER trg_%I_append_only BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION prevent_platform_governance_event_mutation()',table_name,table_name);
  END LOOP;
END $$;

COMMIT;
