BEGIN;

-- Explicit Meta customer-service-window evidence. Rows are provider-event
-- observations, not inferred activity. Keeping prior observations preserves
-- replay/conflict evidence even after a newer inbound message extends the
-- current eligible window for the same sender/contact pair.
CREATE TABLE meta_cloud_conversation_windows (
    meta_sender_id uuid NOT NULL REFERENCES meta_cloud_senders(id) ON DELETE RESTRICT,
    contact_id uuid NOT NULL REFERENCES contacts(id) ON DELETE RESTRICT,
    source_provider_message_id text NOT NULL,
    inbound_occurred_at timestamptz NOT NULL,
    eligible_until timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    evidence_version bigint NOT NULL DEFAULT 1 CHECK (evidence_version = 1),
    PRIMARY KEY (meta_sender_id, source_provider_message_id),
    CHECK (length(btrim(source_provider_message_id)) BETWEEN 1 AND 512),
    CHECK (eligible_until > inbound_occurred_at)
);

CREATE INDEX idx_meta_conversation_window_current
    ON meta_cloud_conversation_windows(
        meta_sender_id, contact_id, inbound_occurred_at DESC,
        source_provider_message_id DESC
    );

CREATE OR REPLACE FUNCTION prevent_meta_conversation_window_update()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    RAISE EXCEPTION 'Meta conversation-window evidence is immutable while retained'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER trg_meta_conversation_window_immutable
BEFORE UPDATE ON meta_cloud_conversation_windows
FOR EACH ROW EXECUTE FUNCTION prevent_meta_conversation_window_update();

COMMIT;
