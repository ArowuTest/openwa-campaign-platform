BEGIN;
DROP INDEX IF EXISTS uq_sender_pacing_active_scope;
ALTER TABLE sender_pacing_policies ALTER COLUMN scope_id TYPE text USING scope_id::text;
CREATE UNIQUE INDEX uq_sender_pacing_active_scope ON sender_pacing_policies(scope, coalesce(scope_id,'')) WHERE status='ACTIVE' AND effective_to IS NULL;
CREATE TABLE sender_pacing_policy_events (
    id uuid PRIMARY KEY,
    policy_id uuid NOT NULL REFERENCES sender_pacing_policies(id),
    action text NOT NULL CHECK (action IN ('CREATED','SUBMITTED','APPROVED','REJECTED','RETIRED')),
    actor_id uuid NOT NULL,
    reason text NOT NULL,
    policy_version bigint NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_sender_pacing_events_policy ON sender_pacing_policy_events(policy_id,occurred_at,id);
CREATE OR REPLACE FUNCTION prevent_sender_pacing_event_mutation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'sender pacing policy events are append-only'; END $$;
CREATE TRIGGER trg_sender_pacing_event_update BEFORE UPDATE OR DELETE ON sender_pacing_policy_events FOR EACH ROW EXECUTE FUNCTION prevent_sender_pacing_event_mutation();
COMMIT;
