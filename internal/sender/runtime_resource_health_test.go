package sender

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func completeRuntimeResourceHealth() RuntimeResourceHealth {
	diskTotal, diskFree, diskAvailable := int64(1_000_000), int64(400_000), int64(350_000)
	inodesTotal, inodesFree := int64(10_000), int64(7_000)
	networkRX, networkTX := int64(12_345), int64(67_890)
	openFiles := int64(14)
	return RuntimeResourceHealth{
		Scope: "CONTAINER", FilesystemPath: "/data",
		DiskTotalBytes: &diskTotal, DiskFreeBytes: &diskFree, DiskAvailableBytes: &diskAvailable,
		InodesTotal: &inodesTotal, InodesFree: &inodesFree,
		ProcessID: 42, ProcessUptimeSeconds: 120, OpenFileDescriptorCount: &openFiles,
		NetworkRXBytes: &networkRX, NetworkTXBytes: &networkTX, NetworkInterfaceCount: 1,
	}
}

func TestRuntimeRegistrationPersistsContainerResourceHealth(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, err := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{
		Name: "health", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys,
		AdapterVersion: "adapter-1", Status: GatewayPoolActive,
		Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1,
	}, "maker", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	node, err := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{
		Name: "node-health", Status: "OFFLINE", GatewayPoolID: pool.ID,
		Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "adapter-1",
	}, "maker", "register")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	report := RuntimeReport{
		NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID,
		Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "adapter-1",
		GatewayVersion: "0.13.0", WorkerVersion: "node-22", ConfigurationVersion: "cfg-1",
		BootID: "boot-health", InternalURL: "https://gateway.internal",
		Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady,
		Capacity: 3, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now,
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, signature, err := SignRuntimeReport(secret, now, "nonce-resource-health-1234", raw)
	if err != nil {
		t.Fatal(err)
	}
	service := &RuntimeRegistrationService{
		Store: repo, GatewayPools: repo, Secret: secret,
		MaximumSkew: time.Minute, Clock: func() time.Time { return now },
	}
	stored, err := service.Register(context.Background(), node.ID, ts, nonce, signature, raw)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ResourceHealth.Scope != "CONTAINER" || stored.ResourceHealth.FilesystemPath != "/data" ||
		stored.ResourceHealth.DiskFreeBytes == nil || *stored.ResourceHealth.DiskFreeBytes != 400_000 ||
		stored.ResourceHealth.NetworkTXBytes == nil || *stored.ResourceHealth.NetworkTXBytes != 67_890 {
		t.Fatalf("resource health evidence was not persisted: %#v", stored.ResourceHealth)
	}
}

func TestRuntimeReportRejectsHostScopedResourceClaims(t *testing.T) {
	health := completeRuntimeResourceHealth()
	health.Scope = "HOST"
	report := RuntimeReport{
		NodeID: "node-1", ExpectedNodeVersion: 1, GatewayPoolID: "pool-1",
		Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "adapter-1",
		GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c",
		BootID: "boot", InternalURL: "https://gateway.internal",
		Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady,
		ResourceHealth: health,
	}
	pool := GatewayPool{
		ID: "pool-1", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys,
		AdapterVersion: "adapter-1", Status: GatewayPoolActive,
		Capabilities: []Capability{CapabilitySendText},
	}
	if err := validateRuntimeReport(&report, "node-1", pool, time.Now().UTC()); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("host-scoped runtime claim was accepted: %v", err)
	}
}
