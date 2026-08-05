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
