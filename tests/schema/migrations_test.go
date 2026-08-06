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
