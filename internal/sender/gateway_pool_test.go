package sender

import (
	"context"
	"errors"
	"testing"
)

type gatewayPoolMemory struct{ value GatewayPool }

func (m *gatewayPoolMemory) ListGatewayPools(context.Context) ([]GatewayPool, error) {
	return []GatewayPool{m.value}, nil
}
func (m *gatewayPoolMemory) CreateGatewayPool(_ context.Context, v GatewayPool, _, _ string) (GatewayPool, error) {
	v.ID = "gp-1"
	v.Version = 1
	m.value = v
	return v, nil
}
func (m *gatewayPoolMemory) UpdateGatewayPool(_ context.Context, _ string, _ int64, v GatewayPool, _, _ string) (GatewayPool, error) {
	m.value = v
	return v, nil
}
func (m *gatewayPoolMemory) GetGatewayPool(context.Context, string) (GatewayPool, error) {
	if m.value.ID == "" {
		return GatewayPool{}, ErrSenderNotFound
	}
	return m.value, nil
}

func TestGatewayPoolRequiresCompatibleCapabilities(t *testing.T) {
	store := &gatewayPoolMemory{}
	svc := GatewayPoolService{Store: store}
	pool, err := svc.Create(context.Background(), GatewayPool{Name: "OpenWA Baileys", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "0.13.0+platform.1", Capabilities: []Capability{CapabilitySendText, CapabilityDeliveryEvents}, MinimumHealthyNodes: 1}, "actor", "initial pool")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.RequireCapabilities(context.Background(), pool.ID, GatewayProviderOpenWA, GatewayEngineBaileys, []Capability{CapabilitySendText}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.RequireCapabilities(context.Background(), pool.ID, GatewayProviderOpenWA, GatewayEngineWhatsAppWebJS, []Capability{CapabilitySendText}); err == nil {
		t.Fatal("expected engine mismatch")
	}
	if _, err = svc.RequireCapabilities(context.Background(), pool.ID, GatewayProviderOpenWA, GatewayEngineBaileys, []Capability{CapabilityReadEvents}); err == nil {
		t.Fatal("expected missing capability")
	}
}

func TestGatewayPoolRejectsInvalidDefinitions(t *testing.T) {
	svc := GatewayPoolService{Store: &gatewayPoolMemory{}}
	_, err := svc.Create(context.Background(), GatewayPool{Name: "bad", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "1", Capabilities: []Capability{"SHELL_ACCESS"}, MinimumHealthyNodes: 1}, "actor", "reason")
	if err == nil || errors.Is(err, ErrSenderNotFound) {
		t.Fatal("expected validation error")
	}
}
