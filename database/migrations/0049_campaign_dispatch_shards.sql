BEGIN;

CREATE TABLE IF NOT EXISTS campaign_dispatch_shards (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_id uuid NOT NULL REFERENCES campaigns(id),
  ordinal integer NOT NULL CHECK (ordinal >= 0),
  target_size integer NOT NULL CHECK (target_size BETWEEN 1 AND 100000),
  recipient_count bigint NOT NULL DEFAULT 0 CHECK (recipient_count >= 0),
  terminal_count bigint NOT NULL DEFAULT 0 CHECK (terminal_count >= 0 AND terminal_count <= recipient_count),
  failed_count bigint NOT NULL DEFAULT 0 CHECK (failed_count >= 0 AND failed_count <= terminal_count),
  unknown_count bigint NOT NULL DEFAULT 0 CHECK (unknown_count >= 0 AND unknown_count <= terminal_count),
  status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','RUNNING','COMPLETED','COMPLETED_WITH_EXCEPTIONS')),
  last_recipient_id uuid,
  lease_owner text,
  lease_version bigint NOT NULL DEFAULT 0 CHECK (lease_version >= 0),
  lease_expires_at timestamptz,
  started_at timestamptz,
  completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(campaign_id, ordinal),
  CHECK ((status='RUNNING') = (lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)),
  CHECK ((status IN ('COMPLETED','COMPLETED_WITH_EXCEPTIONS')) = (completed_at IS NOT NULL))
);

ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS dispatch_shard_id uuid REFERENCES campaign_dispatch_shards(id);
CREATE INDEX IF NOT EXISTS campaign_dispatch_shards_claim_idx ON campaign_dispatch_shards(status, lease_expires_at, campaign_id, ordinal);
CREATE INDEX IF NOT EXISTS campaign_recipients_shard_status_idx ON campaign_recipients(dispatch_shard_id, status, id) WHERE dispatch_shard_id IS NOT NULL;

CREATE OR REPLACE FUNCTION prevent_campaign_shard_identity_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.campaign_id <> OLD.campaign_id OR NEW.ordinal <> OLD.ordinal OR NEW.target_size <> OLD.target_size THEN
    RAISE EXCEPTION 'campaign dispatch shard identity is immutable';
  END IF;
  RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS trg_campaign_shard_identity_immutable ON campaign_dispatch_shards;
CREATE TRIGGER trg_campaign_shard_identity_immutable BEFORE UPDATE ON campaign_dispatch_shards FOR EACH ROW EXECUTE FUNCTION prevent_campaign_shard_identity_change();

COMMIT;
