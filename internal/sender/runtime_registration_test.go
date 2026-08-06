package sender

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestRuntimeRegistrationVerifiesIdentityAndRejectsReplay(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	poolService := &GatewayPoolService{Store: repo}
	pool, err := poolService.Create(context.Background(), GatewayPool{Name: "wwjs", Provider: GatewayProviderOpenWA, Engine: GatewayEngineWhatsAppWebJS, AdapterVersion: "adapter-1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText, CapabilityDeliveryEvents}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	node, err := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "node-a", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AdapterVersion: "adapter-1"}, "maker", "register")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AdapterVersion: "adapter-1", GatewayVersion: "0.13.0", WorkerVersion: "0.8.27", ConfigurationVersion: "cfg-2", BootID: "boot-a", InternalURL: "https://gateway.internal", Capabilities: []Capability{CapabilitySendText, CapabilityDeliveryEvents}, RuntimeState: RuntimeReady, Capacity: 4, SessionCount: 2, QueueDepth: 3, CPUPercent: 12.5, MemoryBytes: 1024, ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, signature, err := SignRuntimeReport(secret, now, "nonce-0123456789012345", raw)
	if err != nil {
		t.Fatal(err)
	}
	service := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }}
	stored, err := service.Register(context.Background(), node.ID, ts, nonce, signature, raw)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "READY" || stored.GatewayVersion != "0.13.0" || stored.ConfigurationVersion != "cfg-2" || stored.RegisteredAt == nil {
		t.Fatalf("runtime evidence was not persisted: %#v", stored)
	}
	if _, err = service.Register(context.Background(), node.ID, ts, nonce, signature, raw); !errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("expected replay rejection, got %v", err)
	}
}

func TestRuntimeRegistrationRejectsCapabilityDrift(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "baileys", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText, CapabilityInboundMessages}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "b", InternalURL: "https://node", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-abcdefghijklmnop", raw)
	service := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }}
	if _, err := service.Register(context.Background(), node.ID, ts, nonce, sig, raw); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("expected drift rejection, got %v", err)
	}
	events, err := service.Events(context.Background(), node.ID, 10)
	if err != nil || len(events) != 1 || events[0].EventType != "REJECTED" {
		t.Fatalf("expected rejection evidence, got %#v %v", events, err)
	}
}

func TestDecodeRuntimeReportRejectsMalformedTrailingData(t *testing.T) {
	raw := []byte(`{"nodeId":"node"}` + "{")
	if _, err := DecodeRuntimeReport(raw); err == nil {
		t.Fatal("expected malformed trailing data to be rejected")
	}
}
