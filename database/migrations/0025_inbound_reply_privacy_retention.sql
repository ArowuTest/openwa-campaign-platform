BEGIN;
ALTER TABLE inbound_replies
 ADD COLUMN IF NOT EXISTS content_retain_until timestamptz,
 ADD COLUMN IF NOT EXISTS content_redacted_at timestamptz,
 ADD COLUMN IF NOT EXISTS legal_hold boolean NOT NULL DEFAULT false;
UPDATE inbound_replies SET content_retain_until=created_at+interval '90 days' WHERE content_retain_until IS NULL;
ALTER TABLE inbound_replies ALTER COLUMN content_retain_until SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_inbound_reply_retention ON inbound_replies(content_retain_until) WHERE content_redacted_at IS NULL AND legal_hold=false;
COMMIT;
