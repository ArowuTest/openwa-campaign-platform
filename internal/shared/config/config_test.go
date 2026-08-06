package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoadRejectsExplicitMalformedValues(t *testing.T) {
	setDevelopmentEnvironment(t)
	t.Setenv("SECURE_COOKIES", "sometimes")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SECURE_COOKIES") {
		t.Fatalf("expected strict boolean parse error, got %v", err)
	}

	setDevelopmentEnvironment(t)
	t.Setenv("SESSION_IDLE_TIMEOUT", "thirty")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SESSION_IDLE_TIMEOUT") {
		t.Fatalf("expected strict duration parse error, got %v", err)
	}

	setDevelopmentEnvironment(t)
	t.Setenv("MAX_IMPORT_FILE_BYTES", "large")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MAX_IMPORT_FILE_BYTES") {
		t.Fatalf("expected strict integer parse error, got %v", err)
	}
}

func TestLoadDevelopmentDefaultsAreSafeAndValid(t *testing.T) {
	setDevelopmentEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != "development" || cfg.MaxImportPreviewRows != 100000 {
		t.Fatalf("unexpected configuration: %#v", cfg)
	}
	if len(cfg.GatewayCallbackSecret) < 32 {
		t.Fatal("development callback secret must still satisfy the cryptographic minimum")
	}
}

func TestLoadProductionFailsClosedAndAcceptsStrongConfiguration(t *testing.T) {
	setProductionEnvironment(t)
	if _, err := Load(); err != nil {
		t.Fatalf("strong production configuration rejected: %v", err)
	}

	setProductionEnvironment(t)
	t.Setenv("GATEWAY_CALLBACK_SECRET", "change-me-to-a-random-secret-at-least-32-characters")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GATEWAY_CALLBACK_SECRET") {
		t.Fatalf("expected placeholder callback secret rejection, got %v", err)
	}

	setProductionEnvironment(t)
	t.Setenv("MSISDN_ENCRYPTION_KEY_BASE64", base64.StdEncoding.EncodeToString(make([]byte, 31)))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "exactly 32 bytes") {
		t.Fatalf("expected invalid encryption key rejection, got %v", err)
	}
}

func TestValidateRejectsUnsafeBounds(t *testing.T) {
	setDevelopmentEnvironment(t)
	t.Setenv("MAX_IMPORT_PREVIEW_ROWS", "2000001")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MAX_IMPORT_PREVIEW_ROWS") {
		t.Fatalf("expected preview bound rejection, got %v", err)
	}

	setDevelopmentEnvironment(t)
	t.Setenv("GATEWAY_CALLBACK_MAX_SKEW", "16m")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GATEWAY_CALLBACK_MAX_SKEW") {
		t.Fatalf("expected callback skew rejection, got %v", err)
	}
}

func setDevelopmentEnvironment(t *testing.T) {
	t.Helper()
	values := map[string]string{
		"APP_ENV": "development", "HTTP_ADDR": ":8080", "LOG_LEVEL": "info",
		"DATABASE_URL": "", "DATABASE_DRIVER": "pgx", "DATABASE_MAX_OPEN": "30", "DATABASE_MAX_IDLE": "10", "DATABASE_CONN_MAX_LIFETIME": "30m", "DATABASE_CONN_MAX_IDLE_TIME": "5m", "DATABASE_PING_TIMEOUT": "5s", "REDIS_ADDR": "localhost:6379", "MAX_IMPORT_PREVIEW_ROWS": "100000",
		"BOOTSTRAP_ADMIN_EMAIL": "admin@example.test", "BOOTSTRAP_ADMIN_PASSWORD": "development-only-password-change-me",
		"BOOTSTRAP_ADMIN_TOTP_SECRET": "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", "SECURE_COOKIES": "false",
		"SESSION_IDLE_TIMEOUT": "30m", "SESSION_ABSOLUTE_TIMEOUT": "12h",
		"MSISDN_ENCRYPTION_KEY_BASE64": "", "MSISDN_LOOKUP_KEY_BASE64": "", "IDENTITY_SECRET_KEY_BASE64": "", "INBOUND_CONTENT_KEY_BASE64": "", "INBOUND_CONTENT_KEYS_JSON": "", "INBOUND_CONTENT_ACTIVE_KEY_VERSION": "v1", "PRIVACY_EVIDENCE_KEY_BASE64": "", "PRIVACY_EVIDENCE_KEYS_JSON": "", "PRIVACY_EVIDENCE_ACTIVE_KEY_VERSION": "v1", "INBOUND_CONTENT_RETENTION_DAYS": "90",
		"GATEWAY_CALLBACK_SECRET": "development-gateway-callback-secret-change-me", "GATEWAY_CALLBACK_MAX_SKEW": "5m", "MEDIA_DOWNLOAD_SECRET": "development-media-download-secret-change-me",
		"OBJECT_STORE_ROOT": t.TempDir(), "MAX_IMPORT_FILE_BYTES": "536870912", "CLAMAV_ADDRESS": "",
		"CLAMAV_DIAL_TIMEOUT": "5s", "CLAMAV_SCAN_TIMEOUT": "2m",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
}

func setProductionEnvironment(t *testing.T) {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	lookup := base64.StdEncoding.EncodeToString(make([]byte, 48))
	values := map[string]string{
		"APP_ENV": "production", "HTTP_ADDR": ":8080", "LOG_LEVEL": "info",
		"DATABASE_URL": "postgres://campaign:secret@postgres:5432/campaign?sslmode=require", "DATABASE_DRIVER": "pgx", "DATABASE_MAX_OPEN": "30", "DATABASE_MAX_IDLE": "10", "DATABASE_CONN_MAX_LIFETIME": "30m", "DATABASE_CONN_MAX_IDLE_TIME": "5m", "DATABASE_PING_TIMEOUT": "5s", "REDIS_ADDR": "redis:6379",
		"MAX_IMPORT_PREVIEW_ROWS": "100000", "BOOTSTRAP_ADMIN_EMAIL": "admin@example.test",
		"BOOTSTRAP_ADMIN_PASSWORD": "a-strong-bootstrap-password-12345", "BOOTSTRAP_ADMIN_TOTP_SECRET": "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
		"SECURE_COOKIES": "true", "SESSION_IDLE_TIMEOUT": "30m", "SESSION_ABSOLUTE_TIMEOUT": "12h",
		"MSISDN_ENCRYPTION_KEY_BASE64": key, "MSISDN_LOOKUP_KEY_BASE64": lookup, "IDENTITY_SECRET_KEY_BASE64": key, "INBOUND_CONTENT_KEY_BASE64": key, "INBOUND_CONTENT_KEYS_JSON": "", "INBOUND_CONTENT_ACTIVE_KEY_VERSION": "v1", "PRIVACY_EVIDENCE_KEY_BASE64": key, "PRIVACY_EVIDENCE_KEYS_JSON": "", "PRIVACY_EVIDENCE_ACTIVE_KEY_VERSION": "v1", "INBOUND_CONTENT_RETENTION_DAYS": "90",
		"GATEWAY_CALLBACK_SECRET": "c7c4d58cba7d4f03a2234cc45891d7f89c8247b4c80a4d01", "GATEWAY_CALLBACK_MAX_SKEW": "5m", "MEDIA_DOWNLOAD_SECRET": "9919af30f33e43db82bdc17c7e8323d1f5e6627f4eb44554",
		"OBJECT_STORE_ROOT": t.TempDir(), "MAX_IMPORT_FILE_BYTES": "536870912", "CLAMAV_ADDRESS": "clamav:3310",
		"CLAMAV_DIAL_TIMEOUT": "5s", "CLAMAV_SCAN_TIMEOUT": "2m",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
}

func TestLoadNetworkCIDRsAreStrictAndCanonical(t *testing.T) {
	setDevelopmentEnvironment(t)
	t.Setenv("ALLOWED_NETWORK_CIDRS", "10.0.0.15/8, 2001:db8::1/32")
	t.Setenv("TRUSTED_PROXY_CIDRS", "192.0.2.0/24")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowedNetworkCIDRs) != 2 || cfg.AllowedNetworkCIDRs[0].String() != "10.0.0.0/8" || cfg.TrustedProxyCIDRs[0].String() != "192.0.2.0/24" {
		t.Fatalf("unexpected network policy: %#v %#v", cfg.AllowedNetworkCIDRs, cfg.TrustedProxyCIDRs)
	}

	setDevelopmentEnvironment(t)
	t.Setenv("ALLOWED_NETWORK_CIDRS", "10.0.0.0/8,not-a-cidr")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ALLOWED_NETWORK_CIDRS") {
		t.Fatalf("expected strict CIDR error, got %v", err)
	}
}

func TestLoadOptOutKeywordsAreStrictAndCanonical(t *testing.T) {
	setDevelopmentEnvironment(t)
	t.Setenv("OPT_OUT_KEYWORDS", " stop , opt   out, STOP, arrêter ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.OptOutKeywords) != 3 || cfg.OptOutKeywords[0] != "STOP" || cfg.OptOutKeywords[1] != "OPT OUT" || cfg.OptOutKeywords[2] != "ARRÊTER" {
		t.Fatalf("unexpected opt-out keywords: %#v", cfg.OptOutKeywords)
	}

	setDevelopmentEnvironment(t)
	t.Setenv("OPT_OUT_KEYWORDS", "STOP,,QUIT")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "OPT_OUT_KEYWORDS") {
		t.Fatalf("expected strict opt-out keyword error, got %v", err)
	}
}

func TestLoadVersionedInboundContentKeyring(t *testing.T) {
	setProductionEnvironment(t)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("INBOUND_CONTENT_KEY_BASE64", "")
	t.Setenv("INBOUND_CONTENT_KEYS_JSON", `{"v1":"`+key+`","v2":"`+key+`"}`)
	t.Setenv("INBOUND_CONTENT_ACTIVE_KEY_VERSION", "v2")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InboundContentActiveKey != "v2" {
		t.Fatalf("active=%s", cfg.InboundContentActiveKey)
	}
}
