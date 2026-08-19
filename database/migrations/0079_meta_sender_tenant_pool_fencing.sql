BEGIN;

-- Future Meta sender evidence is fenced by composite keys. Before adding the
-- foreign keys, preserve and fail-close any legacy evidence that was legal on
-- the pre-0079 schema but violates the new ownership invariant.
ALTER TABLE meta_cloud_senders
    ADD CONSTRAINT meta_cloud_senders_id_pool_key UNIQUE (id, sender_pool_id),
    ADD CONSTRAINT meta_cloud_senders_id_org_pool_key UNIQUE (id, organisation_id, sender_pool_id);

-- Campaign-level Meta authority can be safely unfrozen without guessing a
-- replacement sender. A governed routing plan, when present, remains separate.
UPDATE campaigns c
SET transport_provider = NULL,
    transport_engine = NULL,
    transport_routing_mode = NULL,
    gateway_pool_id = NULL,
    gateway_pool_version = NULL,
    transport_session_id = NULL,
    transport_sender_pool_id = NULL,
    meta_sender_id = NULL,
    provider_adapter_version = NULL,
    provider_capability_definition_id = NULL,
    provider_capability_definition_version = NULL,
    required_capabilities = '[]'::jsonb,
    routing_policy_version = NULL,
    capacity_evidence_version = NULL
WHERE c.meta_sender_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM meta_cloud_senders ms
      WHERE ms.id = c.meta_sender_id
        AND ms.organisation_id = c.organisation_id
        AND ms.sender_pool_id = c.transport_sender_pool_id
  );

-- A bad route cannot be repaired by substituting another sender or by deleting
-- only that route: either choice would change an approved transport subset.
-- Preserve the complete affected plan evidence, release its reservations, and
-- remove every active route from that plan so execution fails Plan.Validate().
CREATE TABLE campaign_routing_plan_quarantine_evidence (
    routing_plan_id uuid PRIMARY KEY REFERENCES campaign_routing_plans(id) ON DELETE RESTRICT,
    campaign_id uuid NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (length(btrim(reason)) >= 8),
    route_evidence jsonb NOT NULL CHECK (jsonb_typeof(route_evidence)='array'),
    reservation_evidence jsonb NOT NULL CHECK (jsonb_typeof(reservation_evidence)='array'),
    quarantined_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_campaign_routing_plan_quarantine_campaign
    ON campaign_routing_plan_quarantine_evidence(campaign_id, quarantined_at DESC);

-- Structured route tombstones are not execution authority. They exist only so
-- already-accepted Meta messages retain sender-bound webhook correlation after
-- a quarantined plan's live routes are removed.
CREATE TABLE campaign_routing_plan_quarantined_routes (
    routing_plan_id uuid NOT NULL REFERENCES campaign_routing_plans(id) ON DELETE RESTRICT,
    campaign_id uuid NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    sender_pool_id uuid NOT NULL REFERENCES sender_pools(id) ON DELETE RESTRICT,
    provider text NOT NULL,
    engine text NOT NULL,
    meta_sender_id uuid,
    meta_sender_version bigint,
    quarantined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (routing_plan_id, sender_pool_id)
);
CREATE INDEX idx_campaign_quarantined_route_meta_sender
    ON campaign_routing_plan_quarantined_routes(meta_sender_id, routing_plan_id)
    WHERE meta_sender_id IS NOT NULL;

CREATE TEMP TABLE invalid_routing_plans ON COMMIT DROP AS
SELECT DISTINCT rp.routing_plan_id
FROM campaign_routing_plan_pools rp
JOIN campaign_routing_plans plan ON plan.id=rp.routing_plan_id
JOIN campaigns c ON c.id=plan.campaign_id
JOIN sender_pools sp ON sp.id=rp.sender_pool_id
LEFT JOIN meta_cloud_senders ms ON ms.id=rp.meta_sender_id
WHERE (sp.organisation_id IS NOT NULL AND sp.organisation_id<>c.organisation_id)
   OR (rp.provider='META' AND (
       ms.id IS NULL
       OR ms.sender_pool_id<>rp.sender_pool_id
       OR ms.organisation_id<>c.organisation_id
   ));
INSERT INTO campaign_routing_plan_quarantined_routes(
    routing_plan_id,campaign_id,sender_pool_id,provider,engine,meta_sender_id,meta_sender_version
)
SELECT route_row.routing_plan_id,plan.campaign_id,route_row.sender_pool_id,
       route_row.provider,route_row.engine,route_row.meta_sender_id,route_row.meta_sender_version
FROM campaign_routing_plan_pools route_row
JOIN campaign_routing_plans plan ON plan.id=route_row.routing_plan_id
JOIN invalid_routing_plans invalid ON invalid.routing_plan_id=route_row.routing_plan_id;

INSERT INTO campaign_routing_plan_quarantine_evidence(
    routing_plan_id,campaign_id,reason,route_evidence,reservation_evidence
)
SELECT plan.id,plan.campaign_id,'ROUTING_SENDER_OR_POOL_OWNERSHIP_MISMATCH',
       coalesce((
           SELECT jsonb_agg(to_jsonb(route_row) ORDER BY route_row.sender_pool_id::text)
           FROM campaign_routing_plan_pools route_row
           WHERE route_row.routing_plan_id=plan.id
       ),'[]'::jsonb),
       coalesce((
           SELECT jsonb_agg(to_jsonb(reservation_row) ORDER BY reservation_row.id::text)
           FROM campaign_pool_capacity_reservations reservation_row
           WHERE reservation_row.routing_plan_id=plan.id
       ),'[]'::jsonb)
FROM campaign_routing_plans plan
JOIN invalid_routing_plans invalid ON invalid.routing_plan_id=plan.id;

UPDATE campaign_pool_capacity_reservations reservation
SET status='RELEASED',
    fencing_version=fencing_version+1,
    updated_at=now()
WHERE reservation.routing_plan_id IN (SELECT routing_plan_id FROM invalid_routing_plans)
  AND reservation.status IN ('HELD','ACTIVE');

DELETE FROM campaign_routing_plan_pools route_row
WHERE route_row.routing_plan_id IN (SELECT routing_plan_id FROM invalid_routing_plans);

ALTER TABLE campaigns
    ADD CONSTRAINT campaigns_meta_sender_org_pool_fkey
    FOREIGN KEY (meta_sender_id, organisation_id, transport_sender_pool_id)
    REFERENCES meta_cloud_senders (id, organisation_id, sender_pool_id);
ALTER TABLE campaign_routing_plan_pools
    ADD CONSTRAINT campaign_route_meta_sender_pool_fkey
    FOREIGN KEY (meta_sender_id, sender_pool_id)
    REFERENCES meta_cloud_senders (id, sender_pool_id);

-- Keep sender-pool tenant ownership at the PostgreSQL boundary as well as in
-- PostgreSQLRoutingPlanStore. NULL-organisation pools remain platform-shared.
CREATE FUNCTION enforce_campaign_route_pool_organisation() RETURNS trigger AS $$
DECLARE
    campaign_org uuid;
    pool_org uuid;
    meta_sender_org uuid;
    meta_sender_pool uuid;
BEGIN
    IF EXISTS (
        SELECT 1 FROM campaign_routing_plan_quarantine_evidence quarantine
        WHERE quarantine.routing_plan_id=NEW.routing_plan_id
    ) THEN
        RAISE EXCEPTION 'routing plan is quarantined and cannot accept active routes';
    END IF;

    SELECT c.organisation_id INTO campaign_org
    FROM campaign_routing_plans plan
    JOIN campaigns c ON c.id=plan.campaign_id
    WHERE plan.id=NEW.routing_plan_id;

    SELECT organisation_id INTO pool_org
    FROM sender_pools
    WHERE id=NEW.sender_pool_id;

    IF pool_org IS NOT NULL AND campaign_org IS NOT NULL AND pool_org<>campaign_org THEN
        RAISE EXCEPTION 'routing-plan sender pool does not belong to campaign organisation';
    END IF;

    IF NEW.meta_sender_id IS NOT NULL THEN
        SELECT organisation_id,sender_pool_id INTO meta_sender_org,meta_sender_pool
        FROM meta_cloud_senders
        WHERE id=NEW.meta_sender_id;
        IF NOT FOUND OR meta_sender_pool<>NEW.sender_pool_id OR meta_sender_org<>campaign_org THEN
            RAISE EXCEPTION 'Meta sender does not belong to routing-plan campaign organisation and pool';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql
SET search_path = pg_catalog, public;

CREATE TRIGGER trg_campaign_route_pool_organisation
BEFORE INSERT OR UPDATE OF routing_plan_id,sender_pool_id,meta_sender_id
ON campaign_routing_plan_pools
FOR EACH ROW EXECUTE FUNCTION enforce_campaign_route_pool_organisation();
COMMIT;