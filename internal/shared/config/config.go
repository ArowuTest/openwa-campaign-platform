package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/shared/envfile"
)

type Config struct {
	Environment                       string
	DataClassification                string
	HTTPAddr                          string
	LogLevel                          string
	ProfilingToken                    string
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
	SenderProxyKeysJSON               string
	SenderProxyActiveKey              string
	InboundRetentionDays              int
	AudienceImportSourceRetentionDays int
	GatewayCallbackSecret             string
	GatewayCallbackPreviousSecret     string
	GatewayCommandSecret              string
	GatewayCommandPreviousSecret      string
	GatewayRuntimeSecret              string
	GatewayRuntimePreviousSecret      string
	GatewayRuntimeAllowedHosts        []string
	ControlAPIInternalURL             string
	MetaCloudCredentialsJSON          string
	MetaHealthStaleAfter              time.Duration
	MetaConversationWindow            time.Duration
	GatewayStaleAfter                 time.Duration
	SenderHeartbeatTTL                time.Duration
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
	classification := strings.ToUpper(strings.TrimSpace(envOrDefault("DATA_CLASSIFICATION", "INTERNAL")))
	if classification == "" || len(classification) > 64 {
		return Config{}, errors.New("DATA_CLASSIFICATION must contain 1 to 64 characters")
	}
	if err := envfile.Resolve(environment, "DATABASE_URL", "BOOTSTRAP_ADMIN_PASSWORD", "BOOTSTRAP_ADMIN_TOTP_SECRET", "MSISDN_ENCRYPTION_KEY_BASE64", "MSISDN_LOOKUP_KEY_BASE64", "IDENTITY_SECRET_KEY_BASE64", "INBOUND_CONTENT_KEY_BASE64", "INBOUND_CONTENT_KEYS_JSON", "PRIVACY_EVIDENCE_KEY_BASE64", "PRIVACY_EVIDENCE_KEYS_JSON", "SENDER_PROXY_KEYS_JSON", "GATEWAY_CALLBACK_SECRET", "GATEWAY_CALLBACK_SECRET_PREVIOUS", "GATEWAY_COMMAND_SECRET", "GATEWAY_COMMAND_SECRET_PREVIOUS", "GATEWAY_RUNTIME_SECRET", "GATEWAY_RUNTIME_SECRET_PREVIOUS", "META_CLOUD_CREDENTIALS_JSON", "MEDIA_DOWNLOAD_SECRET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "S3_SESSION_TOKEN", "PROFILING_TOKEN"); err != nil {
		return Config{}, err
	}
	redisDefault := "localhost:6379"
	if environment == "staging" || environment == "production" {
		redisDefault = ""
	}
	callbackSecret := strings.TrimSpace(os.Getenv("GATEWAY_CALLBACK_SECRET"))
	callbackPreviousSecret := strings.TrimSpace(os.Getenv("GATEWAY_CALLBACK_SECRET_PREVIOUS"))
	commandSecret := strings.TrimSpace(os.Getenv("GATEWAY_COMMAND_SECRET"))
	commandPreviousSecret := strings.TrimSpace(os.Getenv("GATEWAY_COMMAND_SECRET_PREVIOUS"))
	runtimeSecret := strings.TrimSpace(os.Getenv("GATEWAY_RUNTIME_SECRET"))
	runtimePreviousSecret := strings.TrimSpace(os.Getenv("GATEWAY_RUNTIME_SECRET_PREVIOUS"))
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
	if runtimeSecret == "" && environment == "development" {
		runtimeSecret = "development-gateway-runtime-secret-change-me"
	}
	if mediaDownloadSecret == "" && environment == "development" {
		mediaDownloadSecret = "development-media-download-secret-change-me"
	}

	maxPreview, err := intEnv("MAX_IMPORT_PREVIEW_ROWS", 100_000)
	if err != nil {
		return Config{}, err
	}
	metaHealthStaleAfter, err := durationEnv("META_CLOUD_HEALTH_STALE_AFTER", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	metaConversationWindow, err := ParseMetaConversationWindow(os.Getenv("META_CLOUD_CONVERSATION_WINDOW"))
	if err != nil {
		return Config{}, err
	}
	gatewayStaleSeconds, err := intEnv("GATEWAY_STALE_AFTER_SECONDS", 120)
	if err != nil {
		return Config{}, err
	}
	senderHeartbeatTTL, err := durationEnv("SENDER_HEARTBEAT_TTL", 90*time.Second)
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
	gatewayRuntimeAllowedHosts, err := gatewayHostListEnv("GATEWAY_RUNTIME_ALLOWED_HOSTS", nil)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Environment:                       environment,
		DataClassification:                classification,
		HTTPAddr:                          strings.TrimSpace(envOrDefault("HTTP_ADDR", ":8080")),
		LogLevel:                          strings.ToLower(strings.TrimSpace(envOrDefault("LOG_LEVEL", "info"))),
		ProfilingToken:                    strings.TrimSpace(os.Getenv("PROFILING_TOKEN")),
		DatabaseURL:                       strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DatabaseDriver:                    strings.TrimSpace(envOrDefault("DATABASE_DRIVER", "postgres")),
		DatabaseMaxOpen:                   dbMaxOpen,
		DatabaseMaxIdle:                   dbMaxIdle,
		DatabaseConnMaxLifetime:           dbLifetime,
		DatabaseConnMaxIdleTime:           dbIdle,
		DatabasePingTimeout:               dbPing,
		RedisAddr:                         strings.TrimSpace(envOrDefault("REDIS_ADDR", redisDefault)),
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
		SenderProxyKeysJSON:               strings.TrimSpace(os.Getenv("SENDER_PROXY_KEYS_JSON")),
		SenderProxyActiveKey:              strings.TrimSpace(envOrDefault("SENDER_PROXY_ACTIVE_KEY_VERSION", "v1")),
		InboundRetentionDays:              inboundRetentionDays,
		AudienceImportSourceRetentionDays: importSourceRetentionDays,
		GatewayCallbackSecret:             callbackSecret,
		GatewayCallbackPreviousSecret:     callbackPreviousSecret,
		GatewayCommandSecret:              commandSecret,
		GatewayCommandPreviousSecret:      commandPreviousSecret,
		GatewayRuntimeSecret:              runtimeSecret,
		GatewayRuntimePreviousSecret:      runtimePreviousSecret,
		GatewayRuntimeAllowedHosts:        gatewayRuntimeAllowedHosts,
		ControlAPIInternalURL:             strings.TrimSpace(os.Getenv("CONTROL_API_INTERNAL_URL")),
		MetaCloudCredentialsJSON:          strings.TrimSpace(os.Getenv("META_CLOUD_CREDENTIALS_JSON")),
		MetaHealthStaleAfter:              metaHealthStaleAfter,
		MetaConversationWindow:            metaConversationWindow,
		GatewayStaleAfter:                 time.Duration(gatewayStaleSeconds) * time.Second,
		SenderHeartbeatTTL:                senderHeartbeatTTL,
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
	if c.ProfilingToken != "" && len(c.ProfilingToken) < 32 {
		return errors.New("PROFILING_TOKEN must contain at least 32 characters when profiling is enabled")
	}
	if c.MaxImportPreviewRows <= 0 || c.MaxImportPreviewRows > 2_000_000 {
		return errors.New("MAX_IMPORT_PREVIEW_ROWS must be between 1 and 2000000")
	}
	if c.AudienceImportSourceRetentionDays < 1 || c.AudienceImportSourceRetentionDays > 3650 {
		return errors.New("AUDIENCE_IMPORT_SOURCE_RETENTION_DAYS must be between 1 and 3650")
	}
	if c.ObjectStoreDriver != "filesystem" && c.ObjectStoreDriver != "s3" && c.ObjectStoreDriver != "minio" {
		return errors.New("OBJECT_STORE_DRIVER must be filesystem, s3, or minio")
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
	if len(c.GatewayRuntimeSecret) > 0 && len(c.GatewayRuntimeSecret) < 32 {
		return errors.New("GATEWAY_RUNTIME_SECRET must contain at least 32 characters")
	}
	for name, value := range map[string]string{
		"GATEWAY_CALLBACK_SECRET_PREVIOUS": c.GatewayCallbackPreviousSecret,
		"GATEWAY_COMMAND_SECRET_PREVIOUS":  c.GatewayCommandPreviousSecret,
		"GATEWAY_RUNTIME_SECRET_PREVIOUS":  c.GatewayRuntimePreviousSecret,
	} {
		if value != "" && len(value) < 32 {
			return fmt.Errorf("%s must contain at least 32 characters", name)
		}
	}
	if c.GatewayCallbackPreviousSecret == c.GatewayCallbackSecret && c.GatewayCallbackPreviousSecret != "" {
		return errors.New("GATEWAY_CALLBACK_SECRET_PREVIOUS must differ from the active secret")
	}
	if c.GatewayCommandPreviousSecret == c.GatewayCommandSecret && c.GatewayCommandPreviousSecret != "" {
		return errors.New("GATEWAY_COMMAND_SECRET_PREVIOUS must differ from the active secret")
	}
	if c.GatewayRuntimePreviousSecret == c.GatewayRuntimeSecret && c.GatewayRuntimePreviousSecret != "" {
		return errors.New("GATEWAY_RUNTIME_SECRET_PREVIOUS must differ from the active secret")
	}
	if len(c.MediaDownloadSecret) > 0 && len(c.MediaDownloadSecret) < 32 {
		return errors.New("MEDIA_DOWNLOAD_SECRET must contain at least 32 characters")
	}
	if c.MetaHealthStaleAfter <= 0 || c.MetaHealthStaleAfter > time.Hour {
		return errors.New("META_CLOUD_HEALTH_STALE_AFTER must be positive and no more than 1 hour")
	}
	if c.MetaConversationWindow <= 0 || c.MetaConversationWindow > DefaultMetaConversationWindow {
		return errors.New("META_CLOUD_CONVERSATION_WINDOW must be positive and no more than 24 hours")
	}
	if raw := strings.TrimSpace(c.MetaCloudCredentialsJSON); raw != "" {
		var entries []struct {
			Key         string `json:"key"`
			AccessToken string `json:"accessToken"`
			AppSecret   string `json:"appSecret"`
			VerifyToken string `json:"verifyToken"`
		}
		if err := json.Unmarshal([]byte(raw), &entries); err != nil || len(entries) == 0 {
			return errors.New("META_CLOUD_CREDENTIALS_JSON must be a non-empty credential array")
		}
		seen := make(map[string]struct{}, len(entries))
		for _, entry := range entries {
			key := strings.TrimSpace(entry.Key)
			if key == "" || len(strings.TrimSpace(entry.AccessToken)) < 20 || len(strings.TrimSpace(entry.AppSecret)) < 16 || len(strings.TrimSpace(entry.VerifyToken)) < 12 {
				return errors.New("META_CLOUD_CREDENTIALS_JSON contains an invalid credential entry")
			}
			if _, exists := seen[key]; exists {
				return errors.New("META_CLOUD_CREDENTIALS_JSON contains a duplicate credential key")
			}
			seen[key] = struct{}{}
		}
	}
	if c.GatewayStaleAfter < 30*time.Second || c.GatewayStaleAfter > time.Hour {
		return errors.New("GATEWAY_STALE_AFTER_SECONDS must be between 30 and 3600")
	}
	if c.SenderHeartbeatTTL < 10*time.Second || c.SenderHeartbeatTTL > 10*time.Minute {
		return errors.New("SENDER_HEARTBEAT_TTL must be between 10 seconds and 10 minutes")
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
	if err := validateVersionedKeyring("sender proxy", "SENDER_PROXY_KEYS_JSON", c.SenderProxyKeysJSON, c.SenderProxyActiveKey); err != nil {
		return err
	}

	if production {
		if strings.Contains(c.BootstrapAdminPassword, "development-only") || len(c.BootstrapAdminPassword) < 20 {
			return errors.New("production requires a strong BOOTSTRAP_ADMIN_PASSWORD")
		}
		if strings.TrimSpace(c.BootstrapAdminTOTP) == "" || strings.Contains(strings.ToLower(c.BootstrapAdminTOTP), "replace") || strings.TrimSpace(c.BootstrapAdminTOTP) == "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" {
			return errors.New("production requires BOOTSTRAP_ADMIN_TOTP_SECRET distinct from the development default")
		}
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(c.BootstrapAdminEmail)), "@example.test") {
			return errors.New("production requires BOOTSTRAP_ADMIN_EMAIL distinct from the development default")
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
		if c.SenderProxyKeysJSON == "" {
			return errors.New("production requires a sender proxy encryption keyring")
		}
		if weakProductionGatewaySecret(c.GatewayCallbackSecret) {
			return errors.New("production requires a strong GATEWAY_CALLBACK_SECRET")
		}
		if weakProductionGatewaySecret(c.GatewayCommandSecret) {
			return errors.New("production requires a strong GATEWAY_COMMAND_SECRET")
		}
		if weakProductionGatewaySecret(c.GatewayRuntimeSecret) {
			return errors.New("production requires a strong GATEWAY_RUNTIME_SECRET")
		}
		if len(c.GatewayRuntimeAllowedHosts) == 0 {
			return errors.New("production requires GATEWAY_RUNTIME_ALLOWED_HOSTS")
		}
		controlURL, err := url.Parse(strings.TrimSpace(c.ControlAPIInternalURL))
		if err != nil || !strings.EqualFold(controlURL.Scheme, "https") || controlURL.Host == "" || controlURL.User != nil || controlURL.Opaque != "" ||
			(controlURL.Path != "" && controlURL.Path != "/") || controlURL.RawPath != "" || controlURL.RawQuery != "" || controlURL.ForceQuery || controlURL.Fragment != "" || strings.HasSuffix(controlURL.Host, ":") {
			return errors.New("production requires CONTROL_API_INTERNAL_URL to be a valid HTTPS control URL")
		}
		if port := controlURL.Port(); port != "" {
			portNumber, portErr := strconv.Atoi(port)
			if portErr != nil || portNumber < 1 || portNumber > 65535 {
				return errors.New("production requires CONTROL_API_INTERNAL_URL to use a valid port between 1 and 65535")
			}
		}
		controlHost := normalizeInternalHostname(controlURL.Hostname())
		if controlHost == "" {
			return errors.New("production requires CONTROL_API_INTERNAL_URL to identify the control hostname")
		}
		if isDisallowedControlHostname(controlHost) {
			return errors.New("production requires CONTROL_API_INTERNAL_URL to identify a non-local control hostname")
		}
		for _, gatewayHost := range c.GatewayRuntimeAllowedHosts {
			if normalizeInternalHostname(gatewayHost) == controlHost {
				return errors.New("GATEWAY_RUNTIME_ALLOWED_HOSTS must not contain the CONTROL_API_INTERNAL_URL control hostname")
			}
		}
		if c.GatewayRuntimeSecret == c.GatewayCallbackSecret || c.GatewayRuntimeSecret == c.GatewayCommandSecret || c.GatewayCallbackSecret == c.GatewayCommandSecret {
			return errors.New("gateway command, callback, and runtime secrets must be distinct")
		}
		previousGatewaySecrets := []struct{ name, value string }{
			{"GATEWAY_CALLBACK_SECRET_PREVIOUS", c.GatewayCallbackPreviousSecret},
			{"GATEWAY_COMMAND_SECRET_PREVIOUS", c.GatewayCommandPreviousSecret},
			{"GATEWAY_RUNTIME_SECRET_PREVIOUS", c.GatewayRuntimePreviousSecret},
		}
		for _, item := range previousGatewaySecrets {
			if item.value != "" && weakProductionGatewaySecret(item.value) {
				return fmt.Errorf("production requires a strong %s", item.name)
			}
		}
		gatewayTrustSecrets := []string{
			c.GatewayCallbackSecret, c.GatewayCommandSecret, c.GatewayRuntimeSecret,
			c.GatewayCallbackPreviousSecret, c.GatewayCommandPreviousSecret, c.GatewayRuntimePreviousSecret,
		}
		for i := 0; i < len(gatewayTrustSecrets); i++ {
			if gatewayTrustSecrets[i] == "" {
				continue
			}
			for j := i + 1; j < len(gatewayTrustSecrets); j++ {
				if gatewayTrustSecrets[j] != "" && gatewayTrustSecrets[i] == gatewayTrustSecrets[j] {
					return errors.New("gateway command, callback, and runtime active/previous secrets must be distinct")
				}
			}
		}
		mediaDownloadSecretLower := strings.ToLower(c.MediaDownloadSecret)
		if len(c.MediaDownloadSecret) < 32 || strings.Contains(mediaDownloadSecretLower, "development-") || strings.Contains(mediaDownloadSecretLower, "change-me") {
			return errors.New("production requires a strong MEDIA_DOWNLOAD_SECRET")
		}
		switch c.ObjectStoreDriver {
		case "filesystem":
			if c.ObjectStoreRoot == "" || !filepath.IsAbs(c.ObjectStoreRoot) {
				return errors.New("filesystem object storage requires an absolute OBJECT_STORE_ROOT in production")
			}
		case "s3", "minio":
		default:
			return errors.New("OBJECT_STORE_DRIVER must be filesystem, s3, or minio")
		}
		if len(c.AllowedNetworkCIDRs) == 0 {
			return errors.New("production requires ALLOWED_NETWORK_CIDRS")
		}
		if len(c.TrustedProxyCIDRs) == 0 {
			return errors.New("production requires TRUSTED_PROXY_CIDRS")
		}
		if c.ClamAVAddress == "" {
			return errors.New("production requires CLAMAV_ADDRESS")
		}
	}
	return nil
}

func normalizeInternalHostname(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.Trim(strings.TrimSpace(value), "[]")), ".")
}

func isDisallowedControlHostname(value string) bool {
	normalized := normalizeInternalHostname(value)
	if normalized == "control-api" || normalized == "openwa-gateway" || normalized == "localhost" || strings.HasSuffix(normalized, ".localhost") || normalized == "host.docker.internal" || strings.HasSuffix(normalized, ".docker.internal") {
		return true
	}
	// Staging/production control authority is DNS-only. Reject canonical IPs and
	// legacy inet_aton-style numeric IPv4 spellings that libc may resolve as IPs.
	return isIPLikeHostname(normalized)
}

func isIPLikeHostname(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.Trim(value, "[]")))
	if normalized == "" || strings.Contains(normalized, "%") {
		return normalized != ""
	}
	if net.ParseIP(normalized) != nil {
		return true
	}
	parts := strings.Split(normalized, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		base := 10
		digits := part
		if strings.HasPrefix(part, "0x") {
			base, digits = 16, part[2:]
		} else if len(part) > 1 && part[0] == '0' {
			base, digits = 8, part[1:]
		}
		if digits == "" {
			return false
		}
		if _, err := strconv.ParseUint(digits, base, 32); err != nil {
			return false
		}
	}
	return true
}

// ControlAPIHostname returns the canonical control-plane authority after
// configuration validation has established CONTROL_API_INTERNAL_URL.
func (c Config) ControlAPIHostname() string {
	parsed, err := url.Parse(strings.TrimSpace(c.ControlAPIInternalURL))
	if err != nil {
		return ""
	}
	return normalizeInternalHostname(parsed.Hostname())
}

func weakProductionGatewaySecret(value string) bool {
	lower := strings.ToLower(value)
	return len(value) < 32 || strings.Contains(lower, "development-") || strings.Contains(lower, "change-me")
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

func gatewayHostListEnv(key string, fallback []string) ([]string, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return append([]string(nil), fallback...), nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 100 {
		return nil, fmt.Errorf("%s contains more than 100 hosts", key)
	}
	seen := map[string]struct{}{}
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := normalizeGatewayHostname(part)
		if value == "" {
			return nil, fmt.Errorf("%s contains an empty host", key)
		}
		if isDisallowedGatewayHostname(value) || len(value) > 253 || (net.ParseIP(value) == nil && !validGatewayDNSName(value)) {
			return nil, fmt.Errorf("%s contains invalid host %q", key, value)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values, nil
}

func normalizeGatewayHostname(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.Trim(strings.TrimSpace(value), "[]")), ".")
}

func isDisallowedGatewayHostname(value string) bool {
	normalized := normalizeGatewayHostname(value)
	if normalized == "control-api" || normalized == "openwa-gateway" || normalized == "localhost" || strings.HasSuffix(normalized, ".localhost") || normalized == "host.docker.internal" || strings.HasSuffix(normalized, ".docker.internal") {
		return true
	}
	return isIPLikeHostname(normalized)
}

func validGatewayDNSName(value string) bool {
	name := strings.TrimSuffix(value, ".")
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				return false
			}
		}
	}
	return true
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
