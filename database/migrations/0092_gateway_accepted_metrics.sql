BEGIN;

ALTER TABLE campaign_metrics
  ADD COLUMN IF NOT EXISTS gateway_accepted_total bigint NOT NULL DEFAULT 0;

UPDATE campaign_metrics cm
SET submitted_total = (
      SELECT count(*)::bigint
      FROM campaign_recipients cr
      WHERE cr.campaign_id=cm.campaign_id AND cr.status='SUBMITTING'
    ),
    gateway_accepted_total = (
      SELECT count(*)::bigint
      FROM campaign_recipients cr
      WHERE cr.campaign_id=cm.campaign_id AND cr.status='GATEWAY_ACCEPTED'
    ),
    updated_at = now();

COMMIT;
