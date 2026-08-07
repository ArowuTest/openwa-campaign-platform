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

const TransportRuntimeConfigurationKey = "SENDER.TRANSPORT_RECOVERY"

type ReconnectMode string

const (
	ReconnectUnbounded ReconnectMode = "UNBOUNDED"
	ReconnectDisabled  ReconnectMode = "DISABLED"
	ReconnectBounded   ReconnectMode = "BOUNDED"
)

type SessionTransportRuntimeConfiguration struct {
	ReconnectMode             ReconnectMode `json:"reconnectMode"`
	ReconnectMaxAttempts      int           `json:"reconnectMaxAttempts"`
	ReconnectBaseDelayMs      int64         `json:"reconnectBaseDelayMs"`
	ReconnectStabilityResetMs int64         `json:"reconnectStabilityResetMs"`
	WatchdogProbeTimeoutMs    int64         `json:"watchdogProbeTimeoutMs"`
	WatchdogFailureThreshold  int           `json:"watchdogFailureThreshold"`
	EngineTeardownTimeoutMs   int64         `json:"engineTeardownTimeoutMs"`
	Source                    string        `json:"source"`
	ConfigurationID           string        `json:"configurationId,omitempty"`
	ScopeType                 string        `json:"scopeType,omitempty"`
	ScopeID                   string        `json:"scopeId,omitempty"`
	Version                   int64         `json:"version,omitempty"`
}

type transportRuntimePolicyValue struct {
	ReconnectMode             ReconnectMode `json:"reconnectMode"`
	ReconnectMaxAttempts      int           `json:"reconnectMaxAttempts"`
	ReconnectBaseDelayMs      int64         `json:"reconnectBaseDelayMs"`
	ReconnectStabilityResetMs int64         `json:"reconnectStabilityResetMs"`
	WatchdogProbeTimeoutMs    int64         `json:"watchdogProbeTimeoutMs"`
	WatchdogFailureThreshold  int           `json:"watchdogFailureThreshold"`
	EngineTeardownTimeoutMs   int64         `json:"engineTeardownTimeoutMs"`
}

type PlatformTransportRuntimeResolver struct {
	Configurations *platformpolicy.ConfigurationAdministration
}

func (r *PlatformTransportRuntimeResolver) ResolveTransportRuntime(ctx context.Context, session GovernedSession, at time.Time) (*SessionTransportRuntimeConfiguration, error) {
	if r == nil || r.Configurations == nil {
		return nil, errors.New("transport runtime configuration resolver is required")
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
	configuration, err := r.Configurations.Resolve(ctx, TransportRuntimeConfigurationKey, scopes, at.UTC())
	if errors.Is(err, platformpolicy.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := r.Configurations.ValidateResolvedChecksum(configuration); err != nil {
		return nil, err
	}
	policy, err := decodeTransportRuntime(configuration.Value)
	if err != nil {
		return nil, fmt.Errorf("decode active sender transport configuration %s: %w", configuration.ID, err)
	}
	policy.Source = "GOVERNED_CONFIGURATION"
	policy.ConfigurationID = configuration.ID
	policy.ScopeType = string(configuration.ScopeType)
	policy.ScopeID = configuration.ScopeID
	policy.Version = configuration.Version
	return &policy, nil
}
func decodeTransportRuntime(raw []byte) (SessionTransportRuntimeConfiguration, error) {
	var value transportRuntimePolicyValue
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return SessionTransportRuntimeConfiguration{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return SessionTransportRuntimeConfiguration{}, errors.New("sender transport runtime policy must contain one JSON object")
	}
	value.ReconnectMode = ReconnectMode(strings.ToUpper(strings.TrimSpace(string(value.ReconnectMode))))
	switch value.ReconnectMode {
	case ReconnectUnbounded, ReconnectDisabled:
		if value.ReconnectMaxAttempts != 0 {
			return SessionTransportRuntimeConfiguration{}, errors.New("unbounded or disabled reconnect mode requires reconnectMaxAttempts=0")
		}
	case ReconnectBounded:
		if value.ReconnectMaxAttempts < 1 || value.ReconnectMaxAttempts > 20 {
			return SessionTransportRuntimeConfiguration{}, errors.New("bounded reconnect attempts must be between 1 and 20")
		}
	default:
		return SessionTransportRuntimeConfiguration{}, errors.New("reconnectMode must be UNBOUNDED, DISABLED or BOUNDED")
	}
	if value.ReconnectBaseDelayMs < 1000 || value.ReconnectBaseDelayMs > 300000 {
		return SessionTransportRuntimeConfiguration{}, errors.New("reconnectBaseDelayMs is outside safe bounds")
	}
	if value.ReconnectStabilityResetMs < 60000 || value.ReconnectStabilityResetMs > 3600000 {
		return SessionTransportRuntimeConfiguration{}, errors.New("reconnectStabilityResetMs is outside safe bounds")
	}
	if value.WatchdogProbeTimeoutMs < 100 || value.WatchdogProbeTimeoutMs > 60000 {
		return SessionTransportRuntimeConfiguration{}, errors.New("watchdogProbeTimeoutMs is outside safe bounds")
	}
	if value.WatchdogFailureThreshold < 1 || value.WatchdogFailureThreshold > 10 {
		return SessionTransportRuntimeConfiguration{}, errors.New("watchdogFailureThreshold is outside safe bounds")
	}
	if value.EngineTeardownTimeoutMs < 1000 || value.EngineTeardownTimeoutMs > 120000 {
		return SessionTransportRuntimeConfiguration{}, errors.New("engineTeardownTimeoutMs is outside safe bounds")
	}
	return SessionTransportRuntimeConfiguration{
		ReconnectMode:             value.ReconnectMode,
		ReconnectMaxAttempts:      value.ReconnectMaxAttempts,
		ReconnectBaseDelayMs:      value.ReconnectBaseDelayMs,
		ReconnectStabilityResetMs: value.ReconnectStabilityResetMs,
		WatchdogProbeTimeoutMs:    value.WatchdogProbeTimeoutMs,
		WatchdogFailureThreshold:  value.WatchdogFailureThreshold,
		EngineTeardownTimeoutMs:   value.EngineTeardownTimeoutMs,
	}, nil
}
