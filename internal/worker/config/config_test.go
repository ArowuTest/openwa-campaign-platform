package config

import (
	"encoding/base64"
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
	if cfg.Concurrency != 2 || cfg.ClaimBatch != 2 || cfg.DriftInterval > cfg.MatchInterval {
		t.Fatalf("unexpected defaults: %+v", cfg)
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
