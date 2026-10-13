package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportWorkerUpdateAuthorityIsLimitedToLifecycleColumns(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(wd, "..", "..", "database", "bootstrap", "002_service_roles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	start := strings.Index(text, "-- Export worker:")
	end := strings.Index(text, "-- Inbound governance worker:")
	if start < 0 || end <= start {
		t.Fatal("export worker privilege policy is missing")
	}
	policy := text[start:end]
	required := "UPDATE (status,lease_owner,lease_expires_at,attempt_count,updated_at,version,object_key,content_type,sha256,size_bytes,generated_at,expires_at,failure_code,failure_detail)"
	if !strings.Contains(policy, required) {
		t.Fatal("export worker must receive only explicit lifecycle column updates")
	}
	if strings.Contains(policy, "'SELECT, UPDATE'") || strings.Contains(policy, "'UPDATE'") {
		t.Fatal("export worker must not receive table-level UPDATE")
	}
}

func TestExportWorkerPrivilegeUpgradeConvergesWithoutRoleBootstrap(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(wd, "..", "..", "database", "migrations", "0102_export_worker_column_privileges.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"BEGIN;", "COMMIT;",
		"IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'campaign_export_worker')",
		"REVOKE ALL PRIVILEGES ON TABLE public.export_requests FROM campaign_export_worker",
		"GRANT SELECT ON TABLE public.export_requests TO campaign_export_worker",
		"GRANT UPDATE (status,lease_owner,lease_expires_at,attempt_count,updated_at,version,object_key,content_type,sha256,size_bytes,generated_at,expires_at,failure_code,failure_detail)",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("export worker privilege upgrade missing %q", required)
		}
	}
}

func TestExportWorkerCannotPromoteUnapprovedExports(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "database", "migrations", "0102_export_worker_column_privileges.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"CREATE OR REPLACE FUNCTION guard_export_worker_status_transition()",
		"current_user = 'campaign_export_worker'",
		"pg_catalog.pg_has_role(current_user, 'campaign_export_worker', 'MEMBER')",
		"OLD.approved_by IS NULL OR OLD.approved_by = OLD.requested_by",
		"ERRCODE = '42501'",
		"CREATE TRIGGER export_requests_worker_status_transition_guard",
		"BEFORE UPDATE OF status ON export_requests",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("export worker approval boundary missing %q", required)
		}
	}
	if strings.Contains(text, "SECURITY DEFINER") {
		t.Fatal("worker role detection must run with invoker authority")
	}
}

func TestExportWorkerCannotReadSourceAuthenticationEvents(t *testing.T) {
	bootstrap, err := os.ReadFile(filepath.Join("..", "..", "database", "bootstrap", "002_service_roles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(bootstrap)
	start := strings.Index(text, "-- Export worker:")
	end := strings.Index(text, "-- Inbound governance worker:")
	if start < 0 || end <= start {
		t.Fatal("export worker privilege policy is missing")
	}
	if strings.Contains(text[start:end], "'authentication_events'") {
		t.Fatal("snapshot renderer must not read live authentication event source")
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "database", "migrations", "0102_export_worker_column_privileges.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migration), "REVOKE ALL PRIVILEGES ON TABLE public.authentication_events FROM campaign_export_worker") {
		t.Fatal("upgrade must remove legacy authentication event read grants")
	}
}
