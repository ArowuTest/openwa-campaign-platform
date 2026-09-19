BEGIN;

CREATE TABLE unmatched_delivery_events (
    provider_event_id text PRIMARY KEY,
    provider_message_id text,
    client_reference text,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','RESOLVED')),
    resolved_at timestamptz
);

CREATE INDEX idx_unmatched_delivery_events_pending
ON unmatched_delivery_events(received_at,provider_event_id)
WHERE status='PENDING';

COMMIT;
