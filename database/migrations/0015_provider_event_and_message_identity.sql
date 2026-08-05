BEGIN;

-- Provider callback identity and provider message correlation are different
-- concepts. Earlier application code placed the message ID in provider_event_id;
-- preserve that value as message evidence and stop overloading the event column.
ALTER TABLE delivery_events
  ADD COLUMN IF NOT EXISTS provider_message_id text;

UPDATE delivery_events
SET provider_message_id=provider_event_id
WHERE provider_message_id IS NULL
  AND provider_event_id IS NOT NULL
  AND event_deduplication_key IS NOT NULL;

UPDATE delivery_events
SET provider_event_id=NULL
WHERE provider_event_id IS NOT NULL
  AND event_deduplication_key IS NOT NULL;

DROP INDEX IF EXISTS idx_delivery_events_provider_message_time;
CREATE INDEX IF NOT EXISTS idx_delivery_events_provider_message_time
  ON delivery_events(provider_message_id,occurred_at DESC,id)
  WHERE provider_message_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_delivery_events_provider_event_received
  ON delivery_events(provider_event_id,received_at DESC,id)
  WHERE provider_event_id IS NOT NULL;

COMMIT;
