package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadProductionRejectsAbsentRedisAddress(t *testing.T) {
	setProductionEnvironment(t)
	if err := os.Unsetenv("REDIS_ADDR"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "REDIS_ADDR") {
		t.Fatalf("production accepted absent REDIS_ADDR: %v", err)
	}
}

func TestLoadProductionRejectsDevelopmentBootstrapTOTP(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("BOOTSTRAP_ADMIN_TOTP_SECRET", "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "BOOTSTRAP_ADMIN_TOTP_SECRET") {
		t.Fatalf("production accepted shipped development TOTP secret: %v", err)
	}
}

func TestLoadProductionRejectsDevelopmentBootstrapEmail(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "admin@example.test")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "BOOTSTRAP_ADMIN_EMAIL") {
		t.Fatalf("production accepted development bootstrap email: %v", err)
	}
}

func TestLoadRejectsMalformedMetaCloudCredentialsWhenConfigured(t *testing.T) {
	setDevelopmentEnvironment(t)
	t.Setenv("META_CLOUD_CREDENTIALS_JSON", "not-json")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "META_CLOUD_CREDENTIALS_JSON") {
		t.Fatalf("malformed Meta credentials were accepted: %v", err)
	}
}

func TestLoadObjectStoreDriverErrorMentionsMinio(t *testing.T) {
	setDevelopmentEnvironment(t)
	t.Setenv("OBJECT_STORE_DRIVER", "bogus")
	if _, err := Load(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "minio") {
		t.Fatalf("object-store allowlist error omitted minio: %v", err)
	}
}
