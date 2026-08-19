package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestR14LiveCapacityAuthorityMigration(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(migrationDirectory(t), "0088_r14_live_capacity_authority_hardening.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"trg_capacity_reservation_no_truncate",
		"BEFORE TRUNCATE ON campaign_pool_capacity_reservations",
		"R14_LIVE_CAPACITY_AUTHORITY_HARDENING",
		"source_migration",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("0088 live capacity hardening missing %q", required)
		}
	}
}
