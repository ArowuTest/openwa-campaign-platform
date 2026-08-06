package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceRoleBootstrapIsFailClosed(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(wd, "..", "..", "database", "bootstrap", "002_service_roles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"campaign_control_api", "campaign_audience_worker", "campaign_campaign_worker",
		"campaign_export_worker", "campaign_inbound_governance_worker", "campaign_metrics_worker",
		"campaign_platform_governance_worker", "NOLOGIN", "REVOKE CREATE ON SCHEMA public FROM PUBLIC",
		"REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public", "BEGIN;", "COMMIT;",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("service role bootstrap missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(text), "password '") {
		t.Fatal("committed role bootstrap must not contain passwords")
	}
}

func TestDatabaseBootstrapReconcilesServiceGrantsAfterMigrations(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(wd, "..", "..", "database", "bootstrap", "001_bootstrap.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	marker := `psql --set ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --file "$role_bootstrap"`
	if count := strings.Count(text, marker); count != 2 {
		t.Fatalf("service-role bootstrap must run before and after migrations, count=%d", count)
	}
	migrationLoop := strings.Index(text, `for migration in /opt/campaign/migrations/*.sql`)
	lastRolePass := strings.LastIndex(text, marker)
	if migrationLoop < 0 || lastRolePass < migrationLoop {
		t.Fatal("post-migration service-role reconciliation is missing")
	}
}

func TestServiceRoleBootstrapDeclaresWorkerDependenciesExplicitly(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(wd, "..", "..", "database", "bootstrap", "002_service_roles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"pg_temp.grant_existing_tables",
		"campaign_metric_reconciliations", "campaign_release_exclusions",
		"operational_alert_policies", "operational_alerts", "operations_incidents",
		"retention_jobs", "privacy_legal_holds", "audience_imports", "inbound_replies",
		"export_requests", "audit_chain_head", "audit_events",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("service role bootstrap missing explicit dependency %q", required)
		}
	}
	for _, obsolete := range []string{
		"item.tablename ~", "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO campaign_export_worker",
	} {
		if strings.Contains(text, obsolete) {
			t.Errorf("service role bootstrap still contains broad or pattern-based grant %q", obsolete)
		}
	}
}
