BEGIN;

-- Give the 0085 frozen-authority semantics a migration-unique schema object so
-- readiness never has to infer the latest contract from a reused constraint name.
ALTER TABLE campaigns
    DROP CONSTRAINT IF EXISTS campaigns_frozen_authority_v87_check,
    ADD CONSTRAINT campaigns_frozen_authority_v87_check CHECK (
        (provider_capability_definition_id IS NULL
         AND provider_capability_definition_version IS NULL
         AND gateway_pool_version IS NULL
         AND coalesce(btrim(provider_adapter_version),'')='')
        OR
        (provider_capability_definition_id IS NOT NULL
         AND provider_capability_definition_version IS NOT NULL
         AND provider_capability_definition_version > 0
         AND coalesce(btrim(provider_adapter_version),'')<>''
         AND transport_provider IS NOT NULL
         AND ((transport_provider='OPENWA' AND gateway_pool_version IS NOT NULL AND gateway_pool_version > 0)
              OR (transport_provider='META' AND gateway_pool_version IS NULL)))
    );

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM platform_schema_capabilities
                   WHERE capability='CAMPAIGN_FROZEN_EVIDENCE_NULL_HARDENING'
                     AND source_migration=85 AND evidence_version=1) THEN
        RAISE EXCEPTION '0085 frozen-evidence capability marker is missing' USING ERRCODE='55000';
    END IF;
END $$;

-- Re-pin the 0086 evidence guard to the same deterministic function search path
-- used by the other retained-evidence triggers.
CREATE OR REPLACE FUNCTION prevent_platform_schema_capability_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    RAISE EXCEPTION 'platform schema capability evidence is immutable'
        USING ERRCODE='55000';
END;
$$;
INSERT INTO platform_schema_capabilities(capability,source_migration,evidence_version)
VALUES ('CONTROL_SCHEMA_READINESS_HARDENING',87,1);

-- Row triggers do not fire for TRUNCATE. Preserve the capability ledger as
-- immutable schema evidence under maintenance commands as well.
DROP TRIGGER IF EXISTS trg_platform_schema_capability_no_truncate ON platform_schema_capabilities;
CREATE TRIGGER trg_platform_schema_capability_no_truncate
BEFORE TRUNCATE ON platform_schema_capabilities
FOR EACH STATEMENT EXECUTE FUNCTION prevent_platform_schema_capability_mutation();

CREATE TABLE campaign_pool_capacity_reservation_release_tombstones (
    reservation_id uuid PRIMARY KEY,
    campaign_id uuid NOT NULL,
    routing_plan_id uuid NOT NULL,
    sender_pool_id uuid NOT NULL,
    fencing_version bigint NOT NULL CHECK (fencing_version > 0),
    released_by text,
    released_at timestamptz NOT NULL,
    retained_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION retain_reservation_release_on_delete()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, public AS $$
BEGIN
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

CREATE TRIGGER trg_reservation_release_delete_tombstone
BEFORE DELETE ON campaign_pool_capacity_reservations
FOR EACH ROW EXECUTE FUNCTION retain_reservation_release_on_delete();

CREATE TRIGGER trg_reservation_release_tombstone_immutable
BEFORE UPDATE OR DELETE ON campaign_pool_capacity_reservation_release_tombstones
FOR EACH ROW EXECUTE FUNCTION prevent_retained_evidence_mutation();
-- Row-level evidence triggers do not execute for TRUNCATE. Authority/evidence
-- tables therefore fail closed on bulk truncation as well as row mutation.
CREATE TRIGGER trg_meta_conversation_window_no_truncate
BEFORE TRUNCATE ON meta_cloud_conversation_windows
FOR EACH STATEMENT EXECUTE FUNCTION prevent_retained_evidence_mutation();
CREATE TRIGGER trg_meta_window_tombstone_no_truncate
BEFORE TRUNCATE ON meta_cloud_conversation_window_tombstones
FOR EACH STATEMENT EXECUTE FUNCTION prevent_retained_evidence_mutation();
CREATE TRIGGER trg_meta_window_quarantine_no_truncate
BEFORE TRUNCATE ON meta_cloud_conversation_window_quarantine
FOR EACH STATEMENT EXECUTE FUNCTION prevent_retained_evidence_mutation();
CREATE TRIGGER trg_campaign_route_quarantine_evidence_no_truncate
BEFORE TRUNCATE ON campaign_routing_plan_quarantine_evidence
FOR EACH STATEMENT EXECUTE FUNCTION prevent_retained_evidence_mutation();
CREATE TRIGGER trg_campaign_quarantined_route_no_truncate
BEFORE TRUNCATE ON campaign_routing_plan_quarantined_routes
FOR EACH STATEMENT EXECUTE FUNCTION prevent_retained_evidence_mutation();
CREATE TRIGGER trg_reservation_release_tombstone_no_truncate
BEFORE TRUNCATE ON campaign_pool_capacity_reservation_release_tombstones
FOR EACH STATEMENT EXECUTE FUNCTION prevent_retained_evidence_mutation();

COMMIT;
