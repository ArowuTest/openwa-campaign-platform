BEGIN;

CREATE TABLE IF NOT EXISTS campaign_shard_reallocations (
    id uuid PRIMARY KEY,
    campaign_id uuid NOT NULL REFERENCES campaigns(id),
    routing_plan_id uuid NOT NULL REFERENCES campaign_routing_plans(id),
    dispatch_shard_id uuid NOT NULL REFERENCES campaign_dispatch_shards(id),
    from_sender_pool_id uuid NOT NULL REFERENCES sender_pools(id),
    to_sender_pool_id uuid NOT NULL REFERENCES sender_pools(id),
    actor_id uuid NOT NULL REFERENCES internal_users(id),
    reason text NOT NULL CHECK (length(btrim(reason)) >= 8),
    evidence_reference text NOT NULL CHECK (length(btrim(evidence_reference)) > 0),
    previous_lease_version bigint NOT NULL CHECK (previous_lease_version >= 0),
    new_lease_version bigint NOT NULL CHECK (new_lease_version = previous_lease_version + 1),
    created_at timestamptz NOT NULL,
    CHECK (from_sender_pool_id <> to_sender_pool_id)
);
CREATE INDEX IF NOT EXISTS idx_shard_reallocations_campaign_time ON campaign_shard_reallocations(campaign_id,created_at DESC);
CREATE INDEX IF NOT EXISTS idx_shard_reallocations_shard_time ON campaign_shard_reallocations(dispatch_shard_id,created_at);

CREATE OR REPLACE FUNCTION reject_campaign_shard_reallocation_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'campaign shard reallocation evidence is append-only';
END $$;
DROP TRIGGER IF EXISTS trg_campaign_shard_reallocation_immutable ON campaign_shard_reallocations;
CREATE TRIGGER trg_campaign_shard_reallocation_immutable BEFORE UPDATE OR DELETE ON campaign_shard_reallocations FOR EACH ROW EXECUTE FUNCTION reject_campaign_shard_reallocation_mutation();

COMMIT;
