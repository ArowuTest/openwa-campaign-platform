BEGIN;

-- Runtime readiness needs a durable capability boundary rather than inferring the
-- latest schema from constraint names that can be reused by later hardening.
-- Ordered migration execution means this marker can only commit after 0085.
CREATE TABLE platform_schema_capabilities (
    capability TEXT PRIMARY KEY,
    source_migration INTEGER NOT NULL CHECK (source_migration > 0),
    evidence_version INTEGER NOT NULL CHECK (evidence_version > 0),
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT platform_schema_capabilities_name_check CHECK (btrim(capability) <> '')
);

INSERT INTO platform_schema_capabilities(capability,source_migration,evidence_version)
VALUES ('CAMPAIGN_FROZEN_EVIDENCE_NULL_HARDENING',85,1);

CREATE OR REPLACE FUNCTION prevent_platform_schema_capability_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'platform schema capability evidence is immutable'
        USING ERRCODE='55000';
END;
$$;

CREATE TRIGGER trg_platform_schema_capability_immutable
BEFORE UPDATE OR DELETE ON platform_schema_capabilities
FOR EACH ROW EXECUTE FUNCTION prevent_platform_schema_capability_mutation();

COMMIT;
