BEGIN;

ALTER TABLE export_requests
  ADD COLUMN IF NOT EXISTS authentication_event_as_of timestamptz,
  ADD COLUMN IF NOT EXISTS authentication_event_head_id uuid;

CREATE INDEX IF NOT EXISTS export_requests_authentication_event_watermark_idx
  ON export_requests(authentication_event_as_of, authentication_event_head_id)
  WHERE authentication_event_as_of IS NOT NULL;

-- Existing installations may apply migrations without rerunning the role
-- bootstrap. Reconcile the export worker's least-privilege read boundary here
-- as well; the conditional guard keeps isolated migration replay safe when the
-- service role has not yet been provisioned.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'campaign_export_worker') THEN
    REVOKE ALL PRIVILEGES ON TABLE authentication_events FROM campaign_export_worker;
    GRANT SELECT (id,user_id,event_type,correlation_id,occurred_at)
      ON TABLE authentication_events TO campaign_export_worker;
  END IF;
END $$;

COMMIT;
