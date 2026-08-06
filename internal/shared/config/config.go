package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment                       string
	HTTPAddr                          string
	LogLevel                          string
	DatabaseURL                       string
	DatabaseDriver                    string
	DatabaseMaxOpen                   int
	DatabaseMaxIdle                   int
	DatabaseConnMaxLifetime           time.Duration
	DatabaseConnMaxIdleTime           time.Duration
	DatabasePingTimeout               time.Duration
	RedisAddr                         string
	MaxImportPreviewRows              int
	BootstrapAdminEmail               string
	BootstrapAdminPassword            string
	BootstrapAdminTOTP                string
	SecureCookies                     bool
	SessionIdleTimeout                time.Duration
	SessionAbsoluteTimeout            time.Duration
	MSISDNEncryptionKeyBase64         string
	MSISDNLookupKeyBase64             string
	IdentitySecretKeyBase64           string
	InboundContentKeyBase64           string
	InboundContentKeysJSON            string
	InboundContentActiveKey           string
	PrivacyEvidenceKeyBase64          string
	PrivacyEvidenceKeysJSON           string
	PrivacyEvidenceActiveKey          string
	InboundRetentionDays              int
	AudienceImportSourceRetentionDays int
	GatewayCallbackSecret             string
	GatewayCommandSecret              string
	GatewayCallbackMaxSkew            time.Duration
	MediaDownloadSecret               string
	ObjectStoreDriver                 string
	ObjectStoreRoot                   string
	MaxImportFileBytes                int64
	ClamAVAddress                     string
	ClamAVDialTimeout                 time.Duration
	ClamAVScanTimeout                 time.Duration
	AllowedNetworkCIDRs               []netip.Prefix
	TrustedProxyCIDRs                 []netip.Prefix
	OptOutKeywords                    []string
	MessageAllowedHosts               []string
}

// Load parses configuration without silently replacing malformed values with
// defaults. An absent value may use a documented default; an explicitly invalid
// value is always rejected so deployed services fail closed.
func Load() (Config, error) {
	environment := strings.ToLower(strings.TrimSpace(envOrDefault("APP_ENV", "development")))
	callbackSecret := strings.TrimSpace(os.Getenv("GATEWAY_CALLBACK_SECRET"))
	commandSecret := strings.TrimSpace(os.Getenv("GATEWAY_COMMAND_SECRET"))
	mediaDownloadSecret := strings.TrimSpace(os.Getenv("MEDIA_DOWNLOAD_SECRET"))
	objectStoreRoot := strings.TrimSpace(os.Getenv("OBJECT_STORE_ROOT"))
	if objectStoreRoot == "" && environment == "development" {
		objectStoreRoot = "./var/objects"
	}
	if callbackSecret == "" && environment == "development" {
		callbackSecret = "development-gateway-callback-secret-change-me"
	}
	if commandSecret == "" && environment == "development" {
		commandSecret = "development-gateway-command-secret-change-me"
	}
	if mediaDownloadSecret == "" && environment == "development" {
		mediaDownloadSecret = "development-media-download-secret-change-me"
	}

	maxPreview, err := intEnv("MAX_IMPORT_PREVIEW_ROWS", 100_000)
	if err != nil {
		return Config{}, err
	}
	dbMaxOpen, err := intEnv("DATABASE_MAX_OPEN", 30)
	if err != nil {
		return Config{}, err
	}
	dbMaxIdle, err := intEnv("DATABASE_MAX_IDLE", 10)
	if err != nil {
		return Config{}, err
	}
	dbLifetime, err := durationEnv("DATABASE_CONN_MAX_LIFETIME", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	dbIdle, err := durationEnv("DATABASE_CONN_MAX_IDLE_TIME", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	dbPing, err := durationEnv("DATABASE_PING_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	secureCookies, err := boolEnv("SECURE_COOKIES", environment != "development")
	if err != nil {
		return Config{}, err
	}
	idle, err := durationEnv("SESSION_IDLE_TIMEOUT", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	absolute, err := durationEnv("SESSION_ABSOLUTE_TIMEOUT", 12*time.Hour)
	if err != nil {
		return Config{}, err
	}
	callbackSkew, err := durationEnv("GATEWAY_CALLBACK_MAX_SKEW", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	maxImportBytes, err := int64Env("MAX_IMPORT_FILE_BYTES", 512<<20)
	if err != nil {
		return Config{}, err
	}
	clamDial, err := durationEnv("CLAMAV_DIAL_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	clamScan, err := durationEnv("CLAMAV_SCAN_TIMEOUT", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	allowedNetworks, err := prefixListEnv("ALLOWED_NETWORK_CIDRS")
	if err != nil {
		return Config{}, err
	}
	trustedProxies, err := prefixListEnv("TRUSTED_PROXY_CIDRS")
	if err != nil {
		return Config{}, err
	}
	inboundRetentionDays, err := intEnv("INBOUND_CONTENT_RETENTION_DAYS", 90)
	if err != nil {
		return Config{}, err
	}
	importSourceRetentionDays, err := intEnv("AUDIENCE_IMPORT_SOURCE_RETENTION_DAYS", 30)
	if err != nil {
		return Config{}, err
	}
	optOutKeywords, err := stringListEnv("OPT_OUT_KEYWORDS", []string{"STOP", "UNSUBSCRIBE", "CANCEL", "END", "QUIT", "OPTOUT", "OPT OUT"})
	if err != nil {
		return Config{}, err
	}
	messageAllowedHosts, err := stringListEnv("MESSAGE_ALLOWED_HOSTS", nil)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Environment:                       environment,
		HTTPAddr:                          strings.TrimSpace(envOrDefault("HTTP_ADDR", ":8080")),
		LogLevel:                          strings.ToLower(strings.TrimSpace(envOrDefault("LOG_LEVEL", "info"))),
		DatabaseURL:                       strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DatabaseDriver:                    strings.TrimSpace(envOrDefault("DATABASE_DRIVER", "pgx")),
		DatabaseMaxOpen:                   dbMaxOpen,
		DatabaseMaxIdle:                   dbMaxIdle,
		DatabaseConnMaxLifetime:           dbLifetime,
		DatabaseConnMaxIdleTime:           dbIdle,
		DatabasePingTimeout:               dbPing,
		RedisAddr:                         strings.TrimSpace(envOrDefault("REDIS_ADDR", "localhost:6379")),
		MaxImportPreviewRows:              maxPreview,
		BootstrapAdminEmail:               strings.TrimSpace(envOrDefault("BOOTSTRAP_ADMIN_EMAIL", "admin@example.test")),
		BootstrapAdminPassword:            os.Getenv("BOOTSTRAP_ADMIN_PASSWORD"),
		BootstrapAdminTOTP:                strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_TOTP_SECRET")),
		SecureCookies:                     secureCookies,
		SessionIdleTimeout:                idle,
		SessionAbsoluteTimeout:            absolute,
		MSISDNEncryptionKeyBase64:         strings.TrimSpace(os.Getenv("MSISDN_ENCRYPTION_KEY_BASE64")),
		MSISDNLookupKeyBase64:             strings.TrimSpace(os.Getenv("MSISDN_LOOKUP_KEY_BASE64")),
		IdentitySecretKeyBase64:           strings.TrimSpace(os.Getenv("IDENTITY_SECRET_KEY_BASE64")),
		InboundContentKeyBase64:           strings.TrimSpace(os.Getenv("INBOUND_CONTENT_KEY_BASE64")),
		InboundContentKeysJSON:            strings.TrimSpace(os.Getenv("INBOUND_CONTENT_KEYS_JSON")),
		InboundContentActiveKey:           strings.TrimSpace(envOrDefault("INBOUND_CONTENT_ACTIVE_KEY_VERSION", "v1")),
		PrivacyEvidenceKeyBase64:          strings.TrimSpace(os.Getenv("PRIVACY_EVIDENCE_KEY_BASE64")),
		PrivacyEvidenceKeysJSON:           strings.TrimSpace(os.Getenv("PRIVACY_EVIDENCE_KEYS_JSON")),
		PrivacyEvidenceActiveKey:          strings.TrimSpace(envOrDefault("PRIVACY_EVIDENCE_ACTIVE_KEY_VERSION", "v1")),
		InboundRetentionDays:              inboundRetentionDays,
		AudienceImportSourceRetentionDays: importSourceRetentionDays,
		GatewayCallbackSecret:             callbackSecret,
		GatewayCommandSecret:              commandSecret,
		GatewayCallbackMaxSkew:            callbackSkew,
		MediaDownloadSecret:               mediaDownloadSecret,
		ObjectStoreDriver:                 strings.ToLower(strings.TrimSpace(envOrDefault("OBJECT_STORE_DRIVER", "filesystem"))),
		ObjectStoreRoot:                   objectStoreRoot,
		MaxImportFileBytes:                maxImportBytes,
		ClamAVAddress:                     strings.TrimSpace(os.Getenv("CLAMAV_ADDRESS")),
		ClamAVDialTimeout:                 clamDial,
		ClamAVScanTimeout:                 clamScan,
		AllowedNetworkCIDRs:               allowedNetworks,
		TrustedProxyCIDRs:                 trustedProxies,
		OptOutKeywords:                    optOutKeywords,
		MessageAllowedHosts:               messageAllowedHosts,
	}
	if cfg.BootstrapAdminPassword == "" && environment == "development" {
		cfg.BootstrapAdminPassword = "development-only-password-change-me"
	}
	if cfg.BootstrapAdminTOTP == "" && environment == "development" {
		cfg.BootstrapAdminTOTP = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	production := c.Environment == "production" || c.Environment == "staging"
	switch c.Environment {
	case "development", "test", "staging", "production":
	default:
		return fmt.Errorf("APP_ENV %q is unsupported", c.Environment)
	}
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return errors.New("HTTP_ADDR is required")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL %q is unsupported", c.LogLevel)
	}
	if c.MaxImportPreviewRows <= 0 || c.MaxImportPreviewRows > 2_000_000 {
		return errors.New("MAX_IMPORT_PREVIEW_ROWS must be between 1 and 2000000")
	}
	if c.AudienceImportSourceRetentionDays < 1 || c.AudienceImportSourceRetentionDays > 3650 {
		return errors.New("AUDIENCE_IMPORT_SOURCE_RETENTION_DAYS must be between 1 and 3650")
	}
	if c.ObjectStoreDriver != "filesystem" && c.ObjectStoreDriver != "s3" && c.ObjectStoreDriver != "minio" {
		return errors.New("OBJECT_STORE_DRIVER must be filesystem or s3")
	}
	if strings.TrimSpace(c.DatabaseDriver) == "" {
		return errors.New("DATABASE_DRIVER is required")
	}
	if c.DatabaseMaxOpen <= 0 || c.DatabaseMaxOpen > 500 {
		return errors.New("DATABASE_MAX_OPEN must be between 1 and 500")
	}
	if c.DatabaseMaxIdle < 0 || c.DatabaseMaxIdle > c.DatabaseMaxOpen {
		return errors.New("DATABASE_MAX_IDLE must be between 0 and DATABASE_MAX_OPEN")
	}
	if c.DatabaseConnMaxLifetime <= 0 || c.DatabaseConnMaxIdleTime <= 0 || c.DatabasePingTimeout <= 0 || c.DatabasePingTimeout > 30*time.Second {
		return errors.New("database connection settings are invalid")
	}

	if _, err := mail.ParseAddress(c.BootstrapAdminEmail); err != nil {
		return errors.New("BOOTSTRAP_ADMIN_EMAIL is invalid")
	}
	if c.SessionIdleTimeout <= 0 || c.SessionAbsoluteTimeout <= c.SessionIdleTimeout {
		return errors.New("session timeouts are invalid")
	}
	if len(c.GatewayCallbackSecret) > 0 && len(c.GatewayCallbackSecret) < 32 {
		return errors.New("GATEWAY_CALLBACK_SECRET must contain at least 32 characters")
	}
	if len(c.GatewayCommandSecret) > 0 && len(c.GatewayCommandSecret) < 32 {
		return errors.New("GATEWAY_COMMAND_SECRET must contain at least 32 characters")
	}
	if len(c.MediaDownloadSecret) > 0 && len(c.MediaDownloadSecret) < 32 {
		return errors.New("MEDIA_DOWNLOAD_SECRET must contain at least 32 characters")
	}
	if c.GatewayCallbackMaxSkew <= 0 || c.GatewayCallbackMaxSkew > 15*time.Minute {
		return errors.New("GATEWAY_CALLBACK_MAX_SKEW must be positive and no more than 15 minutes")
	}
	if c.MaxImportFileBytes <= 0 || c.MaxImportFileBytes > 2<<30 {
		return errors.New("MAX_IMPORT_FILE_BYTES must be positive and no more than 2 GiB")
	}
	if c.ClamAVDialTimeout <= 0 || c.ClamAVScanTimeout <= c.ClamAVDialTimeout {
		return errors.New("ClamAV timeouts are invalid")
	}
	if c.ClamAVAddress != "" {
		if _, _, err := net.SplitHostPort(c.ClamAVAddress); err != nil {
			return errors.New("CLAMAV_ADDRESS must be in host:port form")
		}
	}
	if err := validateKey("MSISDN_ENCRYPTION_KEY_BASE64", c.MSISDNEncryptionKeyBase64, 32, false); err != nil {
		return err
	}
	if err := validateKey("MSISDN_LOOKUP_KEY_BASE64", c.MSISDNLookupKeyBase64, 32, true); err != nil {
		return err
	}
	if err := validateKey("IDENTITY_SECRET_KEY_BASE64", c.IdentitySecretKeyBase64, 32, false); err != nil {
		return err
	}
	if err := validateKey("INBOUND_CONTENT_KEY_BASE64", c.InboundContentKeyBase64, 32, false); err != nil {
		return err
	}
	if c.InboundRetentionDays < 1 || c.InboundRetentionDays > 3650 {
		return errors.New("INBOUND_CONTENT_RETENTION_DAYS must be between 1 and 3650")
	}
	if c.InboundContentKeysJSON != "" {
		var values map[string]string
		if err := json.Unmarshal([]byte(c.InboundContentKeysJSON), &values); err != nil {
			return errors.New("INBOUND_CONTENT_KEYS_JSON must be valid JSON")
		}
		if len(values) == 0 || strings.TrimSpace(c.InboundContentActiveKey) == "" {
			return errors.New("inbound content keyring requires keys and an active version")
		}
		if _, ok := values[c.InboundContentActiveKey]; !ok {
			return errors.New("active inbound content key version is not configured")
		}
		for version, value := range values {
			if strings.TrimSpace(version) == "" {
				return errors.New("inbound content key version is required")
			}
			if err := validateKey("INBOUND_CONTENT_KEYS_JSON["+version+"]", value, 32, false); err != nil {
				return err
			}
		}
	}
	if err := validateKey("PRIVACY_EVIDENCE_KEY_BASE64", c.PrivacyEvidenceKeyBase64, 32, false); err != nil {
		return err
	}
	if err := validateVersionedKeyring("privacy evidence", "PRIVACY_EVIDENCE_KEYS_JSON", c.PrivacyEvidenceKeysJSON, c.PrivacyEvidenceActiveKey); err != nil {
		return err
	}

	if production {
		if strings.Contains(c.BootstrapAdminPassword, "development-only") || len(c.BootstrapAdminPassword) < 20 {
			return errors.New("production requires a strong BOOTSTRAP_ADMIN_PASSWORD")
		}
		if strings.TrimSpace(c.BootstrapAdminTOTP) == "" || strings.Contains(strings.ToLower(c.BootstrapAdminTOTP), "replace") {
			return errors.New("production requires BOOTSTRAP_ADMIN_TOTP_SECRET")
		}
		if !c.SecureCookies {
			return errors.New("production requires SECURE_COOKIES=true")
		}
		if strings.TrimSpace(c.DatabaseURL) == "" {
			return errors.New("production requires DATABASE_URL")
		}
		if strings.TrimSpace(c.RedisAddr) == "" {
			return errors.New("production requires REDIS_ADDR")
		}
		if c.MSISDNEncryptionKeyBase64 == "" || c.MSISDNLookupKeyBase64 == "" {
			return errors.New("production requires MSISDN encryption and lookup keys")
		}
		if c.IdentitySecretKeyBase64 == "" {
			return errors.New("production requires IDENTITY_SECRET_KEY_BASE64")
		}
		if c.InboundContentKeyBase64 == "" && c.InboundContentKeysJSON == "" {
			return errors.New("production requires an inbound content encryption key or keyring")
		}
		if c.PrivacyEvidenceKeyBase64 == "" && c.PrivacyEvidenceKeysJSON == "" {
			return errors.New("production requires a privacy evidence encryption key or keyring")
		}
		if len(c.GatewayCallbackSecret) < 32 || strings.Contains(c.GatewayCallbackSecret, "development-") || strings.Contains(strings.ToLower(c.GatewayCallbackSecret), "change-me") {
			return errors.New("production requires a strong GATEWAY_CALLBACK_SECRET")
		}
		if len(c.MediaDownloadSecret) < 32 || strings.Contains(c.MediaDownloadSecret, "development-") || strings.Contains(strings.ToLower(c.MediaDownloadSecret), "change-me") {
			return errors.New("production requires a strong MEDIA_DOWNLOAD_SECRET")
		}
		switch c.ObjectStoreDriver {
		case "filesystem":
			if c.ObjectStoreRoot == "" || !filepath.IsAbs(c.ObjectStoreRoot) {
				return errors.New("filesystem object storage requires an absolute OBJECT_STORE_ROOT in production")
			}
		case "s3", "minio":
		default:
			return errors.New("OBJECT_STORE_DRIVER must be filesystem or s3")
		}
		if c.ClamAVAddress == "" {
			return errors.New("production requires CLAMAV_ADDRESS")
		}
	}
	return nil
}

func validateVersionedKeyring(label, envName, value, active string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(value), &values); err != nil {
		return fmt.Errorf("%s must be valid JSON", envName)
	}
	if len(values) == 0 || strings.TrimSpace(active) == "" {
		return fmt.Errorf("%s keyring requires keys and an active version", label)
	}
	if _, ok := values[active]; !ok {
		return fmt.Errorf("active %s key version is not configured", label)
	}
	for version, encoded := range values {
		if strings.TrimSpace(version) == "" {
			return fmt.Errorf("%s key version is required", label)
		}
		if err := validateKey(envName+"["+version+"]", encoded, 32, false); err != nil {
			return err
		}
	}
	return nil
}

func validateKey(name, value string, exact int, minimum bool) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return fmt.Errorf("%s must be valid standard base64", name)
	}
	if minimum {
		if len(decoded) < exact {
			return fmt.Errorf("%s must decode to at least %d bytes", name, exact)
		}
		return nil
	}
	if len(decoded) != exact {
		return fmt.Errorf("%s must decode to exactly %d bytes", name, exact)
	}
	return nil
}

func stringListEnv(key string, fallback []string) ([]string, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return append([]string(nil), fallback...), nil
	}
	seen := map[string]struct{}{}
	values := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		value := strings.Join(strings.Fields(strings.ToUpper(strings.TrimSpace(part))), " ")
		if value == "" {
			return nil, fmt.Errorf("%s contains an empty value", key)
		}
		if len(value) > 64 {
			return nil, fmt.Errorf("%s contains a value longer than 64 characters", key)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	if len(values) == 0 || len(values) > 100 {
		return nil, fmt.Errorf("%s must contain between 1 and 100 values", key)
	}
	return values, nil
}
func prefixListEnv(key string) ([]netip.Prefix, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	result := make([]netip.Prefix, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("%s must contain valid comma-separated CIDR prefixes", key)
		}
		prefix = prefix.Masked()
		canonical := prefix.String()
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, prefix)
	}
	return result, nil
}

func envOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func intEnv(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}

func boolEnv(key string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return parsed, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration", key)
	}
	return parsed, nil
}

func int64Env(key string, fallback int64) (int64, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a 64-bit integer", key)
	}
	return parsed, nil
}
