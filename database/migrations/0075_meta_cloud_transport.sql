BEGIN;

CREATE TABLE meta_cloud_senders (
    id uuid PRIMARY KEY,
    organisation_id uuid NOT NULL REFERENCES organisations(id),
    sender_pool_id uuid NOT NULL REFERENCES sender_pools(id),
    waba_id text NOT NULL CHECK (btrim(waba_id) <> ''),
    phone_number_id text NOT NULL CHECK (btrim(phone_number_id) <> ''),
    display_name text NOT NULL CHECK (btrim(display_name) <> ''),
    business_phone_display text NOT NULL CHECK (btrim(business_phone_display) <> ''),
    credential_key text NOT NULL CHECK (credential_key ~ '^[A-Za-z0-9][A-Za-z0-9._-]{2,127}$'),
    graph_api_version text NOT NULL CHECK (graph_api_version ~ '^v[0-9]{1,3}\.[0-9]{1,2}$'),
    status text NOT NULL CHECK (status IN ('DRAFT','PENDING_APPROVAL','ACTIVE','REJECTED','RETIRED')),
    health_status text NOT NULL DEFAULT 'HEALTH_UNKNOWN' CHECK (health_status IN ('HEALTH_UNKNOWN','HEALTHY','DEGRADED','UNAVAILABLE')),
    health_observed_at timestamptz,
    effective_from timestamptz,
    effective_to timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES internal_users(id),
    submitted_by uuid REFERENCES internal_users(id),
    approved_by uuid REFERENCES internal_users(id),
    reason text NOT NULL CHECK (length(btrim(reason)) >= 5),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (phone_number_id),
    CHECK ((health_status='HEALTH_UNKNOWN' AND health_observed_at IS NULL) OR (health_status<>'HEALTH_UNKNOWN' AND health_observed_at IS NOT NULL)),
    CHECK (effective_to IS NULL OR (effective_from IS NOT NULL AND effective_to > effective_from)),
    CHECK ((status='ACTIVE' AND effective_from IS NOT NULL AND effective_to IS NULL) OR (status='RETIRED' AND effective_from IS NOT NULL AND effective_to IS NOT NULL) OR (status IN ('DRAFT','PENDING_APPROVAL','REJECTED') AND effective_from IS NULL AND effective_to IS NULL))
);
CREATE INDEX idx_meta_cloud_senders_org_status ON meta_cloud_senders(organisation_id,status,created_at DESC);
CREATE INDEX idx_meta_cloud_senders_pool_health ON meta_cloud_senders(sender_pool_id,status,health_status,health_observed_at DESC);
CREATE INDEX idx_meta_cloud_senders_waba ON meta_cloud_senders(organisation_id,waba_id,status);

CREATE TABLE meta_cloud_sender_events (
    id bigserial PRIMARY KEY,
    sender_id uuid NOT NULL REFERENCES meta_cloud_senders(id) ON DELETE RESTRICT,
    action text NOT NULL CHECK (btrim(action) <> ''),
    actor_id uuid NOT NULL REFERENCES internal_users(id),
    reason text NOT NULL CHECK (length(btrim(reason)) >= 5),
    sender_version bigint NOT NULL CHECK (sender_version > 0),
    evidence jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(evidence)='object'),
    occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_meta_cloud_sender_events_sender_time ON meta_cloud_sender_events(sender_id,occurred_at DESC,id DESC);

CREATE TABLE meta_cloud_templates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organisation_id uuid NOT NULL REFERENCES organisations(id),
    waba_id text NOT NULL CHECK (btrim(waba_id) <> ''),
    meta_template_id text NOT NULL CHECK (btrim(meta_template_id) <> ''),
    name text NOT NULL CHECK (btrim(name) <> ''),
    language text NOT NULL CHECK (btrim(language) <> ''),
    category text NOT NULL CHECK (btrim(category) <> ''),
    status text NOT NULL CHECK (btrim(status) <> ''),
    quality_signal text,
    components jsonb NOT NULL CHECK (jsonb_typeof(components)='array'),
    component_hash text NOT NULL CHECK (length(component_hash)=64),
    last_synced_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organisation_id,waba_id,name,language)
);
CREATE INDEX idx_meta_cloud_templates_waba_status ON meta_cloud_templates(organisation_id,waba_id,status,name,language);

CREATE TABLE meta_cloud_message_bindings (
    message_version_id uuid PRIMARY KEY REFERENCES message_versions(id) ON DELETE RESTRICT,
    template_name text NOT NULL CHECK (btrim(template_name) <> ''),
    language text NOT NULL CHECK (btrim(language) <> ''),
    body_variable_names jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(body_variable_names)='array'),
    media_header_type text CHECK (media_header_type IS NULL OR media_header_type IN ('IMAGE','VIDEO','DOCUMENT')),
    template_component_hash text NOT NULL CHECK (length(template_component_hash)=64),
    created_by uuid NOT NULL REFERENCES internal_users(id),
    created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE campaign_routing_plans
    ADD COLUMN distribution_mode text NOT NULL DEFAULT 'WEIGHTED';
ALTER TABLE campaign_routing_plans
    ADD CONSTRAINT campaign_routing_plans_distribution_mode_check CHECK (distribution_mode IN ('AUTO','WEIGHTED'));

ALTER TABLE campaign_routing_plan_pools
    ALTER COLUMN gateway_pool_id DROP NOT NULL,
    ADD COLUMN meta_sender_id uuid REFERENCES meta_cloud_senders(id),
    ADD COLUMN meta_sender_version bigint CHECK (meta_sender_version IS NULL OR meta_sender_version > 0);

ALTER TABLE campaign_routing_plan_pools
    DROP CONSTRAINT campaign_routing_plan_pools_provider_check,
    DROP CONSTRAINT campaign_routing_plan_pools_engine_check,
    ADD CONSTRAINT campaign_routing_plan_pools_provider_engine_check CHECK (
        (provider='OPENWA' AND engine IN ('WHATSAPP_WEB_JS','BAILEYS')) OR
        (provider='META' AND engine='CLOUD_API')
    ),
    ADD CONSTRAINT campaign_routing_plan_pools_endpoint_check CHECK (
        (provider='OPENWA' AND gateway_pool_id IS NOT NULL AND meta_sender_id IS NULL AND meta_sender_version IS NULL) OR
        (provider='META' AND gateway_pool_id IS NULL AND meta_sender_id IS NOT NULL AND meta_sender_version > 0)
    );
ALTER TABLE campaign_routing_plan_pools
    DROP CONSTRAINT IF EXISTS campaign_route_provider_binding_pair,
    ADD CONSTRAINT campaign_route_provider_binding_pair CHECK (
        (provider_capability_definition_id IS NULL AND provider_capability_definition_version IS NULL AND gateway_pool_version IS NULL AND coalesce(btrim(provider_adapter_version),'')='')
        OR
        (provider_capability_definition_id IS NOT NULL AND provider_capability_definition_version > 0
         AND coalesce(btrim(provider_adapter_version),'') <> ''
         AND ((provider='OPENWA' AND gateway_pool_version > 0) OR (provider='META' AND gateway_pool_version IS NULL)))
    );
CREATE INDEX idx_campaign_route_meta_sender ON campaign_routing_plan_pools(meta_sender_id,routing_plan_id) WHERE meta_sender_id IS NOT NULL;

ALTER TABLE campaigns
    ADD COLUMN meta_sender_id uuid REFERENCES meta_cloud_senders(id);
ALTER TABLE campaigns
    DROP CONSTRAINT campaigns_transport_provider_check,
    DROP CONSTRAINT campaigns_transport_engine_check,
    DROP CONSTRAINT campaigns_transport_route_check,
    ADD CONSTRAINT campaigns_transport_provider_engine_check CHECK (
        transport_provider IS NULL OR
        (transport_provider='OPENWA' AND transport_engine IN ('WHATSAPP_WEB_JS','BAILEYS')) OR
        (transport_provider='META' AND transport_engine='CLOUD_API')
    ),
    ADD CONSTRAINT campaigns_transport_route_check CHECK (
        transport_routing_mode IS NULL OR
        (transport_provider='OPENWA' AND transport_routing_mode='SPECIFIC_SESSION' AND transport_session_id IS NOT NULL AND transport_sender_pool_id IS NULL AND coalesce(btrim(gateway_pool_id),'')<>'' AND meta_sender_id IS NULL) OR
        (transport_provider='OPENWA' AND transport_routing_mode='SENDER_POOL' AND transport_sender_pool_id IS NOT NULL AND transport_session_id IS NULL AND coalesce(btrim(gateway_pool_id),'')<>'' AND meta_sender_id IS NULL) OR
        (transport_provider='META' AND transport_routing_mode='SENDER_POOL' AND transport_sender_pool_id IS NOT NULL AND transport_session_id IS NULL AND coalesce(btrim(gateway_pool_id),'')='' AND meta_sender_id IS NOT NULL)
    );
ALTER TABLE sender_pools
    ADD CONSTRAINT sender_pools_id_organisation_key UNIQUE(id,organisation_id);
ALTER TABLE meta_cloud_senders
    ADD CONSTRAINT meta_cloud_senders_pool_organisation_fkey
    FOREIGN KEY(sender_pool_id,organisation_id) REFERENCES sender_pools(id,organisation_id);

ALTER TABLE campaigns
    DROP CONSTRAINT IF EXISTS campaigns_provider_capability_binding_pair,
    ADD CONSTRAINT campaigns_provider_capability_binding_pair CHECK (
        (provider_capability_definition_id IS NULL AND provider_capability_definition_version IS NULL AND gateway_pool_version IS NULL)
        OR
        (provider_capability_definition_id IS NOT NULL AND provider_capability_definition_version > 0
         AND coalesce(btrim(provider_adapter_version),'') <> ''
         AND ((transport_provider='OPENWA' AND gateway_pool_version > 0) OR (transport_provider='META' AND gateway_pool_version IS NULL)))
    );
CREATE INDEX idx_campaigns_meta_sender ON campaigns(meta_sender_id) WHERE meta_sender_id IS NOT NULL;

ALTER TABLE sender_pacing_policies
    DROP CONSTRAINT sender_pacing_policies_engine_check,
    ADD CONSTRAINT sender_pacing_policies_engine_check CHECK (engine IS NULL OR engine IN ('WHATSAPP_WEB_JS','BAILEYS','CLOUD_API'));

COMMIT;
