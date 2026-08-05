BEGIN;

ALTER TABLE sender_sessions
  ADD COLUMN IF NOT EXISTS quarantined_at timestamptz,
  ADD COLUMN IF NOT EXISTS quarantine_reason text,
  ADD COLUMN IF NOT EXISTS reinstated_at timestamptz;

ALTER TABLE sender_sessions DROP CONSTRAINT IF EXISTS sender_sessions_status_check;
ALTER TABLE sender_sessions
  ADD CONSTRAINT sender_sessions_status_check
  CHECK (status IN ('NEW','PAIRING','READY','BUSY','PAUSED','DISCONNECTED','RESTRICTED','QUARANTINED','RETIRED'));

ALTER TABLE sender_sessions DROP CONSTRAINT IF EXISTS sender_sessions_quarantine_evidence_check;
ALTER TABLE sender_sessions
  ADD CONSTRAINT sender_sessions_quarantine_evidence_check CHECK (
    (status <> 'QUARANTINED') OR
    (quarantined_at IS NOT NULL AND length(trim(coalesce(quarantine_reason,''))) >= 8)
  );

CREATE INDEX IF NOT EXISTS sender_sessions_quarantine_review_idx
  ON sender_sessions(status, quarantined_at DESC)
  WHERE status='QUARANTINED';

COMMIT;
