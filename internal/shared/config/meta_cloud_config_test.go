package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMetaCloudCredentialsFromOptionalSecretFile(t *testing.T) {
	setDevelopmentEnvironment(t)
	raw := `[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"0123456789abcdef0123456789abcdef","verifyToken":"verify-token-123456"}]`
	path := filepath.Join(t.TempDir(), "meta-credentials.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("META_CLOUD_CREDENTIALS_JSON", "")
	t.Setenv("META_CLOUD_CREDENTIALS_JSON_FILE", path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetaCloudCredentialsJSON != raw {
		t.Fatalf("Meta credentials were not resolved from the secret file")
	}
}

func TestLoadWithoutMetaCloudCredentialsRemainsValid(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("META_CLOUD_CREDENTIALS_JSON", "")
	t.Setenv("META_CLOUD_CREDENTIALS_JSON_FILE", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetaCloudCredentialsJSON != "" {
		t.Fatal("Meta credentials unexpectedly configured")
	}
}

func TestMetaCloudConversationWindowConfiguration(t *testing.T) {
	setDevelopmentEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetaConversationWindow != 24*time.Hour {
		t.Fatalf("default Meta conversation window=%s want=24h", cfg.MetaConversationWindow)
	}
	t.Setenv("META_CLOUD_CONVERSATION_WINDOW", "12h")
	cfg, err = Load()
	if err != nil || cfg.MetaConversationWindow != 12*time.Hour {
		t.Fatalf("configured Meta conversation window=%s err=%v", cfg.MetaConversationWindow, err)
	}
	for _, invalid := range []string{"0s", "25h"} {
		t.Setenv("META_CLOUD_CONVERSATION_WINDOW", invalid)
		if _, err := Load(); err == nil {
			t.Fatalf("invalid Meta conversation window %q was accepted", invalid)
		}
	}
}

func TestMetaCloudHealthStaleAfterConfiguration(t *testing.T) {
	setDevelopmentEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetaHealthStaleAfter != 5*time.Minute {
		t.Fatalf("default Meta health window=%s want=5m", cfg.MetaHealthStaleAfter)
	}
	t.Setenv("META_CLOUD_HEALTH_STALE_AFTER", "7m")
	cfg, err = Load()
	if err != nil || cfg.MetaHealthStaleAfter != 7*time.Minute {
		t.Fatalf("configured Meta health window=%s err=%v", cfg.MetaHealthStaleAfter, err)
	}
	t.Setenv("META_CLOUD_HEALTH_STALE_AFTER", "0s")
	if _, err := Load(); err == nil {
		t.Fatal("zero Meta health staleness window was accepted")
	}
}
