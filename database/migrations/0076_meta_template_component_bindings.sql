BEGIN;

ALTER TABLE meta_cloud_message_bindings
    ADD COLUMN component_bindings jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT meta_cloud_message_bindings_component_bindings_check
        CHECK (jsonb_typeof(component_bindings) = 'array');

COMMIT;
