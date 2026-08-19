BEGIN;

CREATE TABLE IF NOT EXISTS meta_cloud_conversation_window_quarantine (
    meta_sender_id uuid NOT NULL,
    contact_id uuid NOT NULL,
    source_provider_message_id text NOT NULL,
    inbound_occurred_at timestamptz NOT NULL,
    eligible_until timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    reason text NOT NULL,
    quarantined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(meta_sender_id,source_provider_message_id)
);

INSERT INTO meta_cloud_conversation_window_quarantine(
    meta_sender_id,contact_id,source_provider_message_id,inbound_occurred_at,
    eligible_until,recorded_at,reason
)
SELECT meta_sender_id,contact_id,source_provider_message_id,inbound_occurred_at,
       eligible_until,recorded_at,'ELIGIBILITY_WINDOW_EXCEEDS_MAXIMUM'
FROM meta_cloud_conversation_windows
WHERE eligible_until > inbound_occurred_at + interval '24 hours'
ON CONFLICT DO NOTHING;

DELETE FROM meta_cloud_conversation_windows
WHERE eligible_until > inbound_occurred_at + interval '24 hours';
ALTER TABLE meta_cloud_conversation_windows
    ADD CONSTRAINT meta_cloud_conversation_windows_max_duration_check
    CHECK (eligible_until <= inbound_occurred_at + interval '24 hours');

CREATE TABLE meta_cloud_conversation_window_tombstones (
    meta_sender_id uuid NOT NULL,
    contact_id uuid NOT NULL,
    source_provider_message_id text NOT NULL,
    inbound_occurred_at timestamptz NOT NULL,
    eligible_until timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    deleted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(meta_sender_id,source_provider_message_id)
);

CREATE OR REPLACE FUNCTION protect_meta_conversation_window_evidence()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, public AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        INSERT INTO public.meta_cloud_conversation_window_tombstones(meta_sender_id,contact_id,source_provider_message_id,inbound_occurred_at,eligible_until,recorded_at)
        VALUES(OLD.meta_sender_id,OLD.contact_id,OLD.source_provider_message_id,OLD.inbound_occurred_at,OLD.eligible_until,OLD.recorded_at) ON CONFLICT DO NOTHING;
        RETURN OLD;
    END IF;
    IF EXISTS (SELECT 1 FROM public.meta_cloud_conversation_window_tombstones WHERE meta_sender_id=NEW.meta_sender_id AND source_provider_message_id=NEW.source_provider_message_id) THEN
        RAISE EXCEPTION 'Meta conversation-window provider evidence key was previously retained' USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END; $$;

CREATE TRIGGER trg_meta_conversation_window_delete_tombstone BEFORE DELETE ON meta_cloud_conversation_windows FOR EACH ROW EXECUTE FUNCTION protect_meta_conversation_window_evidence();
CREATE TRIGGER trg_meta_conversation_window_insert_tombstone_guard BEFORE INSERT ON meta_cloud_conversation_windows FOR EACH ROW EXECUTE FUNCTION protect_meta_conversation_window_evidence();

ALTER TABLE campaign_pool_capacity_reservations
    ADD COLUMN released_by text,
    ADD COLUMN released_at timestamptz;

COMMIT;