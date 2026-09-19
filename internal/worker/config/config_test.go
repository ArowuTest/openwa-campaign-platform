package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validAudienceEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("OBJECT_STORE_ROOT", t.TempDir())
	t.Setenv("MSISDN_ENCRYPTION_KEY_BASE64", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("MSISDN_LOOKUP_KEY_BASE64", base64.StdEncoding.EncodeToString(make([]byte, 32)))
}

func TestLoadAudienceAcceptsStrictConfiguration(t *testing.T) {
	validAudienceEnv(t)
	cfg, err := LoadAudience()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Concurrency != 2 || cfg.ClaimBatch != 2 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadAudienceRejectsMissingStableKeys(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("OBJECT_STORE_ROOT", t.TempDir())
	_, err := LoadAudience()
	if err == nil || !strings.Contains(err.Error(), "MSISDN_ENCRYPTION_KEY_BASE64") {
		t.Fatalf("expected key error, got %v", err)
	}
}

func TestLoadAudienceRejectsClaimBatchAboveConcurrency(t *testing.T) {
	validAudienceEnv(t)
	t.Setenv("AUDIENCE_WORKER_CONCURRENCY", "2")
	t.Setenv("AUDIENCE_WORKER_CLAIM_BATCH", "3")
	_, err := LoadAudience()
	if err == nil || !strings.Contains(err.Error(), "CLAIM_BATCH") {
		t.Fatalf("expected claim bound error, got %v", err)
	}
}

func TestLoadAudienceRejectsMalformedInteger(t *testing.T) {
	validAudienceEnv(t)
	t.Setenv("DB_MAX_OPEN", "many")
	if _, err := LoadAudience(); err == nil {
		t.Fatal("expected malformed integer to fail")
	}
}

func validCampaignEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("OPENWA_GATEWAY_URL", "http://openwa-gateway:2785")
	t.Setenv("GATEWAY_COMMAND_SECRET", "01234567890123456789012345678901")
	t.Setenv("MEDIA_DOWNLOAD_BASE_URL", "http://control-api:8080/api/v1/internal/media")
	t.Setenv("MEDIA_DOWNLOAD_SECRET", "abcdefghijklmnopqrstuvwxyz012345")
	t.Setenv("MSISDN_ENCRYPTION_KEY_BASE64", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("MSISDN_LOOKUP_KEY_BASE64", base64.StdEncoding.EncodeToString(make([]byte, 32)))
}

func TestLoadCampaignAcceptsBoundedConfiguration(t *testing.T) {
	validCampaignEnv(t)
	cfg, err := LoadCampaign()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JobClaimBatch > cfg.JobConcurrency || cfg.OutboxClaimBatch > cfg.OutboxConcurrency {
		t.Fatalf("invalid bounds: %+v", cfg)
	}
}

func TestLoadCampaignMetaConversationWindowConfiguration(t *testing.T) {
	validCampaignEnv(t)
	cfg, err := LoadCampaign()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetaConversationWindow != 24*time.Hour {
		t.Fatalf("default Meta conversation window=%s want=24h", cfg.MetaConversationWindow)
	}
	t.Setenv("META_CLOUD_CONVERSATION_WINDOW", "12h")
	cfg, err = LoadCampaign()
	if err != nil || cfg.MetaConversationWindow != 12*time.Hour {
		t.Fatalf("configured Meta conversation window=%s err=%v", cfg.MetaConversationWindow, err)
	}
	for _, invalid := range []string{"0s", "25h"} {
		t.Setenv("META_CLOUD_CONVERSATION_WINDOW", invalid)
		if _, err := LoadCampaign(); err == nil {
			t.Fatalf("invalid Meta conversation window %q was accepted", invalid)
		}
	}
}

func TestLoadCampaignMetaHealthStaleAfterConfiguration(t *testing.T) {
	validCampaignEnv(t)
	cfg, err := LoadCampaign()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetaHealthStaleAfter != 5*time.Minute {
		t.Fatalf("default Meta health window=%s want=5m", cfg.MetaHealthStaleAfter)
	}
	t.Setenv("META_CLOUD_HEALTH_STALE_AFTER", "7m")
	cfg, err = LoadCampaign()
	if err != nil || cfg.MetaHealthStaleAfter != 7*time.Minute {
		t.Fatalf("configured Meta health window=%s err=%v", cfg.MetaHealthStaleAfter, err)
	}
	t.Setenv("META_CLOUD_HEALTH_STALE_AFTER", "0s")
	if _, err := LoadCampaign(); err == nil {
		t.Fatal("zero Meta health staleness window was accepted")
	}
}
func TestLoadCampaignRejectsInvalidGatewayURL(t *testing.T) {
	validCampaignEnv(t)
	t.Setenv("OPENWA_GATEWAY_URL", "javascript:bad")
	if _, err := LoadCampaign(); err == nil {
		t.Fatal("expected gateway URL error")
	}
}

func TestLoadCampaignRejectsOperationTimeoutAtLease(t *testing.T) {
	validCampaignEnv(t)
	t.Setenv("CAMPAIGN_JOB_LEASE", "15s")
	t.Setenv("WORKER_OPERATION_TIMEOUT", "15s")
	if _, err := LoadCampaign(); err == nil {
		t.Fatal("expected operation timeout bound error")
	}
}

func validMetricsEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example")
}

func TestLoadMetricsAcceptsBoundedConfiguration(t *testing.T) {
	validMetricsEnv(t)
	cfg, err := LoadMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Concurrency != 2 || cfg.ClaimBatch != 2 || cfg.OutcomeReconcileBatch != 500 || cfg.DriftInterval > cfg.MatchInterval {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadMetricsRejectsInvalidOutcomeReconcileBatch(t *testing.T) {
	validMetricsEnv(t)
	t.Setenv("DELIVERY_OUTCOME_RECONCILE_BATCH", "5001")
	if _, err := LoadMetrics(); err == nil {
		t.Fatal("expected delivery outcome reconciliation batch bound error")
	}
}

func TestLoadMetricsRejectsClaimBatchAboveConcurrency(t *testing.T) {
	validMetricsEnv(t)
	t.Setenv("METRICS_WORKER_CONCURRENCY", "2")
	t.Setenv("METRICS_WORKER_CLAIM_BATCH", "3")
	if _, err := LoadMetrics(); err == nil {
		t.Fatal("expected metrics claim bound error")
	}
}

func TestLoadMetricsRejectsDriftIntervalAboveMatchInterval(t *testing.T) {
	validMetricsEnv(t)
	t.Setenv("METRICS_MATCH_INTERVAL", "10s")
	t.Setenv("METRICS_DRIFT_INTERVAL", "20s")
	if _, err := LoadMetrics(); err == nil {
		t.Fatal("expected metrics interval error")
	}
}

func TestLoadPlatformGovernanceAcceptsBoundedConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("OBJECT_STORE_ROOT", t.TempDir())
	cfg, err := LoadPlatformGovernance()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetentionBatch != 50 || cfg.AlertEscalationBatch != 100 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadPlatformGovernanceRejectsShortLeases(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("OBJECT_STORE_ROOT", t.TempDir())
	t.Setenv("ALERT_ESCALATION_LEASE", "5s")
	if _, err := LoadPlatformGovernance(); err == nil {
		t.Fatal("expected short escalation lease to fail")
	}
}

func TestLoadPlatformGovernanceGatewayStaleAfterSecondsIsStrictAndBounded(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("OBJECT_STORE_ROOT", t.TempDir())
	cfg, err := LoadPlatformGovernance()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GatewayStaleAfter != 120*time.Second {
		t.Fatalf("unexpected gateway stale default: %s", cfg.GatewayStaleAfter)
	}
	t.Setenv("GATEWAY_STALE_AFTER_SECONDS", "300")
	cfg, err = LoadPlatformGovernance()
	if err != nil || cfg.GatewayStaleAfter != 300*time.Second {
		t.Fatalf("valid gateway stale override rejected: %+v %v", cfg, err)
	}
	t.Setenv("GATEWAY_STALE_AFTER_SECONDS", "3601")
	if _, err = LoadPlatformGovernance(); err == nil || !strings.Contains(err.Error(), "GATEWAY_STALE_AFTER_SECONDS") {
		t.Fatalf("expected unsafe gateway stale threshold rejection, got %v", err)
	}
}

func TestLoadCampaignResolvesOptionalMetaCredentialsFromFile(t *testing.T) {
	validCampaignEnv(t)
	raw := `[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`
	path := filepath.Join(t.TempDir(), "meta-worker-credentials.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("META_CLOUD_CREDENTIALS_JSON", "")
	t.Setenv("META_CLOUD_CREDENTIALS_JSON_FILE", path)
	cfg, err := LoadCampaign()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetaCloudCredentialsJSON != raw {
		t.Fatal("campaign worker did not resolve Meta credentials from secret file")
	}
}

func TestLoadCampaignAllowsMetaOnlyTransportConfiguration(t *testing.T) {
	validCampaignEnv(t)
	t.Setenv("OPENWA_GATEWAY_URL", "")
	t.Setenv("GATEWAY_COMMAND_SECRET", "")
	t.Setenv("OPENWA_GATEWAY_API_KEY", "")
	t.Setenv("META_CLOUD_CREDENTIALS_JSON", `[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	cfg, err := LoadCampaign()
	if err != nil {
		t.Fatalf("Meta-only campaign worker configuration was rejected: %v", err)
	}
	if cfg.GatewayURL != "" || cfg.GatewayCommandSecret != "" || cfg.MetaCloudCredentialsJSON == "" {
		t.Fatalf("unexpected Meta-only transport configuration: %+v", cfg)
	}
}

func TestLoadCampaignRejectsNoConfiguredTransport(t *testing.T) {
	validCampaignEnv(t)
	t.Setenv("OPENWA_GATEWAY_URL", "")
	t.Setenv("GATEWAY_COMMAND_SECRET", "")
	t.Setenv("OPENWA_GATEWAY_API_KEY", "")
	t.Setenv("META_CLOUD_CREDENTIALS_JSON", "")
	if _, err := LoadCampaign(); err == nil {
		t.Fatal("campaign worker accepted configuration with no messaging transport")
	}
}

func TestLoadCampaignRejectsMalformedMetaTransportCredentials(t *testing.T) {
	validCampaignEnv(t)
	t.Setenv("OPENWA_GATEWAY_URL", "")
	t.Setenv("GATEWAY_COMMAND_SECRET", "")
	t.Setenv("OPENWA_GATEWAY_API_KEY", "")
	t.Setenv("META_CLOUD_CREDENTIALS_JSON", `[{"key":"meta-ng","accessToken":"short"}]`)
	if _, err := LoadCampaign(); err == nil {
		t.Fatal("campaign worker accepted malformed Meta credentials")
	}
}

func TestLoadCampaignAllowsGovernedNodeAddressingWithoutStaticGatewayURL(t *testing.T) {
	validCampaignEnv(t)
	t.Setenv("OPENWA_GATEWAY_URL", "")
	cfg, err := LoadCampaign()
	if err != nil {
		t.Fatalf("governed node-addressed OpenWA configuration was rejected: %v", err)
	}
	if cfg.GatewayURL != "" || cfg.GatewayCommandSecret == "" {
		t.Fatalf("unexpected node-addressed OpenWA configuration: %+v", cfg)
	}
}

func TestLoadPlatformGovernanceAllowsS3WithoutFilesystemRoot(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("OBJECT_STORE_DRIVER", "s3")
	t.Setenv("OBJECT_STORE_ROOT", "")
	cfg, err := LoadPlatformGovernance()
	if err != nil {
		t.Fatalf("S3 platform governance configuration was rejected: %v", err)
	}
	if cfg.ObjectStoreDriver != "s3" || cfg.ObjectStoreRoot != "" {
		t.Fatalf("unexpected platform governance object store config: %+v", cfg)
	}
}

func TestLoadCampaignProductionRejectsHTTPMediaDownloadURL(t *testing.T) {
	validCampaignEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("MEDIA_DOWNLOAD_BASE_URL", "http://control-api:8080/api/v1/internal/media")
	if _, err := LoadCampaign(); err == nil {
		t.Fatal("production campaign worker accepted an HTTP/Docker-internal media download URL")
	}
}

func TestLoadCampaignProductionRejectsDockerLocalHTTPSMediaDownloadURL(t *testing.T) {
	validCampaignEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("OPENWA_GATEWAY_URL", "")
	t.Setenv("MEDIA_DOWNLOAD_BASE_URL", "https://control-api/api/v1/internal/media")
	if _, err := LoadCampaign(); err == nil {
		t.Fatal("production campaign worker accepted Docker-local HTTPS media download URL")
	}
}

func TestLoadCampaignProductionRejectsSpecialDockerHostnameMediaDownloadURL(t *testing.T) {
	validCampaignEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("OPENWA_GATEWAY_URL", "")
	for _, raw := range []string{
		"https://host.docker.internal/api/v1/internal/media",
		"https://bridge.docker.internal/api/v1/internal/media",
		"https://127.0.0.1/api/v1/internal/media",
		"https://169.254.10.20/api/v1/internal/media",
	} {
		t.Setenv("MEDIA_DOWNLOAD_BASE_URL", raw)
		if _, err := LoadCampaign(); err == nil {
			t.Fatalf("production campaign worker accepted non-production media endpoint %q", raw)
		}
	}
}

func TestLoadMetricsDefaultHealthAddressMatchesRailwayServiceContract(t *testing.T) {
	previous, hadPrevious := os.LookupEnv("WORKER_HEALTH_ADDR")
	if err := os.Unsetenv("WORKER_HEALTH_ADDR"); err != nil {
		t.Fatalf("unset WORKER_HEALTH_ADDR: %v", err)
	}
	t.Cleanup(func() {
		if hadPrevious {
			_ = os.Setenv("WORKER_HEALTH_ADDR", previous)
			return
		}
		_ = os.Unsetenv("WORKER_HEALTH_ADDR")
	})
	validMetricsEnv(t)

	cfg, err := LoadMetrics()
	if err != nil {
		t.Fatalf("LoadMetrics() error = %v", err)
	}
	if cfg.HealthAddr != ":8096" {
		t.Fatalf("LoadMetrics() default HealthAddr = %q, want %q", cfg.HealthAddr, ":8096")
	}
}

func TestLoadCampaignProductionRejectsStaticGatewayURL(t *testing.T) {
	validCampaignEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("OPENWA_GATEWAY_URL", "https://gateway.example.internal")
	t.Setenv("MEDIA_DOWNLOAD_BASE_URL", "https://media.example.internal/api/v1/internal/media")
	if _, err := LoadCampaign(); err == nil {
		t.Fatal("production campaign worker accepted residual static OPENWA_GATEWAY_URL")
	}
}
