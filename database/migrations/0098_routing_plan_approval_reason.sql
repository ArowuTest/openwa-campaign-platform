BEGIN;

ALTER TABLE campaign_routing_plans
  ADD COLUMN IF NOT EXISTS approval_reason text NULL,
  ADD COLUMN IF NOT EXISTS approved_campaign_version bigint NULL;

ALTER TABLE campaign_routing_plans
  DROP CONSTRAINT IF EXISTS campaign_routing_plans_approval_reason_check;
ALTER TABLE campaign_routing_plans
  ADD CONSTRAINT campaign_routing_plans_approval_reason_check
  CHECK (approval_reason IS NULL OR length(btrim(approval_reason)) BETWEEN 8 AND 1000);

ALTER TABLE campaign_routing_plans
  DROP CONSTRAINT IF EXISTS campaign_routing_plans_approved_campaign_version_check;
ALTER TABLE campaign_routing_plans
  ADD CONSTRAINT campaign_routing_plans_approved_campaign_version_check
  CHECK (approved_campaign_version IS NULL OR approved_campaign_version > 0);

-- Historical routing plans predate frozen campaign-version approval evidence.
-- Do not fabricate approval reasons or campaign versions for legacy rows.

COMMIT;
