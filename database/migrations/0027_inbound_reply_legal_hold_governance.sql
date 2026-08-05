BEGIN;
ALTER TABLE inbound_replies
 ADD COLUMN IF NOT EXISTS legal_hold_reason text,
 ADD COLUMN IF NOT EXISTS legal_hold_applied_by uuid,
 ADD COLUMN IF NOT EXISTS legal_hold_applied_at timestamptz,
 ADD COLUMN IF NOT EXISTS legal_hold_released_by uuid,
 ADD COLUMN IF NOT EXISTS legal_hold_released_at timestamptz;
ALTER TABLE inbound_replies ADD CONSTRAINT inbound_reply_legal_hold_evidence_ck CHECK (
 (legal_hold=false) OR (legal_hold_reason IS NOT NULL AND legal_hold_applied_by IS NOT NULL AND legal_hold_applied_at IS NOT NULL)
) NOT VALID;
CREATE INDEX IF NOT EXISTS idx_inbound_reply_legal_hold ON inbound_replies(legal_hold,legal_hold_applied_at) WHERE legal_hold=true;
COMMIT;
