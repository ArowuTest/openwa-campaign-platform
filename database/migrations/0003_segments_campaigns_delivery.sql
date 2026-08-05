BEGIN;

CREATE TABLE segments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  description text,
  organisation_id uuid REFERENCES organisations(id),
  definition jsonb NOT NULL,
  definition_version integer NOT NULL DEFAULT 1,
  status text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('DRAFT','ACTIVE','ARCHIVED')),
  created_by uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE campaigns (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organisation_id uuid NOT NULL REFERENCES organisations(id),
  name text NOT NULL,
  purpose_id uuid NOT NULL REFERENCES consent_purposes(id),
  status text NOT NULL CHECK (status IN ('DRAFT','CONSENT_REVIEW_PENDING','CONSENT_APPROVED','AUDIENCE_BUILDING','AUDIENCE_VALIDATED','MESSAGE_REVIEW_PENDING','MESSAGE_APPROVED','COMMERCIAL_APPROVED','FINAL_APPROVAL_PENDING','SCHEDULED','DISPATCHING','PAUSED','COMPLETED','COMPLETED_WITH_EXCEPTIONS','CANCELLED')),
  requested_start_at timestamptz,
  completion_deadline_at timestamptz,
  maximum_unique_recipients bigint,
  maximum_messages_per_recipient integer NOT NULL DEFAULT 1,
  created_by uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE message_versions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_id uuid NOT NULL REFERENCES campaigns(id),
  version integer NOT NULL,
  message_type text NOT NULL,
  body text,
  media_object_key text,
  destination_links jsonb NOT NULL DEFAULT '[]'::jsonb,
  content_hash text NOT NULL,
  status text NOT NULL CHECK (status IN ('DRAFT','APPROVED','SUPERSEDED')),
  approved_by uuid,
  approved_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (campaign_id, version)
);

CREATE TABLE audience_snapshots (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_id uuid NOT NULL REFERENCES campaigns(id),
  segment_id uuid REFERENCES segments(id),
  segment_definition jsonb NOT NULL,
  consent_policy_version text NOT NULL,
  snapshot_hash text NOT NULL,
  eligible_count bigint NOT NULL,
  created_by uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (campaign_id, snapshot_hash)
);

CREATE TABLE audience_snapshot_members (
  snapshot_id uuid NOT NULL REFERENCES audience_snapshots(id) ON DELETE CASCADE,
  contact_id uuid NOT NULL REFERENCES contacts(id),
  eligibility_evidence jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (snapshot_id, contact_id)
);

CREATE TABLE sender_nodes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL UNIQUE,
  public_ip inet,
  status text NOT NULL CHECK (status IN ('READY','DRAINING','UNHEALTHY','OFFLINE')),
  last_heartbeat_at timestamptz,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sender_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id uuid REFERENCES sender_nodes(id),
  logical_sender_pool text,
  encrypted_msisdn bytea NOT NULL,
  masked_msisdn text NOT NULL,
  engine_type text NOT NULL,
  status text NOT NULL CHECK (status IN ('NEW','PAIRING','READY','BUSY','PAUSED','DISCONNECTED','RESTRICTED','RETIRED')),
  safe_messages_per_minute numeric,
  safe_daily_capacity bigint,
  last_success_at timestamptz,
  last_heartbeat_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE campaign_recipients (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_id uuid NOT NULL REFERENCES campaigns(id),
  snapshot_id uuid NOT NULL REFERENCES audience_snapshots(id),
  contact_id uuid NOT NULL REFERENCES contacts(id),
  message_version_id uuid NOT NULL REFERENCES message_versions(id),
  idempotency_key text NOT NULL UNIQUE,
  status text NOT NULL CHECK (status IN ('AUTHORISED','QUEUED','CLAIMED','SUBMITTING','GATEWAY_ACCEPTED','SENT','DELIVERED','READ','FAILED_RETRYABLE','FAILED_PERMANENT','UNKNOWN','SUPPRESSED_BEFORE_SEND','CANCELLED')),
  assigned_session_id uuid REFERENCES sender_sessions(id),
  provider_message_id text,
  attempt_count integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz,
  last_error_code text,
  last_error_detail text,
  authorised_at timestamptz NOT NULL DEFAULT now(),
  submitted_at timestamptz,
  completed_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (campaign_id, contact_id, message_version_id)
);

CREATE INDEX idx_campaign_recipients_dispatch ON campaign_recipients(status, next_attempt_at, campaign_id);
CREATE INDEX idx_campaign_recipients_campaign_status ON campaign_recipients(campaign_id, status);
CREATE INDEX idx_campaign_recipients_provider_message ON campaign_recipients(provider_message_id) WHERE provider_message_id IS NOT NULL;

CREATE TABLE transactional_outbox (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  aggregate_type text NOT NULL,
  aggregate_id uuid NOT NULL,
  event_type text NOT NULL,
  payload jsonb NOT NULL,
  status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','PUBLISHED','FAILED')),
  available_at timestamptz NOT NULL DEFAULT now(),
  published_at timestamptz,
  attempt_count integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_transactional_outbox_pending ON transactional_outbox(status, available_at) WHERE status = 'PENDING';

CREATE TABLE delivery_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_recipient_id uuid NOT NULL REFERENCES campaign_recipients(id),
  provider_event_id text,
  event_type text NOT NULL,
  occurred_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  UNIQUE (provider_event_id, event_type)
);

CREATE INDEX idx_delivery_events_recipient_time ON delivery_events(campaign_recipient_id, occurred_at);

CREATE TABLE campaign_metrics (
  campaign_id uuid PRIMARY KEY REFERENCES campaigns(id),
  authorised_total bigint NOT NULL DEFAULT 0,
  queued_total bigint NOT NULL DEFAULT 0,
  submitted_total bigint NOT NULL DEFAULT 0,
  sent_total bigint NOT NULL DEFAULT 0,
  delivered_total bigint NOT NULL DEFAULT 0,
  read_total bigint NOT NULL DEFAULT 0,
  failed_total bigint NOT NULL DEFAULT 0,
  unknown_total bigint NOT NULL DEFAULT 0,
  suppressed_total bigint NOT NULL DEFAULT 0,
  opt_out_total bigint NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now()
);

COMMIT;
