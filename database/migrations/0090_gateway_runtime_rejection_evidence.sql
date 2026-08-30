BEGIN;

-- Runtime reports are authenticated before rejection evidence is appended. A
-- node can legitimately be unassigned while reporting a nonexistent or
-- otherwise unauthorized pool, so the immutable REJECTED event must survive
-- without laundering the declared pool into a governed relationship.
ALTER TABLE gateway_runtime_events
  ALTER COLUMN gateway_pool_id DROP NOT NULL;

ALTER TABLE gateway_runtime_events
  DROP CONSTRAINT IF EXISTS gateway_runtime_events_pool_anchor_check;

ALTER TABLE gateway_runtime_events
  ADD CONSTRAINT gateway_runtime_events_pool_anchor_check
  CHECK (event_type = 'REJECTED' OR gateway_pool_id IS NOT NULL);

COMMIT;
