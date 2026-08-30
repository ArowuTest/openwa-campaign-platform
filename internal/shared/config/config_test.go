package config

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
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

func TestLoadProductionRejectsCaseVariantsOfDevelopmentMediaSecret(t *testing.T) {
	for _, secret := range []string{
		"Development-media-download-secret-not-for-production",
		"DEVELOPMENT-media-download-secret-not-for-production",
	} {
		t.Run(secret[:11], func(t *testing.T) {
			setProductionEnvironment(t)
			t.Setenv("MEDIA_DOWNLOAD_SECRET", secret)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MEDIA_DOWNLOAD_SECRET") {
				t.Fatalf("production accepted development placeholder media secret %q: %v", secret, err)
			}
		})
	}
}

func TestLoadGatewaySecretRotationRequiresDistinctStrongPreviousSecrets(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("GATEWAY_CALLBACK_SECRET_PREVIOUS", "8a3487bb4f3241f4b122e6bd4bc55bdf9fe32fac")
	t.Setenv("GATEWAY_COMMAND_SECRET_PREVIOUS", "5227e0c4710e4578b139ffae0bb64aac2c9fb590")
	t.Setenv("GATEWAY_RUNTIME_SECRET_PREVIOUS", "ac3f48793c0844e89653e1f25b778376a4e7dd69")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("valid rotation configuration rejected: %v", err)
	}
	if cfg.GatewayCallbackPreviousSecret == "" || cfg.GatewayCommandPreviousSecret == "" || cfg.GatewayRuntimePreviousSecret == "" {
		t.Fatal("previous secrets were not loaded")
	}

	setProductionEnvironment(t)
	t.Setenv("GATEWAY_COMMAND_SECRET_PREVIOUS", os.Getenv("GATEWAY_COMMAND_SECRET"))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("expected duplicate active/previous rejection, got %v", err)
	}

	setProductionEnvironment(t)
	t.Setenv("GATEWAY_RUNTIME_SECRET_PREVIOUS", "too-short")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "at least 32") {
		t.Fatalf("expected weak previous secret rejection, got %v", err)
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
		"DATABASE_URL": "", "DATABASE_DRIVER": "postgres", "DATABASE_MAX_OPEN": "30", "DATABASE_MAX_IDLE": "10", "DATABASE_CONN_MAX_LIFETIME": "30m", "DATABASE_CONN_MAX_IDLE_TIME": "5m", "DATABASE_PING_TIMEOUT": "5s", "REDIS_ADDR": "localhost:6379", "MAX_IMPORT_PREVIEW_ROWS": "100000",
		"BOOTSTRAP_ADMIN_EMAIL": "admin@example.test", "BOOTSTRAP_ADMIN_PASSWORD": "development-only-password-change-me",
		"BOOTSTRAP_ADMIN_TOTP_SECRET": "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", "SECURE_COOKIES": "false",
		"SESSION_IDLE_TIMEOUT": "30m", "SESSION_ABSOLUTE_TIMEOUT": "12h",
		"MSISDN_ENCRYPTION_KEY_BASE64": "", "MSISDN_LOOKUP_KEY_BASE64": "", "IDENTITY_SECRET_KEY_BASE64": "", "INBOUND_CONTENT_KEY_BASE64": "", "INBOUND_CONTENT_KEYS_JSON": "", "INBOUND_CONTENT_ACTIVE_KEY_VERSION": "v1", "PRIVACY_EVIDENCE_KEY_BASE64": "", "PRIVACY_EVIDENCE_KEYS_JSON": "", "PRIVACY_EVIDENCE_ACTIVE_KEY_VERSION": "v1", "SENDER_PROXY_KEYS_JSON": "", "SENDER_PROXY_ACTIVE_KEY_VERSION": "v1", "INBOUND_CONTENT_RETENTION_DAYS": "90",
		"GATEWAY_CALLBACK_SECRET": "development-gateway-callback-secret-change-me", "GATEWAY_COMMAND_SECRET": "development-gateway-command-secret-change-me", "GATEWAY_RUNTIME_SECRET": "development-gateway-runtime-secret-change-me", "GATEWAY_CALLBACK_MAX_SKEW": "5m", "MEDIA_DOWNLOAD_SECRET": "development-media-download-secret-change-me",
		"OBJECT_STORE_ROOT": t.TempDir(), "MAX_IMPORT_FILE_BYTES": "536870912", "CLAMAV_ADDRESS": "",
		"CLAMAV_DIAL_TIMEOUT": "5s", "CLAMAV_SCAN_TIMEOUT": "2m",
		"ALLOWED_NETWORK_CIDRS": "10.0.0.0/8", "TRUSTED_PROXY_CIDRS": "10.0.0.0/8",
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
		"DATABASE_URL": "postgres://campaign:secret@postgres:5432/campaign?sslmode=require", "DATABASE_DRIVER": "postgres", "DATABASE_MAX_OPEN": "30", "DATABASE_MAX_IDLE": "10", "DATABASE_CONN_MAX_LIFETIME": "30m", "DATABASE_CONN_MAX_IDLE_TIME": "5m", "DATABASE_PING_TIMEOUT": "5s", "REDIS_ADDR": "redis:6379",
		"MAX_IMPORT_PREVIEW_ROWS": "100000", "BOOTSTRAP_ADMIN_EMAIL": "ops-admin@example.com",
		"BOOTSTRAP_ADMIN_PASSWORD": "a-strong-bootstrap-password-12345", "BOOTSTRAP_ADMIN_TOTP_SECRET": "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP",
		"SECURE_COOKIES": "true", "SESSION_IDLE_TIMEOUT": "30m", "SESSION_ABSOLUTE_TIMEOUT": "12h",
		"MSISDN_ENCRYPTION_KEY_BASE64": key, "MSISDN_LOOKUP_KEY_BASE64": lookup, "IDENTITY_SECRET_KEY_BASE64": key, "INBOUND_CONTENT_KEY_BASE64": key, "INBOUND_CONTENT_KEYS_JSON": "", "INBOUND_CONTENT_ACTIVE_KEY_VERSION": "v1", "PRIVACY_EVIDENCE_KEY_BASE64": key, "PRIVACY_EVIDENCE_KEYS_JSON": "", "PRIVACY_EVIDENCE_ACTIVE_KEY_VERSION": "v1", "SENDER_PROXY_KEYS_JSON": `{"v1":"` + key + `"}`, "SENDER_PROXY_ACTIVE_KEY_VERSION": "v1", "INBOUND_CONTENT_RETENTION_DAYS": "90",
		"GATEWAY_RUNTIME_ALLOWED_HOSTS": "gateway.private.example",
		"CONTROL_API_INTERNAL_URL":      "https://control.internal.example",
		"GATEWAY_CALLBACK_SECRET":       "c7c4d58cba7d4f03a2234cc45891d7f89c8247b4c80a4d01", "GATEWAY_COMMAND_SECRET": "f915536086184fa1b103e78b01abc0045cd1ed2c6cb442c0", "GATEWAY_RUNTIME_SECRET": "2de703d848e949c4a636050ec61f6441cfd0bf2ef1b443d9", "GATEWAY_CALLBACK_MAX_SKEW": "5m", "MEDIA_DOWNLOAD_SECRET": "9919af30f33e43db82bdc17c7e8323d1f5e6627f4eb44554",
		"OBJECT_STORE_ROOT": t.TempDir(), "MAX_IMPORT_FILE_BYTES": "536870912", "CLAMAV_ADDRESS": "clamav:3310",
		"CLAMAV_DIAL_TIMEOUT": "5s", "CLAMAV_SCAN_TIMEOUT": "2m",
		"ALLOWED_NETWORK_CIDRS": "10.0.0.0/8", "TRUSTED_PROXY_CIDRS": "10.0.0.0/8",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
}

func TestLoadProductionRequiresGatewayRuntimeAllowedHosts(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("GATEWAY_RUNTIME_ALLOWED_HOSTS", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GATEWAY_RUNTIME_ALLOWED_HOSTS") {
		t.Fatalf("production accepted missing governed gateway runtime hosts: %v", err)
	}
}

func TestLoadProductionRejectsGatewayControlHostnameAlias(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("CONTROL_API_INTERNAL_URL", "https://control.internal.example")
	t.Setenv("GATEWAY_RUNTIME_ALLOWED_HOSTS", "control.internal.example")
	if _, err := Load(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "control") {
		t.Fatalf("production accepted control hostname as gateway runtime authority: %v", err)
	}
}

func TestLoadDeployedRejectsNonAuthorityControlOrigin(t *testing.T) {
	for _, environment := range []string{"staging", "production"} {
		for _, raw := range []string{
			"https://deploy:secret@control.internal.example",
			"https://control.internal.example/api",
			"https://control.internal.example?token=secret",
			"https://control.internal.example#fragment",
			"https://control.internal.example:0",
			"https://control.internal.example:65536",
		} {
			t.Run(environment+"/"+raw, func(t *testing.T) {
				setProductionEnvironment(t)
				t.Setenv("APP_ENV", environment)
				t.Setenv("CONTROL_API_INTERNAL_URL", raw)
				if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CONTROL_API_INTERNAL_URL") {
					t.Fatalf("deployed config accepted non-authority control origin %q: %v", raw, err)
				}
			})
		}
	}
}

func TestLoadDeployedRejectsReservedControlAuthority(t *testing.T) {
	for _, environment := range []string{"staging", "production"} {
		for _, raw := range []string{
			"https://localhost",
			"https://localhost.",
			"https://control-api",
			"https://openwa-gateway",
			"https://host.docker.internal",
			"https://127.0.0.1",
			"https://169.254.10.20",
			"https://10.20.30.50",
			"https://172.16.0.10",
			"https://192.168.1.10",
			"https://[::1]",
			"https://[fe80::1]",
			"https://[fd00::1]",
			"https://[::ffff:10.20.30.50]",
			"https://010.020.030.050",
			"https://0x0a.0x14.0x1e.0x28",
			"https://167772161",
			"https://10.1",
		} {
			t.Run(environment+"/"+raw, func(t *testing.T) {
				setProductionEnvironment(t)
				t.Setenv("APP_ENV", environment)
				t.Setenv("CONTROL_API_INTERNAL_URL", raw)
				if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CONTROL_API_INTERNAL_URL") {
					t.Fatalf("deployed config accepted reserved/local control authority %q: %v", raw, err)
				}
			})
		}
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

func TestLoadGatewayStaleAfterSecondsIsStrictAndBounded(t *testing.T) {
	setDevelopmentEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GatewayStaleAfter != 120*time.Second {
		t.Fatalf("unexpected gateway stale default: %s", cfg.GatewayStaleAfter)
	}
	setDevelopmentEnvironment(t)
	t.Setenv("GATEWAY_STALE_AFTER_SECONDS", "180")
	cfg, err = Load()
	if err != nil || cfg.GatewayStaleAfter != 180*time.Second {
		t.Fatalf("valid gateway stale override rejected: %+v %v", cfg, err)
	}
	setDevelopmentEnvironment(t)
	t.Setenv("GATEWAY_STALE_AFTER_SECONDS", "29")
	if _, err = Load(); err == nil || !strings.Contains(err.Error(), "GATEWAY_STALE_AFTER_SECONDS") {
		t.Fatalf("expected unsafe gateway stale threshold rejection, got %v", err)
	}
}

func TestLoadGatewayPreviousSecretsFollowProductionTrustPolicy(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("GATEWAY_CALLBACK_SECRET_PREVIOUS", "change-me-to-a-random-previous-secret-at-least-32-characters")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GATEWAY_CALLBACK_SECRET_PREVIOUS") {
		t.Fatalf("expected placeholder previous callback secret rejection, got %v", err)
	}

	setProductionEnvironment(t)
	t.Setenv("GATEWAY_CALLBACK_SECRET_PREVIOUS", os.Getenv("GATEWAY_COMMAND_SECRET"))
	if _, err := Load(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "distinct") {
		t.Fatalf("expected cross-role active/previous secret rejection, got %v", err)
	}

	setProductionEnvironment(t)
	sharedPrevious := "db2d614ea4e64b65ad9a8d4c8e81bcf4d146b3f524fb4e51"
	t.Setenv("GATEWAY_CALLBACK_SECRET_PREVIOUS", sharedPrevious)
	t.Setenv("GATEWAY_COMMAND_SECRET_PREVIOUS", sharedPrevious)
	if _, err := Load(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "distinct") {
		t.Fatalf("expected cross-role previous secret rejection, got %v", err)
	}
}

func TestLoadProductionRequiresNetworkBoundaries(t *testing.T) {
	for _, name := range []string{"ALLOWED_NETWORK_CIDRS", "TRUSTED_PROXY_CIDRS"} {
		t.Run(name, func(t *testing.T) {
			setProductionEnvironment(t)
			t.Setenv("ALLOWED_NETWORK_CIDRS", "10.0.0.0/8")
			t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8")
			t.Setenv(name, "")
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("production accepted missing %s: %v", name, err)
			}
		})
	}
}

func TestLoadProductionAcceptsLongGovernedGatewayHostname(t *testing.T) {
	setProductionEnvironment(t)
	host := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.example.com"
	t.Setenv("GATEWAY_RUNTIME_ALLOWED_HOSTS", host)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("production rejected valid governed FQDN longer than 64 characters: %v", err)
	}
	if len(cfg.GatewayRuntimeAllowedHosts) != 1 || !strings.EqualFold(cfg.GatewayRuntimeAllowedHosts[0], host) {
		t.Fatalf("unexpected governed gateway hosts: %#v", cfg.GatewayRuntimeAllowedHosts)
	}
}

func TestLoadGatewayRuntimeHostsNormalizeFQDNAndRejectReservedNames(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("GATEWAY_RUNTIME_ALLOWED_HOSTS", "Gateway.Private.Example.")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("valid dotted FQDN rejected: %v", err)
	}
	if len(cfg.GatewayRuntimeAllowedHosts) != 1 || cfg.GatewayRuntimeAllowedHosts[0] != "gateway.private.example" {
		t.Fatalf("host not canonicalized: %#v", cfg.GatewayRuntimeAllowedHosts)
	}
	for _, host := range []string{"localhost.", "host.docker.internal."} {
		setProductionEnvironment(t)
		t.Setenv("GATEWAY_RUNTIME_ALLOWED_HOSTS", host)
		if _, err := Load(); err == nil {
			t.Fatalf("reserved governed host accepted: %s", host)
		}
	}
}

func TestLoadGatewayRuntimeHostsRejectIPAndLegacyNumericLiterals(t *testing.T) {
	for _, host := range []string{
		"8.8.8.8", "2001:4860:4860::8888",
		"10.20.30.40", "172.16.0.40", "192.168.1.40", "fd00::1", "::ffff:10.20.30.40",
		"010.020.030.050", "0x0a.0x14.0x1e.0x28", "167772161", "10.1",
	} {
		setProductionEnvironment(t)
		t.Setenv("GATEWAY_RUNTIME_ALLOWED_HOSTS", host)
		if _, err := Load(); err == nil {
			t.Fatalf("IP-like governed gateway host accepted: %s", host)
		}
	}
}
