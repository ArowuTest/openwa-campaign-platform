package dispatch

import (
	"context"
	"errors"
	"testing"
)

type gatewayFunc func(context.Context, GatewayRequest) (GatewayResult, error)

func (f gatewayFunc) Send(ctx context.Context, request GatewayRequest) (GatewayResult, error) {
	return f(ctx, request)
}

type preparedGatewayFunc gatewayFunc

func (f preparedGatewayFunc) Send(ctx context.Context, request GatewayRequest) (GatewayResult, error) {
	return gatewayFunc(f).Send(ctx, request)
}
func (f preparedGatewayFunc) Preflight(context.Context, GatewayRequest) error { return nil }
func (f preparedGatewayFunc) SendPrepared(ctx context.Context, request GatewayRequest) (GatewayResult, error) {
	return gatewayFunc(f).Send(ctx, request)
}

func TestTransportRouterSelectsExactlyOneConfiguredProvider(t *testing.T) {
	openCalls, metaCalls := 0, 0
	router := &TransportRouter{
		OpenWA: preparedGatewayFunc(func(context.Context, GatewayRequest) (GatewayResult, error) {
			openCalls++
			return GatewayResult{Accepted: true, ProviderMessageID: "open"}, nil
		}),
		Meta: preparedGatewayFunc(func(context.Context, GatewayRequest) (GatewayResult, error) {
			metaCalls++
			return GatewayResult{Accepted: true, ProviderMessageID: "meta"}, nil
		}),
	}
	if _, err := router.Send(context.Background(), GatewayRequest{Provider: "META"}); err != nil {
		t.Fatal(err)
	}
	if openCalls != 0 || metaCalls != 1 {
		t.Fatalf("open=%d meta=%d", openCalls, metaCalls)
	}
	_, err := router.Send(context.Background(), GatewayRequest{Provider: "OTHER"})
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailurePermanent {
		t.Fatalf("unsupported provider error=%v", err)
	}
}

func TestTransportRouterFailsClosedWhenSelectedSiblingLacksPreparedContract(t *testing.T) {
	calls := 0
	router := &TransportRouter{OpenWA: gatewayFunc(func(context.Context, GatewayRequest) (GatewayResult, error) {
		calls++
		return GatewayResult{Accepted: true}, nil
	})}
	request := GatewayRequest{Provider: "OPENWA"}
	if err := router.Preflight(context.Background(), request); err == nil {
		t.Fatal("router accepted OpenWA sibling without a preflight contract")
	}
	if _, err := router.SendPrepared(context.Background(), request); err == nil {
		t.Fatal("router fell back to unprepared OpenWA Send after SUBMITTING")
	}
	if calls != 0 {
		t.Fatalf("unprepared OpenWA gateway was called %d times", calls)
	}
}

type transportContractProbe struct {
	preflightCalls int
	preparedCalls  int
	sendCalls      int
}

func (g *transportContractProbe) Preflight(context.Context, GatewayRequest) error {
	g.preflightCalls++
	return nil
}
func (g *transportContractProbe) SendPrepared(context.Context, GatewayRequest) (GatewayResult, error) {
	g.preparedCalls++
	return GatewayResult{Accepted: true, ProviderMessageID: "prepared"}, nil
}
func (g *transportContractProbe) Send(context.Context, GatewayRequest) (GatewayResult, error) {
	g.sendCalls++
	return GatewayResult{Accepted: true, ProviderMessageID: "direct"}, nil
}

func TestTransportRouterSendUsesOnePreflightThenPreparedSend(t *testing.T) {
	probe := &transportContractProbe{}
	router := &TransportRouter{OpenWA: probe}
	result, err := router.Send(context.Background(), GatewayRequest{Provider: "OPENWA"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProviderMessageID != "prepared" || probe.preflightCalls != 1 || probe.preparedCalls != 1 || probe.sendCalls != 0 {
		t.Fatalf("router crossed prepared boundary incorrectly: result=%#v preflight=%d prepared=%d send=%d", result, probe.preflightCalls, probe.preparedCalls, probe.sendCalls)
	}
}
