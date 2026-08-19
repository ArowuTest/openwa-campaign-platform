BEGIN;

-- Live HELD/ACTIVE reservations are dispatch-capacity authority. Row-level
-- release/tombstone guards do not fire for TRUNCATE, so bulk maintenance must
-- fail closed rather than silently erasing active capacity fences.
DROP TRIGGER IF EXISTS trg_capacity_reservation_no_truncate ON campaign_pool_capacity_reservations;
CREATE TRIGGER trg_capacity_reservation_no_truncate
BEFORE TRUNCATE ON campaign_pool_capacity_reservations
FOR EACH STATEMENT EXECUTE FUNCTION prevent_retained_evidence_mutation();

-- Migration-unique readiness evidence prevents an older 0087-class schema
-- from looking current merely because similarly named objects exist.
INSERT INTO platform_schema_capabilities(capability,source_migration,evidence_version)
VALUES ('R14_LIVE_CAPACITY_AUTHORITY_HARDENING',88,1);

COMMIT;
