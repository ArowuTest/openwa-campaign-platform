package operations

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

const GatewayRuntimeHealthConfigurationKey = "GATEWAY.RUNTIME_HEALTH"

type GatewayRuntimeHealthPolicy struct {
	StaleAfter      time.Duration
	Source          string
	ConfigurationID string
	ScopeType       string
	ScopeID         string
	Version         int64
}

type gatewayRuntimeHealthPolicyValue struct {
	StaleAfterSeconds int `json:"staleAfterSeconds"`
}
type GatewayRuntimeHealthPolicyResolver interface {
	ResolveGatewayRuntimeHealth(context.Context, time.Time) (GatewayRuntimeHealthPolicy, error)
}

type PlatformGatewayRuntimeHealthResolver struct {
	Configurations     *platformpolicy.ConfigurationAdministration
	FallbackStaleAfter time.Duration
}

func (r *PlatformGatewayRuntimeHealthResolver) ResolveGatewayRuntimeHealth(ctx context.Context, at time.Time) (GatewayRuntimeHealthPolicy, error) {
	fallback := r.FallbackStaleAfter
	if fallback <= 0 {
		fallback = 2 * time.Minute
	}
	base := GatewayRuntimeHealthPolicy{StaleAfter: fallback, Source: "DEPLOYMENT_BOOTSTRAP"}
	if r == nil || r.Configurations == nil {
		return base, nil
	}
	configuration, err := r.Configurations.Resolve(ctx, GatewayRuntimeHealthConfigurationKey, nil, at.UTC())
	if errors.Is(err, platformpolicy.ErrNotFound) {
		return base, nil
	}
	if err != nil {
		return GatewayRuntimeHealthPolicy{}, err
	}
	if err := r.Configurations.ValidateResolvedChecksum(configuration); err != nil {
		return GatewayRuntimeHealthPolicy{}, err
	}
	policy, err := decodeGatewayRuntimeHealthPolicy(configuration.Value)
	if err != nil {
		return GatewayRuntimeHealthPolicy{}, fmt.Errorf("decode active gateway runtime health configuration %s: %w", configuration.ID, err)
	}
	policy.Source = "GOVERNED_CONFIGURATION"
	policy.ConfigurationID = configuration.ID
	policy.ScopeType = string(configuration.ScopeType)
	policy.ScopeID = configuration.ScopeID
	policy.Version = configuration.Version
	return policy, nil
}

func decodeGatewayRuntimeHealthPolicy(raw []byte) (GatewayRuntimeHealthPolicy, error) {
	var value gatewayRuntimeHealthPolicyValue
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return GatewayRuntimeHealthPolicy{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return GatewayRuntimeHealthPolicy{}, errors.New("gateway runtime health policy must contain one JSON object")
	}
	if value.StaleAfterSeconds < 30 || value.StaleAfterSeconds > 3600 {
		return GatewayRuntimeHealthPolicy{}, errors.New("staleAfterSeconds must be between 30 and 3600")
	}
	return GatewayRuntimeHealthPolicy{StaleAfter: time.Duration(value.StaleAfterSeconds) * time.Second}, nil
}
