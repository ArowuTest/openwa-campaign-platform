BEGIN;

-- Keyset pagination for the permission-gated authentication-event audit view.
CREATE INDEX IF NOT EXISTS idx_authentication_events_occurred_id
  ON authentication_events(occurred_at DESC, id DESC);

COMMIT;
