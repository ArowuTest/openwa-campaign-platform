BEGIN;
CREATE TABLE IF NOT EXISTS opt_out_policies(
 id text PRIMARY KEY,
 keywords jsonb NOT NULL CHECK(jsonb_typeof(keywords)='array' AND jsonb_array_length(keywords) BETWEEN 1 AND 100),
 status text NOT NULL CHECK(status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','RETIRED')),
 effective_from timestamptz NOT NULL,
 effective_to timestamptz,
 version bigint NOT NULL CHECK(version>0),
 created_by text NOT NULL,
 submitted_by text,
 approved_by text,
 reason text NOT NULL CHECK(length(reason)>=5),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK(effective_to IS NULL OR effective_to>effective_from),
 CHECK(approved_by IS NULL OR approved_by IS DISTINCT FROM submitted_by)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_active_opt_out_policy_window ON opt_out_policies(status) WHERE status='ACTIVE' AND effective_to IS NULL;
CREATE INDEX IF NOT EXISTS idx_opt_out_policy_effective ON opt_out_policies(status,effective_from DESC,effective_to);
CREATE TABLE IF NOT EXISTS opt_out_policy_history(
 id bigserial PRIMARY KEY, policy_id text NOT NULL REFERENCES opt_out_policies(id) ON DELETE RESTRICT,
 policy_version bigint NOT NULL, snapshot jsonb NOT NULL, changed_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(policy_id,policy_version)
);
CREATE OR REPLACE FUNCTION record_opt_out_policy_history() RETURNS trigger AS $$
BEGIN
 INSERT INTO opt_out_policy_history(policy_id,policy_version,snapshot) VALUES(NEW.id,NEW.version,to_jsonb(NEW));
 RETURN NEW;
END; $$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_opt_out_policy_history ON opt_out_policies;
CREATE TRIGGER trg_opt_out_policy_history AFTER INSERT OR UPDATE ON opt_out_policies FOR EACH ROW EXECUTE FUNCTION record_opt_out_policy_history();
CREATE OR REPLACE FUNCTION prevent_opt_out_history_mutation() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'opt-out policy history is append-only' USING ERRCODE='55000'; END; $$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_opt_out_policy_history_immutable ON opt_out_policy_history;
CREATE TRIGGER trg_opt_out_policy_history_immutable BEFORE UPDATE OR DELETE ON opt_out_policy_history FOR EACH ROW EXECUTE FUNCTION prevent_opt_out_history_mutation();
COMMIT;
