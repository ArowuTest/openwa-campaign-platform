package sender

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestGatewayPoolRetirementBlockedWhileInUse(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	store := NewMemoryGovernanceStore()
	admin := &GatewayPoolAdministration{Store: store, Clock: func() time.Time { return now }}
	pool, err := admin.Create(ctx, GatewayPool{Name: "OpenWA web pool", Provider: GatewayProviderOpenWA, Engine: GatewayEngineWhatsAppWebJS, AdapterVersion: "0.13.0+platform.1", Capabilities: []Capability{CapabilitySendText, CapabilityDeliveryEvents}, MinimumHealthyNodes: 1}, "maker", "create governed gateway pool")
	if err != nil {
		t.Fatal(err)
	}
	pool, err = admin.Submit(ctx, pool.ID, pool.Version, "submitter", "submit gateway pool")
	if err != nil {
		t.Fatal(err)
	}
	pool, err = admin.Decide(ctx, pool.ID, pool.Version, true, "approver", "approve gateway pool", now)
	if err != nil {
		t.Fatal(err)
	}
	governance := &GovernanceService{Store: store}
	node, err := governance.RegisterNode(ctx, Node{Name: "gateway-1", Status: "READY", Capacity: 2, GatewayPoolID: pool.ID}, "technical-admin", "register approved gateway node")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Retire(ctx, pool.ID, pool.Version, "technical-admin", "retire active pool"); err == nil {
		t.Fatal("retirement succeeded while gateway node remained active")
	}
	if _, err = governance.TransitionNode(ctx, node.ID, node.Version, "RETIRED", "technical-admin", "retire gateway node first"); err != nil {
		t.Fatal(err)
	}
	pool, err = admin.Retire(ctx, pool.ID, pool.Version, "technical-admin", "retire empty gateway pool")
	if err != nil {
		t.Fatal(err)
	}
	if pool.Status != GatewayPoolRetired || pool.EffectiveTo == nil {
		t.Fatalf("pool not retired with evidence: %#v", pool)
	}
}
