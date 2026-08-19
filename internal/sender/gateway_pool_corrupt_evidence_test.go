package sender

import (
	"context"
	"testing"
)

type corruptGatewayPoolStore struct{ *MemoryGovernanceStore }

func (s corruptGatewayPoolStore) GetGatewayPool(context.Context, string) (GatewayPool, error) {
	return GatewayPool{
		Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys,
		AdapterVersion: "0.13.0", Status: GatewayPoolActive,
		Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1,
	}, nil
}

func TestRequireCapabilitiesRejectsCorruptActiveGatewayEvidence(t *testing.T) {
	svc := &GatewayPoolService{Store: corruptGatewayPoolStore{MemoryGovernanceStore: NewMemoryGovernanceStore()}}
	if _, err := svc.RequireCapabilities(context.Background(), "gw-corrupt", GatewayProviderOpenWA, GatewayEngineBaileys, []Capability{CapabilitySendText}); err == nil {
		t.Fatal("active gateway evidence with empty id/version was accepted")
	}
}
