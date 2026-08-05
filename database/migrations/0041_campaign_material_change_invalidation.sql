BEGIN;

CREATE TABLE campaign_material_change_events (
    id uuid PRIMARY KEY,
    campaign_id uuid NOT NULL REFERENCES campaigns(id),
    sequence bigint NOT NULL CHECK (sequence > 0),
    actor_id uuid NOT NULL,
    reason text NOT NULL CHECK (length(btrim(reason)) > 0),
    changed_fields jsonb NOT NULL CHECK (jsonb_typeof(changed_fields) = 'array' AND jsonb_array_length(changed_fields) > 0),
    previous_status text NOT NULL,
    new_status text NOT NULL,
    previous_version bigint NOT NULL CHECK (previous_version > 0),
    new_version bigint NOT NULL CHECK (new_version = previous_version + 1),
    created_at timestamptz NOT NULL,
    UNIQUE (campaign_id, sequence),
    UNIQUE (campaign_id, new_version)
);

CREATE INDEX idx_campaign_material_change_events_campaign_time
    ON campaign_material_change_events(campaign_id, created_at DESC);

CREATE OR REPLACE FUNCTION prevent_campaign_material_change_event_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'campaign material change events are append-only';
END;
$$;

CREATE TRIGGER trg_campaign_material_change_events_append_only
BEFORE UPDATE OR DELETE ON campaign_material_change_events
FOR EACH ROW EXECUTE FUNCTION prevent_campaign_material_change_event_mutation();

COMMIT;
