BEGIN;

-- Retained evidence is an authority fence, not mutable working state. Once a
-- key has been tombstoned or quarantined, SQL maintenance bugs must not be able
-- to erase that fact and make the provider key eligible for live reuse.
CREATE OR REPLACE FUNCTION prevent_retained_evidence_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    RAISE EXCEPTION 'retained governance evidence is immutable'
        USING ERRCODE='55000';
END;
$$;

DROP TRIGGER IF EXISTS trg_meta_conversation_window_tombstone_immutable
    ON meta_cloud_conversation_window_tombstones;
CREATE TRIGGER trg_meta_conversation_window_tombstone_immutable
BEFORE UPDATE OR DELETE ON meta_cloud_conversation_window_tombstones
FOR EACH ROW EXECUTE FUNCTION prevent_retained_evidence_mutation();

DROP TRIGGER IF EXISTS trg_meta_conversation_window_quarantine_immutable
    ON meta_cloud_conversation_window_quarantine;
CREATE TRIGGER trg_meta_conversation_window_quarantine_immutable
BEFORE UPDATE OR DELETE ON meta_cloud_conversation_window_quarantine
FOR EACH ROW EXECUTE FUNCTION prevent_retained_evidence_mutation();

-- The route quarantine tables preserve the exact evidence that justified
-- fail-closing an approved plan and are also used for sender-bound webhook
-- correlation after the live route is removed. Preserve them append-only.
DROP TRIGGER IF EXISTS trg_campaign_route_quarantine_evidence_immutable
    ON campaign_routing_plan_quarantine_evidence;
CREATE TRIGGER trg_campaign_route_quarantine_evidence_immutable
BEFORE UPDATE OR DELETE ON campaign_routing_plan_quarantine_evidence
FOR EACH ROW EXECUTE FUNCTION prevent_retained_evidence_mutation();

DROP TRIGGER IF EXISTS trg_campaign_quarantined_route_immutable
    ON campaign_routing_plan_quarantined_routes;
CREATE TRIGGER trg_campaign_quarantined_route_immutable
BEFORE UPDATE OR DELETE ON campaign_routing_plan_quarantined_routes
FOR EACH ROW EXECUTE FUNCTION prevent_retained_evidence_mutation();

-- A quarantined Meta provider-message key is retained evidence just like a
-- tombstoned key. Neither may ever be inserted back into live FREE_FORM
-- authority under the same sender/provider-message identity.
CREATE OR REPLACE FUNCTION protect_meta_conversation_window_evidence()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        INSERT INTO public.meta_cloud_conversation_window_tombstones(
            meta_sender_id,contact_id,source_provider_message_id,
            inbound_occurred_at,eligible_until,recorded_at
        )
        VALUES(
            OLD.meta_sender_id,OLD.contact_id,OLD.source_provider_message_id,
            OLD.inbound_occurred_at,OLD.eligible_until,OLD.recorded_at
        )
        ON CONFLICT DO NOTHING;
        RETURN OLD;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM public.meta_cloud_conversation_window_tombstones retained
        WHERE retained.meta_sender_id=NEW.meta_sender_id
          AND retained.source_provider_message_id=NEW.source_provider_message_id
    ) OR EXISTS (
        SELECT 1
        FROM public.meta_cloud_conversation_window_quarantine retained
        WHERE retained.meta_sender_id=NEW.meta_sender_id
          AND retained.source_provider_message_id=NEW.source_provider_message_id
    ) THEN
        RAISE EXCEPTION 'Meta conversation-window provider evidence key was previously retained'
            USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;

-- Bound provider event chronology to the platform's established signed-evidence
-- clock-skew tolerance. Preserve any pre-existing impossible observation in the
-- immutable quarantine before removing it from live FREE_FORM authority.
INSERT INTO meta_cloud_conversation_window_quarantine(
    meta_sender_id,contact_id,source_provider_message_id,inbound_occurred_at,
    eligible_until,recorded_at,reason
)
SELECT meta_sender_id,contact_id,source_provider_message_id,inbound_occurred_at,
       eligible_until,recorded_at,'INBOUND_TIMESTAMP_EXCEEDS_RECEIPT_SKEW'
FROM meta_cloud_conversation_windows
WHERE inbound_occurred_at > recorded_at + interval '5 minutes'
ON CONFLICT DO NOTHING;

DELETE FROM meta_cloud_conversation_windows
WHERE inbound_occurred_at > recorded_at + interval '5 minutes';

ALTER TABLE meta_cloud_conversation_windows
    ADD CONSTRAINT meta_cloud_conversation_windows_receipt_skew_check
    CHECK (inbound_occurred_at <= recorded_at + interval '5 minutes');
-- Release evidence is durable audit state. Historical hardening migrations did
-- not know an actor, so preserve that uncertainty while recovering the time
-- from the reservation's already-persisted updated_at timestamp.
UPDATE campaign_pool_capacity_reservations
SET released_at=updated_at
WHERE status='RELEASED' AND released_at IS NULL;

ALTER TABLE campaign_pool_capacity_reservations
    ADD CONSTRAINT campaign_pool_capacity_reservations_release_evidence_check
    CHECK (
        (status='RELEASED' AND released_at IS NOT NULL)
        OR
        (status<>'RELEASED' AND released_by IS NULL AND released_at IS NULL)
    );

CREATE OR REPLACE FUNCTION protect_reservation_release_evidence()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF OLD.status='RELEASED' AND (
        NEW.released_by IS DISTINCT FROM OLD.released_by
        OR NEW.released_at IS DISTINCT FROM OLD.released_at
    ) THEN
        RAISE EXCEPTION 'reservation release evidence is immutable'
            USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_reservation_release_evidence_immutable
BEFORE UPDATE ON campaign_pool_capacity_reservations
FOR EACH ROW EXECUTE FUNCTION protect_reservation_release_evidence();
COMMIT;
