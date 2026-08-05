BEGIN;

CREATE TABLE provider_capability_definitions (
    id uuid PRIMARY KEY,
    provider text NOT NULL CHECK (provider = upper(provider) AND provider <> ''),
    channel text NOT NULL CHECK (channel IN ('WHATSAPP','SMS','EMAIL')),
    engine text NOT NULL DEFAULT '',
    adapter_version text NOT NULL CHECK (btrim(adapter_version) <> ''),
    minimum_gateway_version text,
    capabilities text[] NOT NULL CHECK (cardinality(capabilities) > 0),
    maximum_attachment_bytes bigint NOT NULL DEFAULT 0 CHECK (maximum_attachment_bytes >= 0),
    status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','RETIRED')),
    effective_from timestamptz NOT NULL,
    effective_to timestamptz,
    version bigint NOT NULL CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES user_accounts(id),
    submitted_by uuid REFERENCES user_accounts(id),
    approved_by uuid REFERENCES user_accounts(id),
    reason text NOT NULL CHECK (length(btrim(reason)) >= 5),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (effective_to IS NULL OR effective_to > effective_from),
    CHECK (channel <> 'WHATSAPP' OR engine <> '')
);

CREATE UNIQUE INDEX provider_capability_one_active_route
ON provider_capability_definitions(provider,channel,engine)
WHERE status='ACTIVE' AND effective_to IS NULL;

CREATE INDEX provider_capability_effective_lookup
ON provider_capability_definitions(provider,channel,engine,status,effective_from DESC);

CREATE TABLE provider_capability_events (
    id bigserial PRIMARY KEY,
    definition_id uuid NOT NULL REFERENCES provider_capability_definitions(id),
    action text NOT NULL,
    actor_id uuid NOT NULL REFERENCES user_accounts(id),
    reason text NOT NULL,
    definition_version bigint NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION prevent_provider_capability_event_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'provider capability events are append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER provider_capability_events_no_update
BEFORE UPDATE OR DELETE ON provider_capability_events
FOR EACH ROW EXECUTE FUNCTION prevent_provider_capability_event_mutation();

COMMIT;
