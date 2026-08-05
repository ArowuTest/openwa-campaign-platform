BEGIN;

CREATE TABLE IF NOT EXISTS campaign_release_exclusions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_id uuid NOT NULL REFERENCES campaigns(id),
  snapshot_id uuid NOT NULL REFERENCES audience_snapshots(id),
  contact_id uuid NOT NULL REFERENCES contacts(id),
  eligibility_evidence_hash text NOT NULL,
  reason_code text NOT NULL,
  evaluated_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (campaign_id, snapshot_id, contact_id)
);

CREATE INDEX IF NOT EXISTS idx_campaign_release_exclusions_campaign_reason
  ON campaign_release_exclusions(campaign_id, reason_code);

ALTER TABLE campaigns
  ADD COLUMN IF NOT EXISTS sender_pool text,
  ADD COLUMN IF NOT EXISTS pause_reason text;

ALTER TABLE campaign_metrics
  ADD COLUMN IF NOT EXISTS excluded_final_check_total bigint NOT NULL DEFAULT 0;

COMMIT;
