package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGovernanceResolvesDatabaseAndInboundKeysFromFiles(t *testing.T) {
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "database-url")
	keysPath := filepath.Join(dir, "inbound-keys")
	if err := os.WriteFile(databasePath, []byte("postgres://governance-from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keysPath, []byte(`{"v1":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV", "test")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("INBOUND_CONTENT_KEYS_JSON", "")
	t.Setenv("DATABASE_URL_FILE", databasePath)
	t.Setenv("INBOUND_CONTENT_KEYS_JSON_FILE", keysPath)
	cfg, err := LoadGovernance()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://governance-from-file" {
		t.Fatalf("database URL was not resolved from file: %q", cfg.DatabaseURL)
	}
	if cfg.InboundContentKeysJSON == "" {
		t.Fatal("inbound content keyring was not resolved from file")
	}
}
