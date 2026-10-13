BEGIN;

CREATE TABLE campaign_commercial_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  commercial_record_id uuid NOT NULL REFERENCES campaign_commercial_approvals(id),
  event_type text NOT NULL CHECK (event_type IN ('CREATED','UPDATED','SUBMITTED','APPROVED','REJECTED','REVOKED')),
  actor_id uuid NOT NULL REFERENCES internal_users(id),
  reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 5 AND 1000),
  record_version bigint NOT NULL CHECK (record_version > 0),
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(commercial_record_id,record_version)
);
CREATE INDEX campaign_commercial_events_record_time_idx
  ON campaign_commercial_events(commercial_record_id,occurred_at DESC,id DESC);

CREATE FUNCTION prevent_campaign_commercial_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'campaign commercial events are append-only'; END;
$$;
CREATE TRIGGER campaign_commercial_events_append_only
BEFORE UPDATE OR DELETE ON campaign_commercial_events
FOR EACH ROW EXECUTE FUNCTION prevent_campaign_commercial_event_mutation();

CREATE TRIGGER campaign_commercial_events_no_truncate
BEFORE TRUNCATE ON campaign_commercial_events
FOR EACH STATEMENT EXECUTE FUNCTION prevent_campaign_commercial_event_mutation();

COMMIT;
