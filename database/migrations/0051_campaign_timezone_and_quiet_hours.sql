BEGIN;

ALTER TABLE campaigns
  ADD COLUMN IF NOT EXISTS campaign_timezone text NOT NULL DEFAULT 'UTC',
  ADD COLUMN IF NOT EXISTS quiet_hours_start text,
  ADD COLUMN IF NOT EXISTS quiet_hours_end text;

ALTER TABLE campaigns
  DROP CONSTRAINT IF EXISTS campaigns_timezone_nonempty,
  DROP CONSTRAINT IF EXISTS campaigns_quiet_hours_pair,
  DROP CONSTRAINT IF EXISTS campaigns_quiet_hours_format,
  DROP CONSTRAINT IF EXISTS campaigns_quiet_hours_distinct;

ALTER TABLE campaigns
  ADD CONSTRAINT campaigns_timezone_nonempty CHECK (btrim(campaign_timezone) <> ''),
  ADD CONSTRAINT campaigns_quiet_hours_pair CHECK ((quiet_hours_start IS NULL) = (quiet_hours_end IS NULL)),
  ADD CONSTRAINT campaigns_quiet_hours_format CHECK (
    (quiet_hours_start IS NULL AND quiet_hours_end IS NULL)
    OR (quiet_hours_start ~ '^(?:[01][0-9]|2[0-3]):[0-5][0-9]$' AND quiet_hours_end ~ '^(?:[01][0-9]|2[0-3]):[0-5][0-9]$')
  ),
  ADD CONSTRAINT campaigns_quiet_hours_distinct CHECK (quiet_hours_start IS NULL OR quiet_hours_start <> quiet_hours_end);

CREATE INDEX IF NOT EXISTS idx_campaigns_dispatch_window
  ON campaigns(status, requested_start_at, completion_deadline_at)
  WHERE status IN ('SCHEDULED','DISPATCHING');

COMMIT;
