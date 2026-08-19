BEGIN;

-- HELD/ACTIVE reservations are live dispatch-capacity authority. The retained
-- release tombstone trigger previously allowed those rows to be deleted before
-- governed release, which could erase a live fence and reopen overbooking.
CREATE OR REPLACE FUNCTION retain_reservation_release_on_delete()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF OLD.status IN ('HELD','ACTIVE') THEN
        RAISE EXCEPTION 'live capacity reservation authority cannot be deleted before release'
            USING ERRCODE='55000';
    END IF;
    IF OLD.status='RELEASED' THEN
        INSERT INTO public.campaign_pool_capacity_reservation_release_tombstones(
            reservation_id,campaign_id,routing_plan_id,sender_pool_id,
            fencing_version,released_by,released_at
        ) VALUES (
            OLD.id,OLD.campaign_id,OLD.routing_plan_id,OLD.sender_pool_id,
            OLD.fencing_version,OLD.released_by,OLD.released_at
        );
    END IF;
    RETURN OLD;
END;
$$;

-- Migration-unique readiness evidence prevents an 0088 schema from appearing
-- current when the DELETE guard semantics are still the older fail-open form.
INSERT INTO platform_schema_capabilities(capability,source_migration,evidence_version)
VALUES ('R17_LIVE_CAPACITY_DELETE_HARDENING',89,1);

COMMIT;
