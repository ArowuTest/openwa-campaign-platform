BEGIN;

-- Exact keyset support for newly paged top-level governance inventories.
CREATE INDEX IF NOT EXISTS platform_configurations_updated_id_idx
  ON platform_configurations(updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS maintenance_windows_updated_id_idx
  ON maintenance_windows(updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS retention_policies_created_id_idx
  ON retention_policies(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS operational_alert_policies_created_id_idx
  ON operational_alert_policies(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS provider_capability_definitions_created_id_idx
  ON provider_capability_definitions(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS reporting_privacy_policies_created_id_idx
  ON reporting_privacy_policies(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS reporting_privacy_policies_org_created_id_idx
  ON reporting_privacy_policies(organisation_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS audience_import_mapping_definitions_created_id_idx
  ON audience_import_mapping_definitions(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS audience_import_mapping_definitions_scope_created_id_idx
  ON audience_import_mapping_definitions(organisation_id, upper(coalesce(source_system,'')), created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS privacy_legal_holds_subject_created_id_idx
  ON privacy_legal_holds(subject_lookup_hmac, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS organisation_policy_versions_org_created_id_idx
  ON organisation_policy_versions(organisation_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS campaign_commercial_approvals_created_id_idx
  ON campaign_commercial_approvals(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS campaign_commercial_approvals_org_created_id_idx
  ON campaign_commercial_approvals(organisation_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS consent_reviews_created_id_idx
  ON consent_reviews(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS consent_reviews_org_created_id_idx
  ON consent_reviews(organisation_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS inbound_retention_policies_created_id_idx
  ON inbound_retention_policies(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS opt_out_policies_created_id_idx
  ON opt_out_policies(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS internal_users_created_id_idx
  ON internal_users(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS sender_pacing_policies_created_id_idx
  ON sender_pacing_policies(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS sender_pools_name_id_idx
  ON sender_pools(name ASC, id ASC);
CREATE INDEX IF NOT EXISTS sender_nodes_name_id_idx
  ON sender_nodes(name ASC, id ASC);
CREATE INDEX IF NOT EXISTS gateway_pools_name_id_idx
  ON gateway_pools(name ASC, id ASC);

COMMIT;
