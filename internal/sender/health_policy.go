package sender

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"campaign-platform/internal/platformpolicy"
)

const HealthPolicyConfigurationKey = "SENDER.HEALTH_THRESHOLDS"

type PlatformHealthPolicyResolver struct {
	Configurations *platformpolicy.ConfigurationAdministration
}

type healthPolicyConfiguration struct {
	HeartbeatStaleSeconds    int64 `json:"heartbeatStaleSeconds"`
	HeartbeatCriticalSeconds int64 `json:"heartbeatCriticalSeconds"`
	SuccessStaleSeconds      int64 `json:"successStaleSeconds"`
	SuccessCriticalSeconds   int64 `json:"successCriticalSeconds"`
	FailureWindowSeconds     int64 `json:"failureWindowSeconds"`
	FailureMinimumSamples    int   `json:"failureMinimumSamples"`
	FailureRateThresholdBPS  int   `json:"failureRateThresholdBps"`
	DisconnectWindowSeconds  int64 `json:"disconnectWindowSeconds"`
	DisconnectThreshold      int   `json:"disconnectThreshold"`
	CapacityNearLimitPercent int   `json:"capacityNearLimitPercent"`
}

func (r *PlatformHealthPolicyResolver) ResolveHealthPolicy(ctx context.Context, session GovernedSession, at time.Time) (HealthPolicy, HealthPolicyEvidence, error) {
	if r == nil || r.Configurations == nil {
		return HealthPolicy{}, HealthPolicyEvidence{}, errors.New("health policy configuration resolver is required")
	}
	scopes := make([]platformpolicy.OperationalScopeRef, 0, 3)
	if strings.TrimSpace(session.ID) != "" {
		scopes = append(scopes, platformpolicy.OperationalScopeRef{Type: platformpolicy.ScopeSenderSession, ID: session.ID})
	}
	if strings.TrimSpace(session.PoolID) != "" {
		scopes = append(scopes, platformpolicy.OperationalScopeRef{Type: platformpolicy.ScopeSenderPool, ID: session.PoolID})
	}
	if strings.TrimSpace(session.GatewayPoolID) != "" {
		scopes = append(scopes, platformpolicy.OperationalScopeRef{Type: platformpolicy.ScopeGatewayPool, ID: session.GatewayPoolID})
	}
	configuration, err := r.Configurations.Resolve(ctx, HealthPolicyConfigurationKey, scopes, at.UTC())
	if errors.Is(err, platformpolicy.ErrNotFound) {
		return DefaultHealthPolicy(), HealthPolicyEvidence{Source: "SAFE_DEFAULT"}, nil
	}
	if err != nil {
		return HealthPolicy{}, HealthPolicyEvidence{}, err
	}
	if err := r.Configurations.ValidateResolvedChecksum(configuration); err != nil {
		return HealthPolicy{}, HealthPolicyEvidence{}, err
	}
	policy, err := decodeHealthPolicy(configuration.Value)
	if err != nil {
		return HealthPolicy{}, HealthPolicyEvidence{}, fmt.Errorf("decode active sender health configuration %s: %w", configuration.ID, err)
	}
	return policy, HealthPolicyEvidence{
		Source: "GOVERNED_CONFIGURATION", ConfigurationID: configuration.ID,
		ScopeType: string(configuration.ScopeType), ScopeID: configuration.ScopeID, Version: configuration.Version,
	}, nil
}

func decodeHealthPolicy(raw []byte) (HealthPolicy, error) {
	defaults := DefaultHealthPolicy()
	input := healthPolicyConfiguration{
		HeartbeatStaleSeconds: int64(defaults.HeartbeatStaleAfter / time.Second), HeartbeatCriticalSeconds: int64(defaults.HeartbeatCriticalAfter / time.Second),
		SuccessStaleSeconds: int64(defaults.SuccessStaleAfter / time.Second), SuccessCriticalSeconds: int64(defaults.SuccessCriticalAfter / time.Second),
		FailureWindowSeconds: int64(defaults.FailureWindow / time.Second), FailureMinimumSamples: defaults.FailureMinimumSamples, FailureRateThresholdBPS: defaults.FailureRateThresholdBPS,
		DisconnectWindowSeconds: int64(defaults.DisconnectWindow / time.Second), DisconnectThreshold: defaults.DisconnectThreshold,
		CapacityNearLimitPercent: defaults.CapacityNearLimitPercent,
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return HealthPolicy{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return HealthPolicy{}, errors.New("sender health policy must contain one JSON object")
	}
	if input.HeartbeatStaleSeconds < 10 || input.HeartbeatStaleSeconds > 3600 ||
		input.HeartbeatCriticalSeconds <= input.HeartbeatStaleSeconds || input.HeartbeatCriticalSeconds > 7200 {
		return HealthPolicy{}, errors.New("sender heartbeat thresholds are outside safe bounds")
	}
	if input.SuccessStaleSeconds < 60 || input.SuccessStaleSeconds > int64((7*24*time.Hour)/time.Second) ||
		input.SuccessCriticalSeconds <= input.SuccessStaleSeconds || input.SuccessCriticalSeconds > int64((30*24*time.Hour)/time.Second) {
		return HealthPolicy{}, errors.New("sender success thresholds are outside safe bounds")
	}
	if input.FailureWindowSeconds < 60 || input.FailureWindowSeconds > 86400 || input.FailureMinimumSamples < 1 || input.FailureMinimumSamples > 10000 || input.FailureRateThresholdBPS < 0 || input.FailureRateThresholdBPS > 10000 {
		return HealthPolicy{}, errors.New("sender failure-health thresholds are outside safe bounds")
	}
	if input.DisconnectWindowSeconds < 60 || input.DisconnectWindowSeconds > 86400 || input.DisconnectThreshold < 0 || input.DisconnectThreshold > 1000 {
		return HealthPolicy{}, errors.New("sender disconnect thresholds are outside safe bounds")
	}
	if input.CapacityNearLimitPercent < 1 || input.CapacityNearLimitPercent > 99 {
		return HealthPolicy{}, errors.New("sender capacity near-limit threshold must be between 1 and 99 percent")
	}
	return HealthPolicy{
		HeartbeatStaleAfter: time.Duration(input.HeartbeatStaleSeconds) * time.Second, HeartbeatCriticalAfter: time.Duration(input.HeartbeatCriticalSeconds) * time.Second,
		SuccessStaleAfter: time.Duration(input.SuccessStaleSeconds) * time.Second, SuccessCriticalAfter: time.Duration(input.SuccessCriticalSeconds) * time.Second,
		FailureWindow: time.Duration(input.FailureWindowSeconds) * time.Second, FailureMinimumSamples: input.FailureMinimumSamples, FailureRateThresholdBPS: input.FailureRateThresholdBPS,
		DisconnectWindow: time.Duration(input.DisconnectWindowSeconds) * time.Second, DisconnectThreshold: input.DisconnectThreshold,
		CapacityNearLimitPercent: input.CapacityNearLimitPercent,
	}, nil
}
