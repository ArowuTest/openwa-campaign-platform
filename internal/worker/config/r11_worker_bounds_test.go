package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadAudienceRejectsInvalidMergeBoundsAndHealthAddress(t *testing.T) {
	t.Run("merge concurrency", func(t *testing.T) {
		validAudienceEnv(t)
		t.Setenv("AUDIENCE_MERGE_CONCURRENCY", "0")
		if _, err := LoadAudience(); err == nil || !strings.Contains(err.Error(), "MERGE") {
			t.Fatalf("invalid merge concurrency accepted: %v", err)
		}
	})
	t.Run("merge claim batch", func(t *testing.T) {
		validAudienceEnv(t)
		t.Setenv("AUDIENCE_MERGE_CONCURRENCY", "2")
		t.Setenv("AUDIENCE_MERGE_CLAIM_BATCH", "3")
		if _, err := LoadAudience(); err == nil || !strings.Contains(err.Error(), "MERGE") {
			t.Fatalf("invalid merge claim batch accepted: %v", err)
		}
	})
	t.Run("merge lease", func(t *testing.T) {
		validAudienceEnv(t)
		t.Setenv("AUDIENCE_MERGE_LEASE_DURATION", "1ns")
		if _, err := LoadAudience(); err == nil || !strings.Contains(err.Error(), "MERGE") {
			t.Fatalf("invalid merge lease accepted: %v", err)
		}
	})
	t.Run("merge poll", func(t *testing.T) {
		validAudienceEnv(t)
		t.Setenv("AUDIENCE_MERGE_POLL_INTERVAL", "0s")
		if _, err := LoadAudience(); err == nil || !strings.Contains(err.Error(), "MERGE") {
			t.Fatalf("invalid merge poll accepted: %v", err)
		}
	})
	t.Run("merge failure backoff", func(t *testing.T) {
		validAudienceEnv(t)
		t.Setenv("AUDIENCE_MERGE_FAILURE_BACKOFF", "-1s")
		if _, err := LoadAudience(); err == nil || !strings.Contains(err.Error(), "MERGE") {
			t.Fatalf("invalid merge failure backoff accepted: %v", err)
		}
	})
	t.Run("health address", func(t *testing.T) {
		validAudienceEnv(t)
		t.Setenv("WORKER_HEALTH_ADDR", "not-a-bind-address")
		if _, err := LoadAudience(); err == nil || !strings.Contains(err.Error(), "WORKER_HEALTH_ADDR") {
			t.Fatalf("invalid audience health address accepted: %v", err)
		}
	})
}

func TestLoadCampaignRejectsInvalidDispatchBackpressureBounds(t *testing.T) {
	t.Run("limit", func(t *testing.T) {
		validCampaignEnv(t)
		t.Setenv("DISPATCH_QUEUE_BACKPRESSURE_LIMIT", "0")
		if _, err := LoadCampaign(); err == nil || !strings.Contains(err.Error(), "BACKPRESSURE") {
			t.Fatalf("invalid backpressure limit accepted: %v", err)
		}
	})
	t.Run("retry", func(t *testing.T) {
		validCampaignEnv(t)
		t.Setenv("DISPATCH_QUEUE_BACKPRESSURE_RETRY", "0s")
		if _, err := LoadCampaign(); err == nil || !strings.Contains(err.Error(), "BACKPRESSURE") {
			t.Fatalf("invalid backpressure retry accepted: %v", err)
		}
	})
}

func TestLoadPlatformGovernanceProductionRequiresConfiguredObjectStoreRoot(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://example")
	if err := os.Unsetenv("OBJECT_STORE_ROOT"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPlatformGovernance(); err == nil || !strings.Contains(err.Error(), "OBJECT_STORE_ROOT") {
		t.Fatalf("production accepted implicit temporary object-store root: %v", err)
	}
}

func TestValidateHealthAddressRejectsOutOfRangePorts(t *testing.T) {
	for _, address := range []string{":0", ":65536", "127.0.0.1:70000"} {
		if err := ValidateHealthAddress(address); err == nil {
			t.Fatalf("invalid health address %q was accepted", address)
		}
	}
}
