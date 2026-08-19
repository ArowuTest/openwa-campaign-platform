package dispatch

import (
	"context"
	"errors"
	"strings"
)

type TransportRouter struct {
	OpenWA Gateway
	Meta   Gateway
}

func (r *TransportRouter) Preflight(ctx context.Context, request GatewayRequest) error {
	provider := strings.ToUpper(strings.TrimSpace(request.Provider))
	var target Gateway
	switch provider {
	case "OPENWA":
		if r == nil || r.OpenWA == nil {
			return GatewayError{Code: "OPENWA_TRANSPORT_UNAVAILABLE", Safety: FailureSafeToRetry, Err: errors.New("OpenWA transport is not configured")}
		}
		target = r.OpenWA
	case "META":
		if r == nil || r.Meta == nil {
			return GatewayError{Code: "META_TRANSPORT_UNAVAILABLE", Safety: FailureSafeToRetry, Err: errors.New("Meta transport is not configured")}
		}
		target = r.Meta
	default:
		return GatewayError{Code: "TRANSPORT_PROVIDER_UNSUPPORTED", Safety: FailurePermanent, Err: errors.New("unsupported transport provider")}
	}
	preflight, ok := target.(GatewayPreflight)
	if !ok {
		return GatewayError{Code: "TRANSPORT_PREFLIGHT_UNSUPPORTED", Safety: FailurePermanent, Err: errors.New("selected transport does not implement preflight")}
	}
	return preflight.Preflight(ctx, request)
}

func (r *TransportRouter) SendPrepared(ctx context.Context, request GatewayRequest) (GatewayResult, error) {
	provider := strings.ToUpper(strings.TrimSpace(request.Provider))
	var target Gateway
	switch provider {
	case "OPENWA":
		if r == nil || r.OpenWA == nil {
			return GatewayResult{}, GatewayError{Code: "OPENWA_TRANSPORT_UNAVAILABLE", Safety: FailureSafeToRetry, Err: errors.New("OpenWA transport is not configured")}
		}
		target = r.OpenWA
	case "META":
		if r == nil || r.Meta == nil {
			return GatewayResult{}, GatewayError{Code: "META_TRANSPORT_UNAVAILABLE", Safety: FailureSafeToRetry, Err: errors.New("Meta transport is not configured")}
		}
		target = r.Meta
	default:
		return GatewayResult{}, GatewayError{Code: "TRANSPORT_PROVIDER_UNSUPPORTED", Safety: FailurePermanent, Err: errors.New("unsupported transport provider")}
	}
	prepared, ok := target.(GatewayPreparedSender)
	if !ok {
		return GatewayResult{}, GatewayError{Code: "TRANSPORT_PREPARED_UNSUPPORTED", Safety: FailurePermanent, Err: errors.New("selected transport does not implement prepared send")}
	}
	return prepared.SendPrepared(ctx, request)
}

func (r *TransportRouter) Send(ctx context.Context, request GatewayRequest) (GatewayResult, error) {
	if err := r.Preflight(ctx, request); err != nil {
		return GatewayResult{}, err
	}
	return r.SendPrepared(ctx, request)
}
