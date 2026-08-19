package sender

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	now := time.Now().UTC().Add(time.Second)
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AdapterVersion: "adapter-1", GatewayVersion: "0.13.0", WorkerVersion: "0.8.27", ConfigurationVersion: "cfg-2", BootID: "boot-a", InternalURL: "https://gateway.internal", Capabilities: []Capability{CapabilitySendText, CapabilityDeliveryEvents}, RuntimeState: RuntimeReady, Capacity: 4, SessionCount: 2, QueueDepth: 3, CPUPercent: 12.5, MemoryBytes: 1024, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
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

func TestRuntimeRegistrationAcceptsPreviousSecretDuringRotation(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, err := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "rotation", Provider: GatewayProviderOpenWA, Engine: GatewayEngineWhatsAppWebJS, AdapterVersion: "adapter-1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	node, err := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "rotation-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AdapterVersion: "adapter-1"}, "maker", "register")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AdapterVersion: "adapter-1", GatewayVersion: "0.13.0", WorkerVersion: "0.8.28", ConfigurationVersion: "cfg-rotate", BootID: "boot-rotate", InternalURL: "https://gateway.internal", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	active := []byte("active-runtime-secret-012345678901")
	previous := []byte("previous-runtime-secret-0123456789")
	ts, nonce, signature, err := SignRuntimeReport(previous, now, "nonce-rotation-123456789", raw)
	if err != nil {
		t.Fatal(err)
	}
	service := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: active, PreviousSecrets: [][]byte{previous}, MaximumSkew: time.Minute, Clock: func() time.Time { return now }}
	if _, err := service.Register(context.Background(), node.ID, ts, nonce, signature, raw); err != nil {
		t.Fatalf("previous runtime secret rejected: %v", err)
	}
}

func TestRuntimeRegistrationRejectsCapabilityDrift(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "baileys", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText, CapabilityInboundMessages}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "b", InternalURL: "https://node", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
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

func TestRuntimeRegistrationAcceptsRepeatedHeartbeatsAtGovernedNodeVersion(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, err := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{
		Name: "repeated-heartbeat", Provider: GatewayProviderOpenWA, Engine: GatewayEngineWhatsAppWebJS,
		AdapterVersion: "adapter-1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1,
	}, "maker", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	node, err := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{
		Name: "repeated-heartbeat-node", Status: "OFFLINE", GatewayPoolID: pool.ID,
		Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AdapterVersion: "adapter-1",
	}, "maker", "register")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("01234567890123456789012345678901")
	now := time.Now().UTC()
	service := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }}
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AdapterVersion: "adapter-1", GatewayVersion: "v1", WorkerVersion: "w1", ConfigurationVersion: "cfg1", BootID: "boot-1", InternalURL: "https://gateway.internal", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 2, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	for i := 0; i < 2; i++ {
		report.ObservedAt = now.Add(time.Duration(i) * 10 * time.Second)
		raw, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		ts, nonce, signature, err := SignRuntimeReport(secret, report.ObservedAt, fmt.Sprintf("nonce-repeat-heartbeat-%d", i), raw)
		if err != nil {
			t.Fatal(err)
		}
		service.Clock = func() time.Time { return report.ObservedAt }
		stored, err := service.Register(context.Background(), node.ID, ts, nonce, signature, raw)
		if err != nil {
			t.Fatalf("heartbeat %d rejected: %v", i+1, err)
		}
		if stored.Version != node.Version {
			t.Fatalf("runtime heartbeat changed governed node version: got %d want %d", stored.Version, node.Version)
		}
	}
	events, err := service.Events(context.Background(), node.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].EventType != "HEARTBEAT" || events[1].EventType != "REGISTERED" {
		t.Fatalf("unexpected repeated heartbeat evidence: %#v", events)
	}
}

func TestValidateRuntimeReportRejectsNonProductionInternalURLs(t *testing.T) {
	now := time.Now().UTC()
	pool := GatewayPool{ID: "pool-1", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "adapter-1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}}
	for _, rawURL := range []string{
		"https://host.docker.internal:2785",
		"https://bridge.docker.internal:2785",
		"https://127.0.0.1:2785",
		"https://[::1]:2785",
		"https://169.254.10.20:2785",
	} {
		report := RuntimeReport{NodeID: "node-1", ExpectedNodeVersion: 1, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "adapter-1", GatewayVersion: "v1", WorkerVersion: "w1", ConfigurationVersion: "cfg1", BootID: "boot-1", InternalURL: rawURL, Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
		if err := validateRuntimeReport(&report, "node-1", pool, now); !errors.Is(err, ErrRuntimeDrift) {
			t.Fatalf("runtime registration accepted non-production internal URL %q: %v", rawURL, err)
		}
	}
}
