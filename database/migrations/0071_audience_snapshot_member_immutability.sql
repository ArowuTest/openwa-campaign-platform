BEGIN;

CREATE OR REPLACE FUNCTION prevent_audience_snapshot_member_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audience snapshot membership is immutable after creation';
END;
$$;

DROP TRIGGER IF EXISTS trg_audience_snapshot_members_no_mutation ON audience_snapshot_members;
CREATE TRIGGER trg_audience_snapshot_members_no_mutation
BEFORE UPDATE OR DELETE ON audience_snapshot_members
FOR EACH ROW EXECUTE FUNCTION prevent_audience_snapshot_member_mutation();

CREATE OR REPLACE FUNCTION enforce_audience_snapshot_member_capacity()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    expected_count bigint;
    current_count bigint;
BEGIN
    SELECT eligible_count INTO expected_count
    FROM audience_snapshots
    WHERE id = NEW.snapshot_id
    FOR UPDATE;

    SELECT count(*) INTO current_count
    FROM audience_snapshot_members
    WHERE snapshot_id = NEW.snapshot_id;

    IF current_count > expected_count THEN
        RAISE EXCEPTION 'audience snapshot member count % exceeds frozen eligible count %', current_count, expected_count;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_audience_snapshot_members_capacity ON audience_snapshot_members;
CREATE TRIGGER trg_audience_snapshot_members_capacity
AFTER INSERT ON audience_snapshot_members
FOR EACH ROW EXECUTE FUNCTION enforce_audience_snapshot_member_capacity();

COMMIT;
