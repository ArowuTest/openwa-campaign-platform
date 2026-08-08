BEGIN;

ALTER TABLE sender_nodes
  ADD COLUMN IF NOT EXISTS resource_health jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE sender_nodes DROP CONSTRAINT IF EXISTS sender_nodes_resource_health_shape_check;
ALTER TABLE sender_nodes ADD CONSTRAINT sender_nodes_resource_health_shape_check
  CHECK (jsonb_typeof(resource_health)='object');

-- Stable keyset support for the externally paged sender-session inventory.
CREATE INDEX IF NOT EXISTS sender_sessions_masked_msisdn_id_idx
  ON sender_sessions(masked_msisdn ASC,id ASC);

CREATE INDEX IF NOT EXISTS message_versions_campaign_version_idx
  ON message_versions(campaign_id,version DESC);

CREATE INDEX IF NOT EXISTS test_message_sends_campaign_created_id_idx
  ON test_message_sends(campaign_id,created_at DESC,id DESC);

CREATE INDEX IF NOT EXISTS audience_materialisation_jobs_campaign_requested_id_idx
  ON audience_materialisation_jobs(campaign_id,requested_at DESC,id DESC);

CREATE INDEX IF NOT EXISTS campaign_shard_reallocations_shard_created_id_idx
  ON campaign_shard_reallocations(dispatch_shard_id,created_at ASC,id ASC);

CREATE INDEX IF NOT EXISTS segments_updated_id_idx
  ON segments(updated_at DESC,id ASC);

CREATE INDEX IF NOT EXISTS segments_organisation_updated_id_idx
  ON segments(organisation_id,updated_at DESC,id ASC);

-- The pre-existing node/time index lacks the UUID tie-breaker required for
-- deterministic continuation when multiple runtime events share a timestamp.
CREATE INDEX IF NOT EXISTS gateway_runtime_events_node_time_id_idx
  ON gateway_runtime_events(node_id,occurred_at DESC,id DESC);

CREATE INDEX IF NOT EXISTS organisations_created_id_idx
  ON organisations(created_at DESC,id DESC);

COMMIT;
