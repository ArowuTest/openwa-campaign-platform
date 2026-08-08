BEGIN;

-- Stable keyset continuation for accumulating governance/history APIs. Each
-- index matches the parent filter followed by the exact descending cursor key.
CREATE INDEX IF NOT EXISTS gateway_pool_events_pool_occurred_id_idx
  ON gateway_pool_events(gateway_pool_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS audience_import_mapping_events_mapping_occurred_id_idx
  ON audience_import_mapping_events(mapping_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS reporting_privacy_policy_events_policy_occurred_id_idx
  ON reporting_privacy_policy_events(policy_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS operational_alert_policy_events_policy_occurred_id_idx
  ON operational_alert_policy_events(alert_policy_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS platform_configuration_events_configuration_occurred_id_idx
  ON platform_configuration_events(configuration_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS maintenance_window_events_window_occurred_id_idx
  ON maintenance_window_events(maintenance_window_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS privacy_case_events_case_occurred_id_idx
  ON privacy_case_events(privacy_case_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS privacy_legal_hold_events_hold_occurred_id_idx
  ON privacy_legal_hold_events(legal_hold_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS provider_capability_events_definition_id_idx
  ON provider_capability_events(definition_id, id DESC);
CREATE INDEX IF NOT EXISTS retention_policy_events_policy_occurred_id_idx
  ON retention_policy_events(retention_policy_id, occurred_at DESC, id DESC);

COMMIT;
