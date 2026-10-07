package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"campaign-platform/internal/metacloud"
	sharedconfig "campaign-platform/internal/shared/config"
	"campaign-platform/internal/shared/envfile"
)

// AudienceConfig contains only the settings required by the audience validation
// worker. It deliberately does not reuse the control-plane configuration because
// workers must not require bootstrap-admin or browser-session secrets.
type AudienceConfig struct {
	Environment                       string
	HealthAddr                        string
	DatabaseDriver                    string
	DatabaseURL                       string
	DBMaxOpen                         int
	DBMaxIdle                         int
	DBConnMaxLifetime                 time.Duration
	DBConnMaxIdleTime                 time.Duration
	DBPingTimeout                     time.Duration
	WorkerID                          string
	ObjectStoreDriver                 string
	ObjectStoreRoot                   string
	ObjectStoreTempDir                string
	ClamAVAddress                     string
	ClamAVDialTimeout                 time.Duration
	ClamAVScanTimeout                 time.Duration
	MSISDNEncryptionKey               string
	MSISDNLookupKey                   string
	Concurrency                       int
	ClaimBatch                        int
	PollInterval                      time.Duration
	ClaimFailureBackoff               time.Duration
	MaximumFailureBackoff             time.Duration
	ValidationLeaseDuration           time.Duration
	MaterialisationBatchSize          int
	MaterialisationClaimBatch         int
	MaterialisationLeaseDuration      time.Duration
	MaterialisationPollInterval       time.Duration
	EstimateClaimBatch                int
	EstimateLeaseDuration             time.Duration
	EstimatePollInterval              time.Duration
	EstimateRetryBackoff              time.Duration
	MergeConcurrency                  int
	MergeClaimBatch                   int
	MergeLeaseDuration                time.Duration
	MergePollInterval                 time.Duration
	MergeFailureBackoff               time.Duration
	SourceRetentionBatchSize          int
	SourceRetentionLeaseDuration      time.Duration
	SourceRetentionPollInterval       time.Duration
	UploadFinalisationClaimBatch      int
	UploadFinalisationLeaseDuration   time.Duration
	UploadFinalisationPollInterval    time.Duration
	AudienceImportSourceRetentionDays int
	StageBatchSize                    int
	MaxRows                           int
	MaxIssues                         int
	DefaultCountryISO2                string
	ShutdownTimeout                   time.Duration
}

func LoadAudience() (AudienceConfig, error) {
	environment := strings.ToLower(strings.TrimSpace(env("APP_ENV", "development")))
	if err := resolveWorkerFiles(environment); err != nil {
		return AudienceConfig{}, err
	}
	cfg := AudienceConfig{
		Environment:         environment,
		HealthAddr:          strings.TrimSpace(env("WORKER_HEALTH_ADDR", ":8091")),
		DatabaseDriver:      strings.TrimSpace(env("POSTGRES_DRIVER", "postgres")),
		DatabaseURL:         strings.TrimSpace(os.Getenv("DATABASE_URL")),
		WorkerID:            strings.TrimSpace(env("WORKER_ID", hostname("audience-worker"))),
		ObjectStoreDriver:   strings.ToLower(strings.TrimSpace(env("OBJECT_STORE_DRIVER", "filesystem"))),
		ObjectStoreRoot:     strings.TrimSpace(os.Getenv("OBJECT_STORE_ROOT")),
		ObjectStoreTempDir:  strings.TrimSpace(os.Getenv("OBJECT_STORE_TEMP_DIR")),
		ClamAVAddress:       strings.TrimSpace(os.Getenv("CLAMAV_ADDRESS")),
		MSISDNEncryptionKey: strings.TrimSpace(os.Getenv("MSISDN_ENCRYPTION_KEY_BASE64")),
		MSISDNLookupKey:     strings.TrimSpace(os.Getenv("MSISDN_LOOKUP_KEY_BASE64")),
		DefaultCountryISO2:  strings.ToUpper(strings.TrimSpace(env("DEFAULT_COUNTRY_ISO2", "NG"))),
	}
	var err error
	if cfg.DBMaxOpen, err = integer("DB_MAX_OPEN", 20); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.DBMaxIdle, err = integer("DB_MAX_IDLE", 5); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.Concurrency, err = integer("AUDIENCE_WORKER_CONCURRENCY", 2); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.ClaimBatch, err = integer("AUDIENCE_WORKER_CLAIM_BATCH", 2); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MergeConcurrency, err = integer("AUDIENCE_MERGE_CONCURRENCY", 2); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MergeClaimBatch, err = integer("AUDIENCE_MERGE_CLAIM_BATCH", 2); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MaterialisationBatchSize, err = integer("AUDIENCE_MATERIALISATION_BATCH_SIZE", 5000); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MaterialisationClaimBatch, err = integer("AUDIENCE_MATERIALISATION_CLAIM_BATCH", 1); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.EstimateClaimBatch, err = integer("AUDIENCE_ESTIMATE_CLAIM_BATCH", 1); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.StageBatchSize, err = integer("AUDIENCE_STAGE_BATCH_SIZE", 5000); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.SourceRetentionBatchSize, err = integer("AUDIENCE_SOURCE_RETENTION_BATCH_SIZE", 50); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.UploadFinalisationClaimBatch, err = integer("AUDIENCE_UPLOAD_FINALISATION_CLAIM_BATCH", 2); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.AudienceImportSourceRetentionDays, err = integer("AUDIENCE_IMPORT_SOURCE_RETENTION_DAYS", 30); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MaxRows, err = integer("MAX_IMPORT_ROWS", 2_000_000); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MaxIssues, err = integer("MAX_IMPORT_ISSUES", 1000); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.DBConnMaxLifetime, err = duration("DB_CONN_MAX_LIFETIME", 30*time.Minute); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.DBConnMaxIdleTime, err = duration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.DBPingTimeout, err = duration("DB_PING_TIMEOUT", 5*time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.PollInterval, err = duration("AUDIENCE_WORKER_POLL_INTERVAL", time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.ClaimFailureBackoff, err = duration("AUDIENCE_WORKER_CLAIM_FAILURE_BACKOFF", time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MaximumFailureBackoff, err = duration("AUDIENCE_WORKER_MAX_FAILURE_BACKOFF", 30*time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.ValidationLeaseDuration, err = duration("AUDIENCE_VALIDATION_LEASE_DURATION", 2*time.Minute); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MergeLeaseDuration, err = duration("AUDIENCE_MERGE_LEASE_DURATION", 2*time.Minute); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MergePollInterval, err = duration("AUDIENCE_MERGE_POLL_INTERVAL", time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MergeFailureBackoff, err = duration("AUDIENCE_MERGE_FAILURE_BACKOFF", 30*time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MaterialisationLeaseDuration, err = duration("AUDIENCE_MATERIALISATION_LEASE_DURATION", 2*time.Minute); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.MaterialisationPollInterval, err = duration("AUDIENCE_MATERIALISATION_POLL_INTERVAL", time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.EstimateLeaseDuration, err = duration("AUDIENCE_ESTIMATE_LEASE_DURATION", 5*time.Minute); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.EstimatePollInterval, err = duration("AUDIENCE_ESTIMATE_POLL_INTERVAL", time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.EstimateRetryBackoff, err = duration("AUDIENCE_ESTIMATE_RETRY_BACKOFF", 30*time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.SourceRetentionLeaseDuration, err = duration("AUDIENCE_SOURCE_RETENTION_LEASE_DURATION", 2*time.Minute); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.SourceRetentionPollInterval, err = duration("AUDIENCE_SOURCE_RETENTION_POLL_INTERVAL", time.Minute); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.UploadFinalisationLeaseDuration, err = duration("AUDIENCE_UPLOAD_FINALISATION_LEASE_DURATION", 5*time.Minute); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.UploadFinalisationPollInterval, err = duration("AUDIENCE_UPLOAD_FINALISATION_POLL_INTERVAL", time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.ClamAVDialTimeout, err = duration("CLAMAV_DIAL_TIMEOUT", 5*time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.ClamAVScanTimeout, err = duration("CLAMAV_SCAN_TIMEOUT", 10*time.Minute); err != nil {
		return AudienceConfig{}, err
	}
	if cfg.ShutdownTimeout, err = duration("WORKER_SHUTDOWN_TIMEOUT", 30*time.Second); err != nil {
		return AudienceConfig{}, err
	}
	if err := cfg.Validate(); err != nil {
		return AudienceConfig{}, err
	}
	return cfg, nil
}

func (c AudienceConfig) Validate() error {
	switch c.Environment {
	case "development", "test", "staging", "production":
	default:
		return fmt.Errorf("APP_ENV %q is unsupported", c.Environment)
	}
	if err := ValidateHealthAddress(c.HealthAddr); err != nil {
		return err
	}
	if c.DatabaseDriver == "" {
		return errors.New("POSTGRES_DRIVER is required")
	}
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if c.WorkerID == "" || len(c.WorkerID) > 128 {
		return errors.New("WORKER_ID must contain 1 to 128 characters")
	}
	switch c.ObjectStoreDriver {
	case "filesystem":
		if c.ObjectStoreRoot == "" {
			return errors.New("OBJECT_STORE_ROOT is required for filesystem storage")
		}
		if (c.Environment == "staging" || c.Environment == "production") && !filepath.IsAbs(c.ObjectStoreRoot) {
			return errors.New("deployed workers require an absolute OBJECT_STORE_ROOT for filesystem storage")
		}
	case "s3", "minio":
	default:
		return errors.New("OBJECT_STORE_DRIVER must be filesystem, s3, or minio")
	}
	if c.ObjectStoreTempDir != "" && (c.Environment == "staging" || c.Environment == "production") && !filepath.IsAbs(c.ObjectStoreTempDir) {
		return errors.New("deployed workers require an absolute OBJECT_STORE_TEMP_DIR when configured")
	}
	if strings.TrimSpace(c.ClamAVAddress) == "" {
		return errors.New("CLAMAV_ADDRESS is required for audience upload finalisation")
	}
	if _, _, err := net.SplitHostPort(strings.TrimSpace(c.ClamAVAddress)); err != nil {
		return errors.New("CLAMAV_ADDRESS must be in host:port form")
	}
	if c.ClamAVDialTimeout <= 0 || c.ClamAVDialTimeout > time.Minute {
		return errors.New("CLAMAV_DIAL_TIMEOUT must be positive and no more than one minute")
	}
	if c.ClamAVScanTimeout < time.Minute || c.ClamAVScanTimeout > time.Hour {
		return errors.New("CLAMAV_SCAN_TIMEOUT must be between one minute and one hour")
	}
	if err := validateKey("MSISDN_ENCRYPTION_KEY_BASE64", c.MSISDNEncryptionKey, 32, true); err != nil {
		return err
	}
	if err := validateKey("MSISDN_LOOKUP_KEY_BASE64", c.MSISDNLookupKey, 32, false); err != nil {
		return err
	}
	if c.DBMaxOpen < 1 || c.DBMaxOpen > 200 {
		return errors.New("DB_MAX_OPEN must be between 1 and 200")
	}
	if c.DBMaxIdle < 0 || c.DBMaxIdle > c.DBMaxOpen {
		return errors.New("DB_MAX_IDLE must be between 0 and DB_MAX_OPEN")
	}
	if c.Concurrency < 1 || c.Concurrency > 64 {
		return errors.New("AUDIENCE_WORKER_CONCURRENCY must be between 1 and 64")
	}
	if c.ClaimBatch < 1 || c.ClaimBatch > c.Concurrency {
		return errors.New("AUDIENCE_WORKER_CLAIM_BATCH must be between 1 and worker concurrency")
	}
	if c.MaterialisationBatchSize < 100 || c.MaterialisationBatchSize > 10_000 {
		return errors.New("AUDIENCE_MATERIALISATION_BATCH_SIZE must be between 100 and 10000")
	}
	if c.MaterialisationClaimBatch < 1 || c.MaterialisationClaimBatch > 16 {
		return errors.New("AUDIENCE_MATERIALISATION_CLAIM_BATCH must be between 1 and 16")
	}
	if c.MaterialisationLeaseDuration < 15*time.Second || c.MaterialisationLeaseDuration > 30*time.Minute {
		return errors.New("AUDIENCE_MATERIALISATION_LEASE_DURATION must be between 15 seconds and 30 minutes")
	}
	if c.MaterialisationPollInterval <= 0 || c.MaterialisationPollInterval > time.Minute {
		return errors.New("AUDIENCE_MATERIALISATION_POLL_INTERVAL must be positive and no more than one minute")
	}
	if c.EstimateClaimBatch < 1 || c.EstimateClaimBatch > 16 {
		return errors.New("AUDIENCE_ESTIMATE_CLAIM_BATCH must be between 1 and 16")
	}
	if c.EstimateLeaseDuration < 15*time.Second || c.EstimateLeaseDuration > 30*time.Minute {
		return errors.New("AUDIENCE_ESTIMATE_LEASE_DURATION must be between 15 seconds and 30 minutes")
	}
	if c.EstimatePollInterval <= 0 || c.EstimatePollInterval > time.Minute {
		return errors.New("AUDIENCE_ESTIMATE_POLL_INTERVAL must be positive and no more than one minute")
	}
	if c.EstimateRetryBackoff <= 0 || c.EstimateRetryBackoff > time.Hour {
		return errors.New("AUDIENCE_ESTIMATE_RETRY_BACKOFF must be positive and no more than one hour")
	}
	if c.MergeConcurrency < 1 || c.MergeConcurrency > 64 {
		return errors.New("AUDIENCE_MERGE_CONCURRENCY must be between 1 and 64")
	}
	if c.MergeClaimBatch < 1 || c.MergeClaimBatch > c.MergeConcurrency {
		return errors.New("AUDIENCE_MERGE_CLAIM_BATCH must be between 1 and merge concurrency")
	}
	if c.MergeLeaseDuration < 15*time.Second || c.MergeLeaseDuration > 30*time.Minute {
		return errors.New("AUDIENCE_MERGE_LEASE_DURATION must be between 15 seconds and 30 minutes")
	}
	if c.MergePollInterval <= 0 || c.MergePollInterval > time.Minute {
		return errors.New("AUDIENCE_MERGE_POLL_INTERVAL must be positive and no more than one minute")
	}
	if c.MergeFailureBackoff <= 0 || c.MergeFailureBackoff > time.Hour {
		return errors.New("AUDIENCE_MERGE_FAILURE_BACKOFF must be positive and no more than one hour")
	}
	if c.StageBatchSize < 1 || c.StageBatchSize > 10_000 {
		return errors.New("AUDIENCE_STAGE_BATCH_SIZE must be between 1 and 10000")
	}
	if c.SourceRetentionBatchSize < 1 || c.SourceRetentionBatchSize > 500 {
		return errors.New("AUDIENCE_SOURCE_RETENTION_BATCH_SIZE must be between 1 and 500")
	}
	if c.SourceRetentionLeaseDuration < 15*time.Second || c.SourceRetentionLeaseDuration > 30*time.Minute {
		return errors.New("AUDIENCE_SOURCE_RETENTION_LEASE_DURATION must be between 15 seconds and 30 minutes")
	}
	if c.SourceRetentionPollInterval <= 0 || c.SourceRetentionPollInterval > time.Hour {
		return errors.New("AUDIENCE_SOURCE_RETENTION_POLL_INTERVAL must be positive and no more than one hour")
	}
	if c.UploadFinalisationClaimBatch < 1 || c.UploadFinalisationClaimBatch > 16 {
		return errors.New("AUDIENCE_UPLOAD_FINALISATION_CLAIM_BATCH must be between 1 and 16")
	}
	if c.UploadFinalisationLeaseDuration < 15*time.Second || c.UploadFinalisationLeaseDuration > 30*time.Minute {
		return errors.New("AUDIENCE_UPLOAD_FINALISATION_LEASE_DURATION must be between 15 seconds and 30 minutes")
	}
	if c.UploadFinalisationPollInterval <= 0 || c.UploadFinalisationPollInterval > time.Minute {
		return errors.New("AUDIENCE_UPLOAD_FINALISATION_POLL_INTERVAL must be positive and no more than one minute")
	}
	if c.AudienceImportSourceRetentionDays < 1 || c.AudienceImportSourceRetentionDays > 3650 {
		return errors.New("AUDIENCE_IMPORT_SOURCE_RETENTION_DAYS must be between 1 and 3650")
	}
	if c.MaxRows < 1 || c.MaxRows > 20_000_000 {
		return errors.New("MAX_IMPORT_ROWS must be between 1 and 20000000")
	}
	if c.MaxIssues < 1 || c.MaxIssues > 100_000 {
		return errors.New("MAX_IMPORT_ISSUES must be between 1 and 100000")
	}
	if c.DBPingTimeout <= 0 || c.DBPingTimeout > 30*time.Second {
		return errors.New("DB_PING_TIMEOUT must be positive and no more than 30 seconds")
	}
	if c.DBConnMaxLifetime <= 0 || c.DBConnMaxIdleTime <= 0 {
		return errors.New("database connection lifetimes must be positive")
	}
	if c.PollInterval <= 0 || c.ClaimFailureBackoff <= 0 || c.MaximumFailureBackoff < c.ClaimFailureBackoff {
		return errors.New("worker polling/backoff durations are invalid")
	}
	if c.ValidationLeaseDuration < 15*time.Second || c.ValidationLeaseDuration > 30*time.Minute {
		return errors.New("AUDIENCE_VALIDATION_LEASE_DURATION must be between 15 seconds and 30 minutes")
	}
	if c.ShutdownTimeout < time.Second || c.ShutdownTimeout > 5*time.Minute {
		return errors.New("WORKER_SHUTDOWN_TIMEOUT must be between 1 second and 5 minutes")
	}
	if len(c.DefaultCountryISO2) != 2 {
		return errors.New("DEFAULT_COUNTRY_ISO2 must be an ISO-3166 alpha-2 code")
	}
	return nil
}

func validateKey(name, value string, length int, exact bool) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return fmt.Errorf("%s must be valid standard base64", name)
	}
	if exact && len(decoded) != length {
		return fmt.Errorf("%s must decode to exactly %d bytes", name, length)
	}
	if !exact && len(decoded) < length {
		return fmt.Errorf("%s must decode to at least %d bytes", name, length)
	}
	return nil
}

func integer(key string, fallback int) (int, error) {
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

func duration(key string, fallback time.Duration) (time.Duration, error) {
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

func resolveWorkerFiles(environment string) error {
	return envfile.Resolve(environment, "DATABASE_URL", "MSISDN_ENCRYPTION_KEY_BASE64", "MSISDN_LOOKUP_KEY_BASE64", "GATEWAY_COMMAND_SECRET", "OPENWA_GATEWAY_API_KEY", "MEDIA_DOWNLOAD_SECRET", "META_CLOUD_CREDENTIALS_JSON", "INBOUND_CONTENT_KEYS_JSON", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "S3_SESSION_TOKEN", "PROFILING_TOKEN")
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func hostname(fallback string) string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

// ValidateHealthAddress catches common configuration mistakes without binding
// the port. It is separated for focused tests and startup diagnostics.
func ValidateHealthAddress(address string) error {
	_, portText, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return errors.New("WORKER_HEALTH_ADDR must be in host:port form")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("WORKER_HEALTH_ADDR has an invalid port")
	}
	return nil
}

// CampaignConfig controls the outbox publisher and dispatch job runner. Both
// share one database pool but have independent fencing leases and concurrency
// bounds so publishing pressure cannot starve gateway dispatch.
type CampaignConfig struct {
	Environment                    string
	HealthAddr                     string
	DatabaseDriver                 string
	DatabaseURL                    string
	DBMaxOpen                      int
	DBMaxIdle                      int
	DBConnMaxLifetime              time.Duration
	DBConnMaxIdleTime              time.Duration
	DBPingTimeout                  time.Duration
	WorkerID                       string
	MSISDNEncryptionKey            string
	MSISDNLookupKey                string
	GatewayURL                     string
	GatewayCommandSecret           string
	MediaDownloadBaseURL           string
	MediaDownloadSecret            string
	MetaCloudCredentialsJSON       string
	MetaHealthStaleAfter           time.Duration
	MetaConversationWindow         time.Duration
	GatewayHTTPTimeout             time.Duration
	GatewayMaxResponse             int64
	SenderHeartbeatTTL             time.Duration
	JobConcurrency                 int
	JobClaimBatch                  int
	JobLease                       time.Duration
	JobPollInterval                time.Duration
	JobRetryBaseDelay              time.Duration
	JobRetryMaxDelay               time.Duration
	JobRetryJitterPercent          int
	JobRetryCategoryPolicy         string
	OutboxConcurrency              int
	OutboxClaimBatch               int
	DispatchQueueBackpressureLimit int
	DispatchQueueBackpressureRetry time.Duration
	DispatchShardTargetSize        int
	DispatchShardClaimBatch        int
	DispatchQueueRepairBatch       int
	DispatchQueueRepairInterval    time.Duration
	OutboxLease                    time.Duration
	OutboxPollInterval             time.Duration
	OperationTimeout               time.Duration
	ShutdownTimeout                time.Duration
}

func LoadCampaign() (CampaignConfig, error) {
	environment := strings.ToLower(strings.TrimSpace(env("APP_ENV", "development")))
	if err := resolveWorkerFiles(environment); err != nil {
		return CampaignConfig{}, err
	}
	cfg := CampaignConfig{
		Environment:              environment,
		HealthAddr:               strings.TrimSpace(env("WORKER_HEALTH_ADDR", ":8092")),
		DatabaseDriver:           strings.TrimSpace(env("POSTGRES_DRIVER", "postgres")),
		DatabaseURL:              strings.TrimSpace(os.Getenv("DATABASE_URL")),
		WorkerID:                 strings.TrimSpace(env("WORKER_ID", hostname("campaign-worker"))),
		MSISDNEncryptionKey:      strings.TrimSpace(os.Getenv("MSISDN_ENCRYPTION_KEY_BASE64")),
		MSISDNLookupKey:          strings.TrimSpace(os.Getenv("MSISDN_LOOKUP_KEY_BASE64")),
		GatewayURL:               strings.TrimSpace(os.Getenv("OPENWA_GATEWAY_URL")),
		GatewayCommandSecret:     strings.TrimSpace(firstNonEmpty(os.Getenv("GATEWAY_COMMAND_SECRET"), os.Getenv("OPENWA_GATEWAY_API_KEY"))),
		MediaDownloadBaseURL:     strings.TrimSpace(os.Getenv("MEDIA_DOWNLOAD_BASE_URL")),
		MediaDownloadSecret:      strings.TrimSpace(os.Getenv("MEDIA_DOWNLOAD_SECRET")),
		MetaCloudCredentialsJSON: strings.TrimSpace(os.Getenv("META_CLOUD_CREDENTIALS_JSON")),
	}
	var err error
	if cfg.DBMaxOpen, err = integer("DB_MAX_OPEN", 40); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.DBMaxIdle, err = integer("DB_MAX_IDLE", 10); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.JobConcurrency, err = integer("CAMPAIGN_JOB_CONCURRENCY", 8); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.JobClaimBatch, err = integer("CAMPAIGN_JOB_CLAIM_BATCH", 8); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.OutboxConcurrency, err = integer("OUTBOX_CONCURRENCY", 4); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.OutboxClaimBatch, err = integer("OUTBOX_CLAIM_BATCH", 4); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.DispatchQueueBackpressureLimit, err = integer("DISPATCH_QUEUE_BACKPRESSURE_LIMIT", 100000); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.DispatchShardTargetSize, err = integer("DISPATCH_SHARD_TARGET_SIZE", 10000); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.DispatchShardClaimBatch, err = integer("DISPATCH_SHARD_CLAIM_BATCH", 20); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.DispatchQueueRepairBatch, err = integer("DISPATCH_QUEUE_REPAIR_BATCH", 1000); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.GatewayMaxResponse, err = int64Value("GATEWAY_MAX_RESPONSE_BYTES", 1<<20); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.DBConnMaxLifetime, err = duration("DB_CONN_MAX_LIFETIME", 30*time.Minute); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.DBConnMaxIdleTime, err = duration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.DBPingTimeout, err = duration("DB_PING_TIMEOUT", 5*time.Second); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.GatewayHTTPTimeout, err = duration("GATEWAY_HTTP_TIMEOUT", 30*time.Second); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.MetaHealthStaleAfter, err = duration("META_CLOUD_HEALTH_STALE_AFTER", 5*time.Minute); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.MetaConversationWindow, err = sharedconfig.ParseMetaConversationWindow(os.Getenv("META_CLOUD_CONVERSATION_WINDOW")); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.SenderHeartbeatTTL, err = duration("SENDER_HEARTBEAT_TTL", 90*time.Second); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.JobLease, err = duration("CAMPAIGN_JOB_LEASE", 2*time.Minute); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.JobPollInterval, err = duration("CAMPAIGN_JOB_POLL_INTERVAL", 500*time.Millisecond); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.JobRetryBaseDelay, err = duration("CAMPAIGN_JOB_RETRY_BASE_DELAY", time.Second); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.JobRetryMaxDelay, err = duration("CAMPAIGN_JOB_RETRY_MAX_DELAY", 2*time.Minute); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.JobRetryJitterPercent, err = integer("CAMPAIGN_JOB_RETRY_JITTER_PERCENT", 20); err != nil {
		return CampaignConfig{}, err
	}
	cfg.JobRetryCategoryPolicy = strings.TrimSpace(os.Getenv("CAMPAIGN_JOB_RETRY_CATEGORY_POLICY"))
	if cfg.OutboxLease, err = duration("OUTBOX_LEASE", 2*time.Minute); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.OutboxPollInterval, err = duration("OUTBOX_POLL_INTERVAL", 500*time.Millisecond); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.DispatchQueueBackpressureRetry, err = duration("DISPATCH_QUEUE_BACKPRESSURE_RETRY", 5*time.Second); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.DispatchQueueRepairInterval, err = duration("DISPATCH_QUEUE_REPAIR_INTERVAL", 30*time.Second); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.OperationTimeout, err = duration("WORKER_OPERATION_TIMEOUT", 10*time.Second); err != nil {
		return CampaignConfig{}, err
	}
	if cfg.ShutdownTimeout, err = duration("WORKER_SHUTDOWN_TIMEOUT", 30*time.Second); err != nil {
		return CampaignConfig{}, err
	}
	if err := cfg.Validate(); err != nil {
		return CampaignConfig{}, err
	}
	return cfg, nil
}

func (c CampaignConfig) Validate() error {
	switch c.Environment {
	case "development", "test", "staging", "production":
	default:
		return fmt.Errorf("APP_ENV %q is unsupported", c.Environment)
	}
	if err := ValidateHealthAddress(c.HealthAddr); err != nil {
		return err
	}
	if c.DatabaseDriver == "" || c.DatabaseURL == "" {
		return errors.New("POSTGRES_DRIVER and DATABASE_URL are required")
	}
	if c.WorkerID == "" || len(c.WorkerID) > 128 {
		return errors.New("WORKER_ID must contain 1 to 128 characters")
	}
	if err := validateKey("MSISDN_ENCRYPTION_KEY_BASE64", c.MSISDNEncryptionKey, 32, true); err != nil {
		return err
	}
	if err := validateKey("MSISDN_LOOKUP_KEY_BASE64", c.MSISDNLookupKey, 32, false); err != nil {
		return err
	}
	openwaConfigured := strings.TrimSpace(c.GatewayURL) != "" || strings.TrimSpace(c.GatewayCommandSecret) != ""
	metaConfigured := strings.TrimSpace(c.MetaCloudCredentialsJSON) != ""
	if !openwaConfigured && !metaConfigured {
		return errors.New("campaign worker requires at least one configured messaging transport")
	}
	if metaConfigured {
		if _, err := metacloud.ParseCredentialSet(c.MetaCloudCredentialsJSON); err != nil {
			return errors.New("META_CLOUD_CREDENTIALS_JSON is invalid")
		}
	}
	if openwaConfigured {
		if strings.TrimSpace(c.GatewayURL) != "" {
			gateway, err := url.Parse(c.GatewayURL)
			if err != nil || gateway.Scheme == "" || gateway.Host == "" || (gateway.Scheme != "http" && gateway.Scheme != "https") {
				return errors.New("OPENWA_GATEWAY_URL must be a valid http or https URL when configured")
			}
			if c.Environment == "staging" || c.Environment == "production" {
				return errors.New("deployed campaign workers must not configure OPENWA_GATEWAY_URL; use governed node-addressed runtime URLs")
			}
		}
		if len(c.GatewayCommandSecret) < 32 {
			return errors.New("GATEWAY_COMMAND_SECRET must contain at least 32 characters when OpenWA is configured")
		}
	}
	mediaURL, err := url.Parse(c.MediaDownloadBaseURL)
	if err != nil || mediaURL.Scheme == "" || mediaURL.Host == "" || (mediaURL.Scheme != "http" && mediaURL.Scheme != "https") {
		return errors.New("MEDIA_DOWNLOAD_BASE_URL must be a valid http or https URL")
	}
	if c.Environment == "staging" || c.Environment == "production" {
		if mediaURL.Scheme != "https" {
			return errors.New("deployed campaign workers require an HTTPS MEDIA_DOWNLOAD_BASE_URL")
		}
		if isDisallowedDeployedHostname(mediaURL.Hostname()) {
			return errors.New("deployed campaign workers must not use a Docker-local, loopback, or link-local MEDIA_DOWNLOAD_BASE_URL hostname")
		}
	}
	if len(c.MediaDownloadSecret) < 32 {
		return errors.New("MEDIA_DOWNLOAD_SECRET must contain at least 32 characters")
	}
	if c.DBMaxOpen < 1 || c.DBMaxOpen > 300 || c.DBMaxIdle < 0 || c.DBMaxIdle > c.DBMaxOpen {
		return errors.New("database pool bounds are invalid")
	}
	if c.JobConcurrency < 1 || c.JobConcurrency > 256 || c.JobClaimBatch < 1 || c.JobClaimBatch > c.JobConcurrency {
		return errors.New("campaign job concurrency or claim batch is invalid")
	}
	if c.DispatchShardTargetSize < 100 || c.DispatchShardTargetSize > 100000 || c.DispatchShardClaimBatch < 1 || c.DispatchShardClaimBatch > 100 {
		return errors.New("dispatch shard bounds are invalid")
	}
	if c.DispatchQueueRepairBatch < 1 || c.DispatchQueueRepairBatch > 10000 || c.DispatchQueueRepairInterval < time.Second || c.DispatchQueueRepairInterval > time.Hour {
		return errors.New("dispatch queue repair bounds are invalid")
	}
	if c.DispatchQueueBackpressureLimit < 1 || c.DispatchQueueBackpressureLimit > 10_000_000 {
		return errors.New("DISPATCH_QUEUE_BACKPRESSURE_LIMIT must be between 1 and 10000000")
	}
	if c.DispatchQueueBackpressureRetry <= 0 || c.DispatchQueueBackpressureRetry > 5*time.Minute {
		return errors.New("DISPATCH_QUEUE_BACKPRESSURE_RETRY must be positive and no more than 5 minutes")
	}
	if c.OutboxConcurrency < 1 || c.OutboxConcurrency > 128 || c.OutboxClaimBatch < 1 || c.OutboxClaimBatch > c.OutboxConcurrency {
		return errors.New("outbox concurrency or claim batch is invalid")
	}
	if c.MetaHealthStaleAfter <= 0 || c.MetaHealthStaleAfter > time.Hour {
		return errors.New("META_CLOUD_HEALTH_STALE_AFTER must be positive and no more than 1 hour")
	}
	if c.MetaConversationWindow <= 0 || c.MetaConversationWindow > sharedconfig.DefaultMetaConversationWindow {
		return errors.New("META_CLOUD_CONVERSATION_WINDOW must be positive and no more than 24 hours")
	}
	if c.GatewayHTTPTimeout <= 0 || c.GatewayHTTPTimeout > 2*time.Minute {
		return errors.New("GATEWAY_HTTP_TIMEOUT must be positive and no more than 2 minutes")
	}
	if c.GatewayMaxResponse < 1024 || c.GatewayMaxResponse > 4<<20 {
		return errors.New("GATEWAY_MAX_RESPONSE_BYTES must be between 1 KiB and 4 MiB")
	}
	if c.SenderHeartbeatTTL < 10*time.Second || c.SenderHeartbeatTTL > 10*time.Minute {
		return errors.New("SENDER_HEARTBEAT_TTL must be between 10 seconds and 10 minutes")
	}
	if c.JobLease < 15*time.Second || c.OutboxLease < 15*time.Second {
		return errors.New("job and outbox leases must be at least 15 seconds")
	}
	if c.JobPollInterval <= 0 || c.OutboxPollInterval <= 0 {
		return errors.New("worker poll intervals must be positive")
	}
	if c.JobRetryBaseDelay <= 0 || c.JobRetryMaxDelay < c.JobRetryBaseDelay || c.JobRetryMaxDelay > time.Hour {
		return errors.New("campaign job retry delays must be positive, ordered, and capped at one hour")
	}
	if c.JobRetryJitterPercent < 0 || c.JobRetryJitterPercent > 100 {
		return errors.New("CAMPAIGN_JOB_RETRY_JITTER_PERCENT must be between 0 and 100")
	}
	if c.OperationTimeout <= 0 || c.OperationTimeout >= c.JobLease || c.OperationTimeout >= c.OutboxLease {
		return errors.New("WORKER_OPERATION_TIMEOUT must be positive and shorter than both leases")
	}
	if c.ShutdownTimeout < time.Second || c.ShutdownTimeout > 5*time.Minute {
		return errors.New("WORKER_SHUTDOWN_TIMEOUT must be between 1 second and 5 minutes")
	}
	return nil
}

func int64Value(key string, fallback int64) (int64, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return parsed, nil
}

// MetricsConfig controls the independent campaign-metric reconciliation worker.
// It observes the authoritative recipient/exclusion ledgers and records drift
// without racing the transactional counters maintained by delivery processing.
type MetricsConfig struct {
	Environment              string
	HealthAddr               string
	DatabaseDriver           string
	DatabaseURL              string
	DBMaxOpen                int
	DBMaxIdle                int
	DBConnMaxLifetime        time.Duration
	DBConnMaxIdleTime        time.Duration
	DBPingTimeout            time.Duration
	WorkerID                 string
	Concurrency              int
	ClaimBatch               int
	Lease                    time.Duration
	PollInterval             time.Duration
	MatchInterval            time.Duration
	DriftInterval            time.Duration
	FailureInterval          time.Duration
	OutcomeReconcileInterval time.Duration
	OutcomeReconcileWindow   time.Duration
	OutcomeReconcileBatch    int
	FinalUnknownWindow       time.Duration
	ShutdownTimeout          time.Duration
}

func LoadMetrics() (MetricsConfig, error) {
	environment := strings.ToLower(strings.TrimSpace(env("APP_ENV", "development")))
	if err := resolveWorkerFiles(environment); err != nil {
		return MetricsConfig{}, err
	}
	cfg := MetricsConfig{
		Environment:    environment,
		HealthAddr:     strings.TrimSpace(env("WORKER_HEALTH_ADDR", ":8096")),
		DatabaseDriver: strings.TrimSpace(env("POSTGRES_DRIVER", "postgres")),
		DatabaseURL:    strings.TrimSpace(os.Getenv("DATABASE_URL")),
		WorkerID:       strings.TrimSpace(env("WORKER_ID", hostname("metrics-worker"))),
	}
	var err error
	if cfg.DBMaxOpen, err = integer("DB_MAX_OPEN", 12); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.DBMaxIdle, err = integer("DB_MAX_IDLE", 4); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.Concurrency, err = integer("METRICS_WORKER_CONCURRENCY", 2); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.ClaimBatch, err = integer("METRICS_WORKER_CLAIM_BATCH", 2); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.DBConnMaxLifetime, err = duration("DB_CONN_MAX_LIFETIME", 30*time.Minute); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.DBConnMaxIdleTime, err = duration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.DBPingTimeout, err = duration("DB_PING_TIMEOUT", 5*time.Second); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.Lease, err = duration("METRICS_RECONCILIATION_LEASE", time.Minute); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.PollInterval, err = duration("METRICS_WORKER_POLL_INTERVAL", 5*time.Second); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.MatchInterval, err = duration("METRICS_MATCH_INTERVAL", time.Minute); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.DriftInterval, err = duration("METRICS_DRIFT_INTERVAL", 10*time.Second); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.FailureInterval, err = duration("METRICS_FAILURE_INTERVAL", time.Minute); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.OutcomeReconcileInterval, err = duration("DELIVERY_OUTCOME_RECONCILE_INTERVAL", time.Minute); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.OutcomeReconcileWindow, err = duration("DELIVERY_OUTCOME_RECONCILE_WINDOW", 15*time.Minute); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.OutcomeReconcileBatch, err = integer("DELIVERY_OUTCOME_RECONCILE_BATCH", 500); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.FinalUnknownWindow, err = duration("DELIVERY_FINAL_UNKNOWN_WINDOW", 24*time.Hour); err != nil {
		return MetricsConfig{}, err
	}
	if cfg.ShutdownTimeout, err = duration("WORKER_SHUTDOWN_TIMEOUT", 30*time.Second); err != nil {
		return MetricsConfig{}, err
	}
	if err := cfg.Validate(); err != nil {
		return MetricsConfig{}, err
	}
	return cfg, nil
}

func (c MetricsConfig) Validate() error {
	switch c.Environment {
	case "development", "test", "staging", "production":
	default:
		return fmt.Errorf("APP_ENV %q is unsupported", c.Environment)
	}
	if err := ValidateHealthAddress(c.HealthAddr); err != nil {
		return err
	}
	if c.DatabaseDriver == "" || c.DatabaseURL == "" {
		return errors.New("POSTGRES_DRIVER and DATABASE_URL are required")
	}
	if c.WorkerID == "" || len(c.WorkerID) > 128 {
		return errors.New("WORKER_ID must contain 1 to 128 characters")
	}
	if c.DBMaxOpen < 1 || c.DBMaxOpen > 100 || c.DBMaxIdle < 0 || c.DBMaxIdle > c.DBMaxOpen {
		return errors.New("database pool bounds are invalid")
	}
	if c.DBPingTimeout <= 0 || c.DBPingTimeout > 30*time.Second || c.DBConnMaxLifetime <= 0 || c.DBConnMaxIdleTime <= 0 {
		return errors.New("database connection timing is invalid")
	}
	if c.Concurrency < 1 || c.Concurrency > 32 || c.ClaimBatch < 1 || c.ClaimBatch > c.Concurrency {
		return errors.New("metrics worker concurrency or claim batch is invalid")
	}
	if c.Lease < 15*time.Second || c.Lease > 10*time.Minute {
		return errors.New("METRICS_RECONCILIATION_LEASE must be between 15 seconds and 10 minutes")
	}
	if c.PollInterval <= 0 || c.MatchInterval <= 0 || c.DriftInterval <= 0 || c.FailureInterval <= 0 || c.OutcomeReconcileInterval <= 0 {
		return errors.New("metrics worker intervals must be positive")
	}
	if c.OutcomeReconcileWindow <= 0 || c.FinalUnknownWindow < c.OutcomeReconcileWindow || c.FinalUnknownWindow > 7*24*time.Hour {
		return errors.New("delivery outcome reconciliation windows are invalid")
	}
	if c.OutcomeReconcileBatch < 1 || c.OutcomeReconcileBatch > 5000 {
		return errors.New("DELIVERY_OUTCOME_RECONCILE_BATCH must be between 1 and 5000")
	}
	if c.DriftInterval > c.MatchInterval {
		return errors.New("METRICS_DRIFT_INTERVAL must not exceed METRICS_MATCH_INTERVAL")
	}
	if c.ShutdownTimeout < time.Second || c.ShutdownTimeout > 5*time.Minute {
		return errors.New("WORKER_SHUTDOWN_TIMEOUT must be between 1 second and 5 minutes")
	}
	return nil
}

// GovernanceConfig controls scheduled inbound privacy retention and resumable
// content-key rotation. It is intentionally isolated from interactive API
// credentials and accepts only database and encryption material.
type GovernanceConfig struct {
	Environment             string
	HealthAddr              string
	DatabaseDriver          string
	DatabaseURL             string
	DBMaxOpen               int
	DBMaxIdle               int
	DBConnMaxLifetime       time.Duration
	DBConnMaxIdleTime       time.Duration
	DBPingTimeout           time.Duration
	WorkerID                string
	InboundContentKeysJSON  string
	InboundContentActiveKey string
	RotationBatchSize       int
	RotationLease           time.Duration
	RotationPollInterval    time.Duration
	RetentionSweepInterval  time.Duration
	ShutdownTimeout         time.Duration
}

func LoadGovernance() (GovernanceConfig, error) {
	environment := strings.ToLower(strings.TrimSpace(env("APP_ENV", "development")))
	if err := resolveWorkerFiles(environment); err != nil {
		return GovernanceConfig{}, err
	}
	cfg := GovernanceConfig{
		Environment:             environment,
		HealthAddr:              strings.TrimSpace(env("WORKER_HEALTH_ADDR", ":8094")),
		DatabaseDriver:          strings.TrimSpace(env("POSTGRES_DRIVER", "postgres")),
		DatabaseURL:             strings.TrimSpace(os.Getenv("DATABASE_URL")),
		WorkerID:                strings.TrimSpace(env("WORKER_ID", hostname("inbound-governance-worker"))),
		InboundContentKeysJSON:  strings.TrimSpace(os.Getenv("INBOUND_CONTENT_KEYS_JSON")),
		InboundContentActiveKey: strings.TrimSpace(env("INBOUND_CONTENT_ACTIVE_KEY_VERSION", "v1")),
	}
	var err error
	if cfg.DBMaxOpen, err = integer("DB_MAX_OPEN", 8); err != nil {
		return GovernanceConfig{}, err
	}
	if cfg.DBMaxIdle, err = integer("DB_MAX_IDLE", 2); err != nil {
		return GovernanceConfig{}, err
	}
	if cfg.DBConnMaxLifetime, err = duration("DB_CONN_MAX_LIFETIME", 30*time.Minute); err != nil {
		return GovernanceConfig{}, err
	}
	if cfg.DBConnMaxIdleTime, err = duration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute); err != nil {
		return GovernanceConfig{}, err
	}
	if cfg.DBPingTimeout, err = duration("DB_PING_TIMEOUT", 5*time.Second); err != nil {
		return GovernanceConfig{}, err
	}
	if cfg.RotationBatchSize, err = integer("INBOUND_ROTATION_BATCH_SIZE", 100); err != nil {
		return GovernanceConfig{}, err
	}
	if cfg.RotationLease, err = duration("INBOUND_ROTATION_LEASE", time.Minute); err != nil {
		return GovernanceConfig{}, err
	}
	if cfg.RotationPollInterval, err = duration("INBOUND_ROTATION_POLL_INTERVAL", 5*time.Second); err != nil {
		return GovernanceConfig{}, err
	}
	if cfg.RetentionSweepInterval, err = duration("INBOUND_RETENTION_SWEEP_INTERVAL", time.Hour); err != nil {
		return GovernanceConfig{}, err
	}
	if cfg.ShutdownTimeout, err = duration("WORKER_SHUTDOWN_TIMEOUT", 30*time.Second); err != nil {
		return GovernanceConfig{}, err
	}
	if err := cfg.Validate(); err != nil {
		return GovernanceConfig{}, err
	}
	return cfg, nil
}
func (c GovernanceConfig) Validate() error {
	switch c.Environment {
	case "development", "test", "staging", "production":
	default:
		return fmt.Errorf("APP_ENV %q is unsupported", c.Environment)
	}
	if err := ValidateHealthAddress(c.HealthAddr); err != nil {
		return err
	}
	if c.DatabaseDriver == "" || c.DatabaseURL == "" {
		return errors.New("POSTGRES_DRIVER and DATABASE_URL are required")
	}
	if c.WorkerID == "" || len(c.WorkerID) > 128 {
		return errors.New("WORKER_ID must contain 1 to 128 characters")
	}
	if c.DBMaxOpen < 1 || c.DBMaxOpen > 50 || c.DBMaxIdle < 0 || c.DBMaxIdle > c.DBMaxOpen {
		return errors.New("database pool bounds are invalid")
	}
	if c.DBPingTimeout <= 0 || c.DBPingTimeout > 30*time.Second || c.DBConnMaxLifetime <= 0 || c.DBConnMaxIdleTime <= 0 {
		return errors.New("database connection timing is invalid")
	}
	if c.InboundContentKeysJSON == "" || c.InboundContentActiveKey == "" {
		return errors.New("versioned inbound content keyring configuration is required")
	}
	if c.RotationBatchSize < 1 || c.RotationBatchSize > 1000 {
		return errors.New("INBOUND_ROTATION_BATCH_SIZE must be between 1 and 1000")
	}
	if c.RotationLease < 15*time.Second || c.RotationLease > 10*time.Minute {
		return errors.New("INBOUND_ROTATION_LEASE must be between 15 seconds and 10 minutes")
	}
	if c.RotationPollInterval <= 0 || c.RetentionSweepInterval < time.Minute {
		return errors.New("governance worker intervals are invalid")
	}
	if c.ShutdownTimeout < time.Second || c.ShutdownTimeout > 5*time.Minute {
		return errors.New("WORKER_SHUTDOWN_TIMEOUT must be between 1 second and 5 minutes")
	}
	return nil
}

// PlatformGovernanceConfig controls the independent retention and operational
// alert worker. It intentionally has no browser-session, administrator or
// messaging-provider credentials; the worker needs only PostgreSQL and the
// object-store credentials required for governed retention actions.
type PlatformGovernanceConfig struct {
	Environment             string
	ObjectStoreDriver       string
	HealthAddr              string
	DatabaseDriver          string
	DatabaseURL             string
	DBMaxOpen               int
	DBMaxIdle               int
	DBConnMaxLifetime       time.Duration
	DBConnMaxIdleTime       time.Duration
	DBPingTimeout           time.Duration
	WorkerID                string
	ObjectStoreRoot         string
	RetentionBatch          int
	RetentionLease          time.Duration
	RetentionPollInterval   time.Duration
	AlertEvaluationInterval time.Duration
	GatewayStaleAfter       time.Duration
	AlertEscalationInterval time.Duration
	AlertEscalationBatch    int
	AlertEscalationLease    time.Duration
	FailureBackoff          time.Duration
	ShutdownTimeout         time.Duration
}

func LoadPlatformGovernance() (PlatformGovernanceConfig, error) {
	environment := strings.ToLower(strings.TrimSpace(env("APP_ENV", "development")))
	if err := resolveWorkerFiles(environment); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	cfg := PlatformGovernanceConfig{
		Environment:       environment,
		ObjectStoreDriver: strings.ToLower(strings.TrimSpace(env("OBJECT_STORE_DRIVER", "filesystem"))),
		HealthAddr:        strings.TrimSpace(env("WORKER_HEALTH_ADDR", ":8095")),
		DatabaseDriver:    strings.TrimSpace(env("POSTGRES_DRIVER", "postgres")),
		DatabaseURL:       strings.TrimSpace(os.Getenv("DATABASE_URL")),
		WorkerID:          strings.TrimSpace(env("WORKER_ID", hostname("platform-governance-worker"))),
		ObjectStoreRoot:   strings.TrimSpace(os.Getenv("OBJECT_STORE_ROOT")),
	}
	var err error
	if cfg.DBMaxOpen, err = integer("DB_MAX_OPEN", 12); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.DBMaxIdle, err = integer("DB_MAX_IDLE", 4); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.DBConnMaxLifetime, err = duration("DB_CONN_MAX_LIFETIME", 30*time.Minute); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.DBConnMaxIdleTime, err = duration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.DBPingTimeout, err = duration("DB_PING_TIMEOUT", 5*time.Second); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.RetentionBatch, err = integer("PLATFORM_RETENTION_BATCH", 50); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.RetentionLease, err = duration("PLATFORM_RETENTION_LEASE", 2*time.Minute); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.RetentionPollInterval, err = duration("PLATFORM_RETENTION_POLL_INTERVAL", time.Minute); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.AlertEvaluationInterval, err = duration("ALERT_EVALUATION_INTERVAL", 30*time.Second); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	var gatewayStaleSeconds int
	if gatewayStaleSeconds, err = integer("GATEWAY_STALE_AFTER_SECONDS", 120); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	cfg.GatewayStaleAfter = time.Duration(gatewayStaleSeconds) * time.Second
	if cfg.AlertEscalationInterval, err = duration("ALERT_ESCALATION_INTERVAL", 30*time.Second); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.AlertEscalationBatch, err = integer("ALERT_ESCALATION_BATCH", 100); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.AlertEscalationLease, err = duration("ALERT_ESCALATION_LEASE", time.Minute); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.FailureBackoff, err = duration("PLATFORM_GOVERNANCE_FAILURE_BACKOFF", 10*time.Second); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if cfg.ShutdownTimeout, err = duration("WORKER_SHUTDOWN_TIMEOUT", 30*time.Second); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	if err := cfg.Validate(); err != nil {
		return PlatformGovernanceConfig{}, err
	}
	return cfg, nil
}

func (c PlatformGovernanceConfig) Validate() error {
	switch c.Environment {
	case "development", "test", "staging", "production":
	default:
		return fmt.Errorf("APP_ENV %q is unsupported", c.Environment)
	}
	if err := ValidateHealthAddress(c.HealthAddr); err != nil {
		return err
	}
	if c.DatabaseDriver == "" || c.DatabaseURL == "" {
		return errors.New("POSTGRES_DRIVER and DATABASE_URL are required")
	}
	if c.WorkerID == "" || len(c.WorkerID) > 128 {
		return errors.New("WORKER_ID must contain 1 to 128 characters")
	}
	switch c.ObjectStoreDriver {
	case "filesystem":
		if c.ObjectStoreRoot == "" {
			return errors.New("OBJECT_STORE_ROOT is required for filesystem storage")
		}
		if c.Environment == "staging" || c.Environment == "production" {
			if !filepath.IsAbs(c.ObjectStoreRoot) {
				return errors.New("deployed platform governance workers require an absolute OBJECT_STORE_ROOT for filesystem storage")
			}
			root := filepath.Clean(c.ObjectStoreRoot)
			temp := filepath.Clean(os.TempDir())
			if rel, err := filepath.Rel(temp, root); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return errors.New("deployed platform governance workers must not use a temporary OBJECT_STORE_ROOT")
			}
		}
	case "s3", "minio":
	default:
		return errors.New("OBJECT_STORE_DRIVER must be filesystem, s3, or minio")
	}
	if c.DBMaxOpen < 1 || c.DBMaxOpen > 100 || c.DBMaxIdle < 0 || c.DBMaxIdle > c.DBMaxOpen {
		return errors.New("database pool bounds are invalid")
	}
	if c.DBPingTimeout <= 0 || c.DBPingTimeout > 30*time.Second || c.DBConnMaxLifetime <= 0 || c.DBConnMaxIdleTime <= 0 {
		return errors.New("database connection timing is invalid")
	}
	if c.RetentionBatch < 1 || c.RetentionBatch > 500 {
		return errors.New("PLATFORM_RETENTION_BATCH must be between 1 and 500")
	}
	if c.RetentionLease < 15*time.Second || c.RetentionLease > 10*time.Minute {
		return errors.New("PLATFORM_RETENTION_LEASE must be between 15 seconds and 10 minutes")
	}
	if c.AlertEscalationBatch < 1 || c.AlertEscalationBatch > 500 {
		return errors.New("ALERT_ESCALATION_BATCH must be between 1 and 500")
	}
	if c.AlertEscalationLease < 15*time.Second || c.AlertEscalationLease > 10*time.Minute {
		return errors.New("ALERT_ESCALATION_LEASE must be between 15 seconds and 10 minutes")
	}
	if c.GatewayStaleAfter < 30*time.Second || c.GatewayStaleAfter > time.Hour {
		return errors.New("GATEWAY_STALE_AFTER_SECONDS must be between 30 and 3600")
	}
	if c.RetentionPollInterval <= 0 || c.AlertEvaluationInterval <= 0 || c.AlertEscalationInterval <= 0 || c.FailureBackoff <= 0 {
		return errors.New("platform governance worker intervals must be positive")
	}
	if c.ShutdownTimeout < time.Second || c.ShutdownTimeout > 5*time.Minute {
		return errors.New("WORKER_SHUTDOWN_TIMEOUT must be between 1 second and 5 minutes")
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isDisallowedDeployedHostname(hostname string) bool {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(hostname), "[]"))
	if normalized == "control-api" || normalized == "openwa-gateway" || normalized == "localhost" ||
		strings.HasSuffix(normalized, ".localhost") || normalized == "host.docker.internal" || strings.HasSuffix(normalized, ".docker.internal") {
		return true
	}
	ip := net.ParseIP(normalized)
	return ip != nil && (ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified())
}
