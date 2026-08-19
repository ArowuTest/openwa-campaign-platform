package schema_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func migrationDirectory(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// go test executes this package from tests/schema.
	return filepath.Clean(filepath.Join(wd, "..", "..", "database", "migrations"))
}

func migrationFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(migrationDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary migration artifact must not be committed: %s", entry.Name())
		}
		if strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, filepath.Join(migrationDirectory(t), entry.Name()))
		}
	}
	sort.Strings(files)
	return files
}

func TestMigrationsAreTransactionBounded(t *testing.T) {
	for _, file := range migrationFiles(t) {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		normalized := strings.ToUpper(strings.TrimSpace(string(content)))
		if !strings.HasPrefix(normalized, "BEGIN;") || !strings.HasSuffix(normalized, "COMMIT;") {
			t.Errorf("migration %s is not bounded by BEGIN/COMMIT", filepath.Base(file))
		}
		if strings.Count(normalized, "BEGIN;") != 1 || strings.Count(normalized, "COMMIT;") != 1 {
			t.Errorf("migration %s must contain one explicit transaction", filepath.Base(file))
		}
	}
}

func TestFinalHighVolumeSchemaContainsRequiredFencingAndIndexes(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0012_high_volume_indexes_and_fencing.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(content)
	for _, required := range []string{
		"durable_jobs\n  ADD COLUMN IF NOT EXISTS lease_version",
		"transactional_outbox\n  ADD COLUMN IF NOT EXISTS lease_version",
		"event_fingerprint",
		"profile_recorded_at",
		"idx_contacts_active_geo_age_id",
		"idx_consent_grants_contact_basis_active",
		"idx_suppressions_contact_window_active",
		"idx_durable_jobs_pending_claim",
		"idx_durable_jobs_expired_lease_claim",
		"idx_transactional_outbox_pending_claim",
		"idx_transactional_outbox_expired_lease_claim",
		"idx_sender_sessions_allocatable_pool",
		"uq_message_versions_campaign_content_hash",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("high-volume migration missing %q", required)
		}
	}
	if strings.Contains(sqlText, "xmax") {
		t.Fatal("application merge logic must not depend on PostgreSQL MVCC implementation columns")
	}
}

func TestFinalIndexSetDropsKnownRedundantIndexes(t *testing.T) {
	createPattern := regexp.MustCompile(`(?is)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z0-9_]+)\s+ON`)
	dropPattern := regexp.MustCompile(`(?is)DROP\s+INDEX\s+(?:IF\s+EXISTS\s+)?([a-zA-Z0-9_]+)`)
	final := map[string]bool{}
	for _, file := range migrationFiles(t) {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		// Preserve statement order within a migration, because a migration can drop
		// an old index and create its replacement.
		statementPattern := regexp.MustCompile(`(?is)(CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?[a-zA-Z0-9_]+\s+ON|DROP\s+INDEX\s+(?:IF\s+EXISTS\s+)?[a-zA-Z0-9_]+)`)
		for _, statement := range statementPattern.FindAllString(text, -1) {
			if match := createPattern.FindStringSubmatch(statement); len(match) == 2 {
				final[strings.ToLower(match[1])] = true
				continue
			}
			if match := dropPattern.FindStringSubmatch(statement); len(match) == 2 {
				delete(final, strings.ToLower(match[1]))
			}
		}
	}
	for _, redundant := range []string{
		"idx_audience_import_staging_lookup",      // duplicated by UNIQUE(import_id, lookup_hmac)
		"idx_audience_snapshot_members_ordered",   // duplicated by the primary key
		"idx_campaign_recipients_dispatch",        // dispatch is driven by durable jobs
		"idx_campaign_recipients_campaign_status", // replaced by ordered dashboard index
		"idx_transactional_outbox_pending",        // replaced by split claim indexes
	} {
		if final[redundant] {
			t.Errorf("redundant high-write index remains in final schema: %s", redundant)
		}
	}
}

func TestImportGovernanceMigrationContainsReplayLeaseAndImmutabilityControls(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0014_import_governance_and_replay.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(content)
	for _, required := range []string{
		"'QUARANTINED'",
		"client_request_id text",
		"validation_lease_version bigint NOT NULL DEFAULT 0",
		"uq_audience_imports_org_client_request",
		"idx_audience_imports_validation_lease",
		"malware_scan_status='CLEAN'",
		"content_signature_valid=true",
		"prevent_audience_import_identity_mutation",
		"trg_audience_import_identity_immutable",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("import governance migration missing %q", required)
		}
	}
}

func TestProviderIdentityMigrationSeparatesEventAndMessageLookup(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0015_provider_event_and_message_identity.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(content)
	for _, required := range []string{"provider_message_id text", "idx_delivery_events_provider_message_time", "idx_delivery_events_provider_event_received"} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("provider identity migration missing %q", required)
		}
	}
}

func TestQueryPathIndexCorrectionCoversMandatoryCohortAndWorkerClaims(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0016_query_path_index_corrections.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(content)
	for _, required := range []string{
		"idx_contacts_active_country_state_age_id",
		"DROP INDEX IF EXISTS idx_contacts_active_geo_gender_age_id",
		"INCLUDE (granted_at,wording_version)",
		"idx_durable_jobs_type_pending_claim",
		"idx_durable_jobs_type_expired_lease_claim",
		"DROP CONSTRAINT IF EXISTS delivery_events_provider_event_id_event_type_key",
		"uq_delivery_events_provider_event_id",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("query-path correction migration missing %q", required)
		}
	}
}

func TestBackendHardeningMigrationPersistsProviderAndRoutingIdempotencyEvidence(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0059_backend_hardening_and_provider_binding.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(content)
	for _, required := range []string{
		"provider_capability_definition_id uuid REFERENCES provider_capability_definitions(id)",
		"provider_capability_active_period_exclusion",
		"idempotency_key text",
		"request_hash text",
		"uq_campaign_routing_plan_idempotency",
		"idx_campaign_recipients_campaign_contact_keyset",
		"idx_transactional_outbox_evidence_keyset",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("backend hardening migration missing %q", required)
		}
	}
}

func TestSenderLifecycleAndGatewayFencingMigrationContainsCanonicalControls(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0060_sender_lifecycle_and_gateway_fencing.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(content)
	for _, required := range []string{
		"gateway_session_authorities",
		"session_lease_version bigint",
		"session_configuration_version bigint",
		"authority_expires_at timestamptz",
		"route_reference text",
		"prevent_gateway_session_authority_event_mutation",
		"'NEW','PAIRING','CONNECTING','READY','BUSY','DRAINING','PAUSED',",
		"'DISCONNECTED','RECOVERING','FAILED_RECOVERY','RESTRICTED','QUARANTINED','RETIRED'",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("sender lifecycle migration missing %q", required)
		}
	}
}

func TestTrustedAssetAndConsentReviewMigrationContainsTrustBoundaryControls(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0061_trusted_assets_and_consent_review_governance.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(content)
	for _, required := range []string{
		"CREATE TABLE trusted_assets",
		"CREATE TABLE trusted_asset_events",
		"message_versions_trusted_media_check",
		"review_scope text",
		"exact_consent_wording text",
		"permitted_partner_organisations jsonb",
		"trusted_asset_id uuid REFERENCES trusted_assets(id)",
		"CREATE TABLE consent_review_events",
		"'REVOKED','SUPERSEDED'",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("trusted asset and consent governance migration missing %q", required)
		}
	}
}

func TestPartitionReadinessMigrationIsGovernedAndFailClosed(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0066_partition_readiness_and_maintenance.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sqlText := string(content)
	for _, required := range []string{
		"CREATE TABLE high_volume_partition_policies",
		"CREATE TABLE high_volume_partition_windows",
		"CREATE TABLE high_volume_partition_events",
		"high_volume_partition_events_immutable",
		"create_governed_range_partition",
		"parent relation is not declaratively partitioned",
		"detach_governed_partition",
		"record_governed_partition_archive",
		"campaign_recipients_partition_time_idx",
		"delivery_events_partition_time_idx",
		"audit_events_partition_time_idx",
		"high_volume_partition_readiness",
		"REVOKE ALL ON FUNCTION create_governed_range_partition",
	} {
		if !strings.Contains(sqlText, required) {
			t.Errorf("partition-readiness migration missing %q", required)
		}
	}
	validation, err := os.ReadFile(filepath.Join(migrationDirectory(t), "..", "tests", "partition_maintenance.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"PARTITION BY RANGE", "create_governed_range_partition", "cross-partition query failed", "detach_governed_partition", "ROLLBACK;"} {
		if !strings.Contains(string(validation), required) {
			t.Errorf("partition validation script missing %q", required)
		}
	}
}

func TestMigrationsReferenceCanonicalRolesTable(t *testing.T) {
	for _, file := range migrationFiles(t) {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		for _, obsolete := range []string{"role_definitions", "internal_roles", "REFERENCES users(id)", "REFERENCES segment_definitions(id)", "REFERENCES user_accounts(id)"} {
			if strings.Contains(string(content), obsolete) {
				t.Errorf("migration %s references obsolete %s table; canonical table is roles", filepath.Base(file), obsolete)
			}
		}
	}
}

func TestMetaUpgradeMigrationsNormalizeLegacyInvalidEvidenceBeforeConstraints(t *testing.T) {
	m77, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0077_meta_cloud_council_hardening.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text77 := string(m77)
	cleanup77 := strings.Index(text77, "UPDATE campaigns")
	constraint77 := strings.Index(text77, "ADD CONSTRAINT campaigns_meta_sender_route_coherence")
	if cleanup77 < 0 || constraint77 < 0 || cleanup77 > constraint77 {
		t.Fatal("0077 must unfreeze legacy partial Meta campaign evidence before adding route coherence")
	}

	m78, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0078_campaign_transport_null_hardening.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text78 := string(m78)
	cleanup78 := strings.Index(text78, "UPDATE campaigns")
	providerCheck78 := strings.Index(text78, "ADD CONSTRAINT campaigns_transport_provider_engine_check")
	routeCheck78 := strings.Index(text78, "ADD CONSTRAINT campaigns_transport_route_check")
	if cleanup78 < 0 || providerCheck78 < 0 || routeCheck78 < 0 || cleanup78 > providerCheck78 || cleanup78 > routeCheck78 {
		t.Fatal("0078 must normalize legacy transport evidence before adding strict NULL-safe checks")
	}

	m79, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0079_meta_sender_tenant_pool_fencing.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text79 := string(m79)
	campaignCleanup := strings.Index(text79, "UPDATE campaigns c")
	campaignFK := strings.Index(text79, "ADD CONSTRAINT campaigns_meta_sender_org_pool_fkey")
	if campaignCleanup < 0 || campaignFK < 0 || campaignCleanup > campaignFK {
		t.Fatal("0079 must unfreeze invalid campaign Meta ownership before adding its composite foreign key")
	}
	quarantine := strings.Index(text79, "CREATE TABLE campaign_routing_plan_quarantine_evidence")
	routeFK := strings.Index(text79, "ADD CONSTRAINT campaign_route_meta_sender_pool_fkey")
	if quarantine < 0 || routeFK < 0 || quarantine > routeFK {
		t.Fatal("0079 must quarantine invalid routing plans before adding the Meta sender/pool foreign key")
	}
	for _, required := range []string{
		"CREATE TABLE campaign_routing_plan_quarantined_routes",
		"CREATE TEMP TABLE invalid_routing_plans ON COMMIT DROP",
		"route_evidence jsonb NOT NULL",
		"reservation_evidence jsonb NOT NULL",
		"SET status='RELEASED'",
		"DELETE FROM campaign_routing_plan_pools",
		"CREATE TRIGGER trg_campaign_route_pool_organisation",
		"UPDATE OF routing_plan_id,sender_pool_id,meta_sender_id",
		"routing plan is quarantined and cannot accept active routes",
		"SET search_path = pg_catalog, public",
	} {
		if !strings.Contains(text79, required) {
			t.Errorf("0079 upgrade hardening missing %q", required)
		}
	}
	reservationRelease := strings.Index(text79, "SET status='RELEASED'")
	routeDelete := strings.Index(text79, "DELETE FROM campaign_routing_plan_pools")
	if reservationRelease < 0 || routeDelete < 0 || reservationRelease > routeDelete {
		t.Fatal("0079 must release capacity reservations before removing quarantined plan routes")
	}

	m81, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0081_frozen_provider_gateway_version_null_hardening.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text81 := string(m81)
	quarantine81 := strings.Index(text81, "CREATE TEMP TABLE invalid_frozen_version_plans")
	routeConstraint81 := strings.Index(text81, "ADD CONSTRAINT campaign_route_provider_binding_pair")
	campaignCleanup81 := strings.Index(text81, "UPDATE campaigns")
	campaignConstraint81 := strings.Index(text81, "ADD CONSTRAINT campaigns_provider_capability_binding_pair")
	if quarantine81 < 0 || routeConstraint81 < 0 || quarantine81 > routeConstraint81 {
		t.Fatal("0081 must quarantine partial frozen routing evidence before installing NULL-safe route checks")
	}
	if campaignCleanup81 < 0 || campaignConstraint81 < 0 || campaignCleanup81 > campaignConstraint81 {
		t.Fatal("0081 must unfreeze partial campaign evidence before installing NULL-safe campaign checks")
	}
	for _, required := range []string{
		"provider_capability_definition_version IS NOT NULL",
		"gateway_pool_version IS NOT NULL",
		"campaign_route_frozen_version_null_safe_check",
		"campaigns_frozen_version_null_safe_check",
		"FROZEN_PROVIDER_OR_GATEWAY_VERSION_MISSING",
		"SET status='RELEASED'",
		"DELETE FROM campaign_routing_plan_pools",
	} {
		if !strings.Contains(text81, required) {
			t.Errorf("0081 NULL-safe frozen-version hardening missing %q", required)
		}
	}

	m80, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0080_meta_route_sender_version_null_hardening.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text80 := string(m80)
	quarantine80 := strings.Index(text80, "CREATE TEMP TABLE invalid_meta_sender_version_plans ON COMMIT DROP")
	constraint80 := strings.Index(text80, "ADD CONSTRAINT campaign_routing_plan_pools_meta_sender_version_frozen_check")
	if quarantine80 < 0 || constraint80 < 0 || quarantine80 > constraint80 {
		t.Fatal("0080 must quarantine META routes with missing frozen sender version before installing the NULL-safe constraint")
	}
	for _, required := range []string{"meta_sender_version IS NOT NULL AND meta_sender_version > 0", "META_SENDER_VERSION_MISSING", "SET status='RELEASED'", "DELETE FROM campaign_routing_plan_pools"} {
		if !strings.Contains(text80, required) {
			t.Errorf("0080 sender-version hardening missing %q", required)
		}
	}

	m82, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0082_meta_conversation_window_evidence.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text82 := string(m82)
	for _, required := range []string{
		"CREATE TABLE meta_cloud_conversation_windows",
		"meta_sender_id uuid NOT NULL REFERENCES meta_cloud_senders(id)",
		"contact_id uuid NOT NULL REFERENCES contacts(id)",
		"PRIMARY KEY (meta_sender_id, source_provider_message_id)",
		"CHECK (eligible_until > inbound_occurred_at)",
		"CREATE INDEX idx_meta_conversation_window_current",
		"CREATE TRIGGER trg_meta_conversation_window_immutable",
		"SET search_path = pg_catalog, public",
	} {
		if !strings.Contains(text82, required) {
			t.Errorf("0082 conversation-window evidence missing %q", required)
		}
	}
	m83, err := os.ReadFile(filepath.Join("..", "..", "database", "migrations", "0083_final_review_boundary_hardening.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text83 := string(m83)
	for _, required := range []string{"meta_cloud_conversation_windows_max_duration_check", "trg_meta_conversation_window_insert_tombstone_guard", "released_by text", "released_at timestamptz", "meta_cloud_conversation_window_quarantine", "meta_cloud_conversation_window_tombstones"} {
		if !strings.Contains(text83, required) {
			t.Errorf("0083 boundary hardening missing %q", required)
		}
	}
	m84, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0084_final_council_evidence_hardening.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text84 := string(m84)
	for _, required := range []string{
		"trg_meta_conversation_window_tombstone_immutable",
		"trg_meta_conversation_window_quarantine_immutable",
		"trg_campaign_route_quarantine_evidence_immutable",
		"trg_campaign_quarantined_route_immutable",
		"meta_cloud_conversation_window_quarantine retained",
		"campaign_pool_capacity_reservations_release_evidence_check",
		"trg_reservation_release_evidence_immutable",
		"meta_cloud_conversation_windows_receipt_skew_check",
	} {
		if !strings.Contains(text84, required) {
			t.Errorf("0084 retained-evidence hardening missing %q", required)
		}
	}

	m87, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0087_final_readiness_and_authority_fences.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text87 := string(m87)
	for _, required := range []string{
		"campaigns_frozen_authority_v87_check",
		"CONTROL_SCHEMA_READINESS_HARDENING",
		"source_migration=85",
		"trg_platform_schema_capability_no_truncate",
		"BEFORE TRUNCATE ON platform_schema_capabilities",
		"campaign_pool_capacity_reservation_release_tombstones",
		"trg_reservation_release_delete_tombstone",
		"trg_reservation_release_tombstone_immutable",
		"trg_meta_conversation_window_no_truncate",
		"trg_meta_window_tombstone_no_truncate",
		"trg_meta_window_quarantine_no_truncate",
	} {
		if !strings.Contains(text87, required) {
			t.Errorf("0087 final readiness hardening missing %q", required)
		}
	}
}
