BEGIN;

-- Stable service privilege roles. These roles remain NOLOGIN deliberately.
-- Production provisioning creates rotatable LOGIN roles outside this file and
-- grants each login membership in exactly one privilege role. Credentials must
-- never be committed to this repository.
DO $$
DECLARE role_name text;
BEGIN
  FOREACH role_name IN ARRAY ARRAY[
    'campaign_control_api',
    'campaign_audience_worker',
    'campaign_campaign_worker',
    'campaign_export_worker',
    'campaign_inbound_governance_worker',
    'campaign_metrics_worker',
    'campaign_platform_governance_worker'
  ] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = role_name) THEN
      EXECUTE format('CREATE ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS', role_name);
    END IF;
  END LOOP;
END $$;

DO $$
BEGIN
  EXECUTE format('GRANT CONNECT ON DATABASE %I TO campaign_control_api, campaign_audience_worker, campaign_campaign_worker, campaign_export_worker, campaign_inbound_governance_worker, campaign_metrics_worker, campaign_platform_governance_worker', current_database());
END $$;

-- Owner-scoped defaults ensure later migrations do not silently create
-- relations that the synchronous control API cannot access after an upgrade.
-- Worker access is intentionally reconciled from explicit lists after every
-- migration run by 001_bootstrap.sh; broad default worker grants would defeat
-- the least-privilege boundary.
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO campaign_control_api;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO campaign_control_api;

-- Remove broad worker defaults from earlier releases before enforcing the
-- explicit post-migration policy below.
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE SELECT ON TABLES FROM campaign_export_worker;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE USAGE, SELECT ON SEQUENCES FROM
  campaign_audience_worker,
  campaign_campaign_worker,
  campaign_export_worker,
  campaign_inbound_governance_worker,
  campaign_metrics_worker,
  campaign_platform_governance_worker;

-- Grants are additive in PostgreSQL. Revoke previous broad or pattern-based
-- worker grants first so an upgrade converges to the exact current policy.
REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM
  campaign_audience_worker,
  campaign_campaign_worker,
  campaign_export_worker,
  campaign_inbound_governance_worker,
  campaign_metrics_worker,
  campaign_platform_governance_worker;
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM
  campaign_audience_worker,
  campaign_campaign_worker,
  campaign_export_worker,
  campaign_inbound_governance_worker,
  campaign_metrics_worker,
  campaign_platform_governance_worker;

GRANT USAGE ON SCHEMA public TO
  campaign_control_api,
  campaign_audience_worker,
  campaign_campaign_worker,
  campaign_export_worker,
  campaign_inbound_governance_worker,
  campaign_metrics_worker,
  campaign_platform_governance_worker;

-- The control API owns governed synchronous business mutations but cannot
-- create schemas, extensions, roles or migration objects.
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO campaign_control_api;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO campaign_control_api;

-- Grant only tables that exist. This file runs once before migrations and once
-- after them, so a clean install and an in-place upgrade use the same policy.
CREATE OR REPLACE FUNCTION pg_temp.grant_existing_tables(
  target_role text,
  privilege_list text,
  table_names text[]
) RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE table_name text;
BEGIN
  FOREACH table_name IN ARRAY table_names LOOP
    IF to_regclass(format('public.%I', table_name)) IS NOT NULL THEN
      EXECUTE format('GRANT %s ON TABLE public.%I TO %I', privilege_list, table_name, target_role);
    END IF;
  END LOOP;
END $$;

-- Audience worker: validation, merge, source retention, cohort execution and
-- immutable audience materialisation.
SELECT pg_temp.grant_existing_tables('campaign_audience_worker', 'SELECT', ARRAY[
  'administrative_areas','attribute_definitions','campaign_recipients','campaigns',
  'consent_purposes','consent_review_evidence','consent_reviews',
  'contact_attribute_values','countries','organisation_policy_versions',
  'organisations','suppressions'
]);
SELECT pg_temp.grant_existing_tables('campaign_audience_worker', 'SELECT, INSERT, UPDATE, DELETE', ARRAY[
  'audience_import_contact_mutations','audience_import_issues',
  'audience_import_mapping_definitions','audience_import_mapping_events',
  'audience_import_merge_results','audience_import_reconciliations',
  'audience_import_rollback_events','audience_import_source_deletion_events',
  'audience_import_staging','audience_imports','audience_materialisation_jobs',
  'audience_materialisation_members','audience_profile_conflicts',
  'audience_snapshot_members','audience_snapshots','audience_source_trust_policies',
  'consent_events','consent_grants','contact_lifecycle_events',
  'contact_profile_history','contact_sources','contacts',
  'segment_definition_versions','segments'
]);

-- Campaign worker: outbox, jobs, execution/sharding, controlled test sends,
-- dispatch, final eligibility, pacing and sender allocation.
SELECT pg_temp.grant_existing_tables('campaign_campaign_worker', 'SELECT', ARRAY[
  'administrative_areas','attribute_definitions','campaign_release_exclusions',
  'campaigns','consent_grants','consent_reviews','contact_attribute_values',
  'contacts','countries','maintenance_windows','message_versions',
  'organisation_policy_versions','organisations','platform_configurations',
  'provider_capability_definitions','sender_session_leases','suppressions'
]);
SELECT pg_temp.grant_existing_tables('campaign_campaign_worker', 'SELECT, INSERT, UPDATE, DELETE', ARRAY[
  'approved_test_recipients','campaign_capacity_assessments','campaign_dispatch_shards',
  'campaign_execution_events','campaign_execution_leases','campaign_metric_reconciliations',
  'campaign_metrics','campaign_pool_capacity_reservations','campaign_recipients',
  'campaign_routing_plan_pools','campaign_routing_plans','campaign_shard_reallocations',
  'delivery_events','delivery_exception_resolutions','durable_job_administration_events',
  'durable_jobs','gateway_pool_events','gateway_pools','gateway_runtime_events',
  'gateway_runtime_nonces','gateway_session_authorities','gateway_session_authority_events',
  'sender_daily_submission_usage','sender_governance_events','sender_hourly_submission_usage',
  'sender_nodes','sender_pacing_runtime','sender_pools','sender_session_campaign_assignments',
  'sender_sessions','test_message_sends'
]);
SELECT pg_temp.grant_existing_tables('campaign_campaign_worker', 'SELECT, UPDATE', ARRAY[
  'transactional_outbox'
]);

-- Export worker: it claims and finalises approved export jobs, verifies the
-- frozen audit-chain head and streams the immutable audit page set. It does not
-- own export approval, download grants or privacy-case mutation.
SELECT pg_temp.grant_existing_tables('campaign_export_worker', 'SELECT, UPDATE', ARRAY[
  'export_requests'
]);
SELECT pg_temp.grant_existing_tables('campaign_export_worker', 'SELECT', ARRAY[
  'audit_chain_head','audit_events'
]);

-- Inbound governance worker: encrypted-content rotation and governed retention.
SELECT pg_temp.grant_existing_tables('campaign_inbound_governance_worker', 'SELECT, INSERT, UPDATE, DELETE', ARRAY[
  'inbound_content_reencryption_runs','inbound_replies',
  'inbound_retention_policies','inbound_retention_sweep_runs'
]);

-- Metrics worker: durable reconciliation queue plus canonical/stored metric reads.
SELECT pg_temp.grant_existing_tables('campaign_metrics_worker', 'SELECT, INSERT, UPDATE', ARRAY[
  'campaign_metric_reconciliations'
]);
SELECT pg_temp.grant_existing_tables('campaign_metrics_worker', 'SELECT', ARRAY[
  'campaigns','campaign_metrics','campaign_recipients','campaign_release_exclusions'
]);

-- Platform-governance worker: retention execution, alert evaluation/escalation,
-- incident creation and the operational dashboard reads used by alert policies.
SELECT pg_temp.grant_existing_tables('campaign_platform_governance_worker', 'SELECT, INSERT, UPDATE, DELETE', ARRAY[
  'retention_jobs','retention_policies','retention_policy_events',
  'operational_alert_events','operational_alert_policies',
  'operational_alert_policy_events','operational_alerts',
  'operational_incident_events','operational_notifications','operations_incidents'
]);
SELECT pg_temp.grant_existing_tables('campaign_platform_governance_worker', 'SELECT, UPDATE', ARRAY[
  'audience_imports','export_requests','inbound_replies'
]);
SELECT pg_temp.grant_existing_tables('campaign_platform_governance_worker', 'SELECT', ARRAY[
  'administrative_areas','audit_events','campaign_capacity_assessments',
  'campaign_commercial_approvals','campaign_metric_reconciliations','campaign_metrics',
  'campaign_pool_capacity_reservations','campaign_recipients',
  'campaign_routing_plan_pools','campaign_routing_plans','campaigns','consent_purposes',
  'contacts','delivery_events','gender_options','organisations','privacy_cases',
  'privacy_legal_holds','reporting_privacy_policies','sender_nodes','sender_pools',
  'sender_sessions'
]);

GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO
  campaign_audience_worker,
  campaign_campaign_worker,
  campaign_export_worker,
  campaign_inbound_governance_worker,
  campaign_metrics_worker,
  campaign_platform_governance_worker;

-- Prevent accidental privilege inheritance from PUBLIC.
REVOKE CREATE ON SCHEMA public FROM PUBLIC;

COMMIT;
