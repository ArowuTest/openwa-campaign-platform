package sender

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AdapterVersion: "adapter-1", GatewayVersion: "0.13.0", WorkerVersion: "0.8.27", ConfigurationVersion: "cfg-2", BootID: "boot-a", RuntimeSequence: 1, InternalURL: "https://gateway.internal", Capabilities: []Capability{CapabilitySendText, CapabilityDeliveryEvents}, RuntimeState: RuntimeReady, Capacity: 4, SessionCount: 2, QueueDepth: 3, CPUPercent: 12.5, MemoryBytes: 1024, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
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
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AdapterVersion: "adapter-1", GatewayVersion: "0.13.0", WorkerVersion: "0.8.28", ConfigurationVersion: "cfg-rotate", BootID: "boot-rotate", RuntimeSequence: 1, InternalURL: "https://gateway.internal", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
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
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "b", RuntimeSequence: 1, InternalURL: "https://node", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
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
	if _, err := service.Register(context.Background(), node.ID, ts, nonce, sig, raw); !errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("replayed rejected runtime report was not fenced by nonce: %v", err)
	}
	events, err = service.Events(context.Background(), node.ID, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("replayed rejected runtime report appended duplicate evidence: %#v %v", events, err)
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
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS", AdapterVersion: "adapter-1", GatewayVersion: "v1", WorkerVersion: "w1", ConfigurationVersion: "cfg1", BootID: "boot-1", RuntimeSequence: 1, InternalURL: "https://gateway.internal", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 2, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	for i := 0; i < 2; i++ {
		report.RuntimeSequence = int64(i + 1)
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
		"https://8.8.8.8:2785",
		"https://10.20.30.40:2785",
		"https://172.16.0.40:2785",
		"https://192.168.1.40:2785",
		"https://[fd00::1]:2785",
		"https://[::ffff:10.20.30.40]:2785",
		"https://gateway.attacker.example:2785",
		"http://10.20.30.40:2785",
		"https://deploy:secret@gateway.private.example:2785",
		"https://gateway.private.example:2785/runtime",
		"https://gateway.private.example:2785?token=secret",
		"https://gateway.private.example:2785#fragment",
		"https://gateway.private.example:0",
		"https://gateway.private.example:65536",
	} {
		report := RuntimeReport{NodeID: "node-1", ExpectedNodeVersion: 1, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "adapter-1", GatewayVersion: "v1", WorkerVersion: "w1", ConfigurationVersion: "cfg1", BootID: "boot-1", RuntimeSequence: 1, InternalURL: rawURL, Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
		if err := validateRuntimeReport(&report, "node-1", pool, now, []string{"gateway.private.example"}, nil); !errors.Is(err, ErrRuntimeDrift) {
			t.Fatalf("runtime registration accepted non-production internal URL %q: %v", rawURL, err)
		}
	}
}

func TestCanonicalRuntimeInternalURLRejectsPrivateIPAuthorities(t *testing.T) {
	for _, raw := range []string{
		"https://10.20.30.40",
		"https://172.16.0.40:443/",
		"https://192.168.1.40",
		"https://[fd00::1]",
		"https://[::ffff:10.20.30.40]",
		"https://010.020.030.050",
		"https://0x0a.0x14.0x1e.0x28",
		"https://167772161",
		"https://10.1",
		"https://[fe80::1%25eth0]",
	} {
		if canonical, valid := canonicalRuntimeInternalURL(raw, []string{"10.20.30.40", "172.16.0.40", "192.168.1.40", "fd00::1", "::ffff:10.20.30.40", "010.020.030.050", "0x0a.0x14.0x1e.0x28", "167772161", "10.1", "fe80::1%eth0"}, nil); valid || canonical != "" {
			t.Fatalf("private runtime authority %q canonicalized to %q valid=%v", raw, canonical, valid)
		}
	}
}

func TestValidateRuntimeReportRejectsControlAuthorityAlias(t *testing.T) {
	now := time.Now().UTC()
	pool := GatewayPool{ID: "pool-1", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "adapter-1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}}
	report := RuntimeReport{NodeID: "node-1", ExpectedNodeVersion: 1, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "adapter-1", GatewayVersion: "v1", WorkerVersion: "w1", ConfigurationVersion: "cfg1", BootID: "boot-1", RuntimeSequence: 1, InternalURL: "https://control.internal.example:2785", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	if err := validateRuntimeReport(&report, "node-1", pool, now, []string{"control.internal.example"}, []string{"control.internal.example"}); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("runtime registration accepted the control hostname as gateway authority: %v", err)
	}
}

func TestRuntimeRegistrationConsumesNonceBeforeUnavailablePoolRejection(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, err := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "nonce-pool", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	node, err := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "nonce-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: "00000000-0000-4000-8000-000000000099", Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "b", RuntimeSequence: 1, InternalURL: "https://gateway.internal", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-pool-miss-123456789", raw)
	service := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }}
	if _, err := service.Register(context.Background(), node.ID, ts, nonce, sig, raw); err == nil || errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("expected first pool-miss rejection, got %v", err)
	}
	events, _ := service.Events(context.Background(), node.ID, 10)
	if len(events) != 1 {
		t.Fatalf("expected one pool-miss rejection event, got %#v", events)
	}
	if _, err := service.Register(context.Background(), node.ID, ts, nonce, sig, raw); !errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("replayed pool-miss report was not fenced: %v", err)
	}
	events, _ = service.Events(context.Background(), node.ID, 10)
	if len(events) != 1 {
		t.Fatalf("replayed pool-miss report appended duplicate evidence: %#v", events)
	}
}

func TestRuntimeRegistrationPoolMissSeparatesDeclaredFromCanonicalInternalURL(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, err := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "pool-miss-url", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	node, err := (&GovernanceService{Store: repo, RequireCanonicalRuntimeURL: true, RuntimeAllowedInternalHosts: []string{"gateway.private.example"}}).RegisterNode(context.Background(), Node{Name: "pool-miss-url-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", InternalURL: "https://gateway.private.example:2785"}, "maker", "register")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	declaredURL := "https://deploy:secret@gateway.private.example:2785/runtime"
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: "00000000-0000-4000-8000-000000000099", Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "pool-miss-url-boot", RuntimeSequence: 1, InternalURL: declaredURL, Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-pool-miss-url-123456", raw)
	service := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }, AllowedInternalHosts: []string{"gateway.private.example"}, RequireCanonicalRuntimeURL: true}
	if _, err := service.Register(context.Background(), node.ID, ts, nonce, sig, raw); err == nil {
		t.Fatal("expected pool-miss rejection")
	}
	events, _ := service.Events(context.Background(), node.ID, 10)
	if len(events) != 1 {
		t.Fatalf("expected one pool-miss rejection event, got %#v", events)
	}
	if events[0].RuntimeIdentity["declaredInternalUrl"] != declaredURL || events[0].RuntimeIdentity["internalUrl"] != "" {
		t.Fatalf("pool-miss rejection conflated declared and canonical URLs: %#v", events[0].RuntimeIdentity)
	}
}

func TestRuntimeRegistrationPathMismatchDoesNotConsumeNonce(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "path-bind", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	gov := &GovernanceService{Store: repo}
	nodeA, _ := gov.RegisterNode(context.Background(), Node{Name: "node-a", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	nodeB, _ := gov.RegisterNode(context.Background(), Node{Name: "node-b", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: nodeA.ID, ExpectedNodeVersion: nodeA.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "b", RuntimeSequence: 1, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-path-bind-123456789", raw)
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }, AllowedInternalHosts: []string{"gateway.private.example"}}
	if _, err := svc.Register(context.Background(), nodeB.ID, ts, nonce, sig, raw); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("retargeted report not rejected: %v", err)
	}
	if len(repo.runtimeNonces) != 0 {
		t.Fatalf("path mismatch consumed nonce state: %#v", repo.runtimeNonces)
	}
	if _, err := svc.Register(context.Background(), nodeA.ID, ts, nonce, sig, raw); err != nil {
		t.Fatalf("legitimate delivery rejected after retarget attempt: %v", err)
	}
}

type transientGatewayPoolStore struct {
	GatewayPoolStore
	err error
}

func (s *transientGatewayPoolStore) GetGatewayPool(ctx context.Context, id string) (GatewayPool, error) {
	if s.err != nil {
		return GatewayPool{}, s.err
	}
	return s.GatewayPoolStore.GetGatewayPool(ctx, id)
}

func TestRuntimeRegistrationTransientPoolErrorDoesNotCreateRejectionOrBurnNonce(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "transient-pool", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "transient-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "b", RuntimeSequence: 1, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-transient-123456789", raw)
	transient := errors.New("database temporarily unavailable")
	pools := &transientGatewayPoolStore{GatewayPoolStore: repo, err: transient}
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: pools, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }, AllowedInternalHosts: []string{"gateway.private.example"}}
	if _, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw); !errors.Is(err, transient) {
		t.Fatalf("expected transient pool error, got %v", err)
	}
	events, _ := svc.Events(context.Background(), node.ID, 10)
	if len(events) != 0 {
		t.Fatalf("transient pool error created governance rejection evidence: %#v", events)
	}
	pools.err = nil
	if _, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw); err != nil {
		t.Fatalf("transient pool error burned legitimate runtime nonce: %v", err)
	}
}

func TestRuntimeRegistrationRejectsGatewayPoolAuthorityDrift(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	poolService := &GatewayPoolService{Store: repo}
	poolA, _ := poolService.Create(context.Background(), GatewayPool{Name: "authority-a", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	poolB, _ := poolService.Create(context.Background(), GatewayPool{Name: "authority-b", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "authority-node", Status: "OFFLINE", GatewayPoolID: poolA.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: poolB.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "b", RuntimeSequence: 1, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-pool-authority-123456", raw)
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }, AllowedInternalHosts: []string{"gateway.private.example"}}
	if _, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("runtime registration changed governed gateway-pool authority: %v", err)
	}
	stored, _ := repo.GetNode(context.Background(), node.ID)
	if stored.GatewayPoolID != poolA.ID {
		t.Fatalf("runtime registration moved governed node from %s to %s", poolA.ID, stored.GatewayPoolID)
	}
	events, _ := svc.Events(context.Background(), node.ID, 10)
	if len(events) != 1 || events[0].EventType != "REJECTED" {
		t.Fatalf("pool-authority drift lacks immutable rejection evidence: %#v", events)
	}
}

func TestRuntimeRegistrationRejectsGovernedInternalURLDrift(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "url-authority", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	governedURL := "https://gateway-a.private.example:2785"
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "url-authority-node", Status: "OFFLINE", InternalURL: governedURL, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "b", RuntimeSequence: 1, InternalURL: "https://gateway-b.private.example:2785", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-url-authority-1234567", raw)
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }, AllowedInternalHosts: []string{"gateway-a.private.example", "gateway-b.private.example"}}
	if _, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("runtime registration changed governed internal URL authority: %v", err)
	}
	stored, _ := repo.GetNode(context.Background(), node.ID)
	if stored.InternalURL != governedURL {
		t.Fatalf("runtime registration moved governed URL from %s to %s", governedURL, stored.InternalURL)
	}
	events, _ := svc.Events(context.Background(), node.ID, 10)
	if len(events) != 1 || events[0].EventType != "REJECTED" {
		t.Fatalf("internal-URL authority drift lacks immutable rejection evidence: %#v", events)
	}
}

func TestRuntimeRegistrationPreservesDeclaredInternalURLInRejectionEvidence(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "declared-url-audit", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "declared-url-audit-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	now := time.Now().UTC()
	declaredURL := "https://deploy:secret@gateway.private.example:2785/runtime"
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "declared-url-boot", RuntimeSequence: 1, InternalURL: declaredURL, Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-declared-url-audit-123", raw)
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }, AllowedInternalHosts: []string{"gateway.private.example"}}
	if _, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("invalid declared URL was not rejected: %v", err)
	}
	events, _ := svc.Events(context.Background(), node.ID, 10)
	if len(events) != 1 || events[0].RuntimeIdentity["declaredInternalUrl"] != declaredURL || events[0].RuntimeIdentity["internalUrl"] != "" {
		t.Fatalf("rejection evidence did not preserve declared URL separately from canonical URL: %#v", events)
	}
}

func TestGovernanceRegisterNodeRejectsUnusableRuntimeAuthorities(t *testing.T) {
	for _, raw := range []string{
		"http://10.20.30.40:2785",
		"https://deploy:secret@gateway.private.example:2785",
		"https://gateway.private.example:2785/runtime",
		"https://gateway.private.example:0",
	} {
		repo := NewMemoryGovernanceStore()
		_, err := (&GovernanceService{Store: repo, RequireCanonicalRuntimeURL: true}).RegisterNode(context.Background(), Node{Name: "invalid-runtime-authority", Status: "OFFLINE", InternalURL: raw}, "maker", "register")
		if err == nil {
			t.Fatalf("governance accepted unusable runtime authority %q", raw)
		}
	}
	repo := NewMemoryGovernanceStore()
	node, err := (&GovernanceService{Store: repo, RequireCanonicalRuntimeURL: true}).RegisterNode(context.Background(), Node{Name: "canonical-runtime-authority", Status: "OFFLINE", InternalURL: "https://Gateway.Private.Example.:443/"}, "maker", "register")
	if err != nil || node.InternalURL != "https://gateway.private.example" {
		t.Fatalf("governance did not canonicalize valid runtime authority: node=%#v err=%v", node, err)
	}
}

func TestRuntimeRegistrationAcceptsGovernedDevelopmentHTTPInternalURL(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "dev-http", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	governedURL := "http://10.20.30.40:2785"
	node, err := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "dev-http-node", Status: "OFFLINE", InternalURL: governedURL, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	if err != nil {
		t.Fatalf("development governance rejected private HTTP runtime authority: %v", err)
	}
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "dev-http-boot", RuntimeSequence: 1, InternalURL: governedURL, Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-dev-http-123456789", raw)
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }}
	updated, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw)
	if err != nil {
		t.Fatalf("development runtime registration rejected governed private HTTP authority: %v", err)
	}
	if updated.Status != "READY" || updated.InternalURL != governedURL {
		t.Fatalf("unexpected development runtime state: %#v", updated)
	}
}

func TestRuntimeRegistrationAcceptsComposeDevelopmentGatewayAuthority(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "dev-compose-http", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	governedURL := "http://openwa-gateway:2785"
	node, err := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "dev-compose-http-node", Status: "OFFLINE", InternalURL: governedURL, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	if err != nil {
		t.Fatalf("development governance rejected Compose gateway authority: %v", err)
	}
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "dev-compose-http-boot", RuntimeSequence: 1, InternalURL: governedURL, Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-dev-compose-http-123", raw)
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }}
	updated, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw)
	if err != nil {
		t.Fatalf("development runtime registration rejected Compose gateway authority: %v", err)
	}
	if updated.Status != "READY" || updated.InternalURL != governedURL {
		t.Fatalf("unexpected Compose development runtime state: %#v", updated)
	}
}

func TestRuntimeRegistrationAcceptsMatchingGovernedInternalURL(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "url-match", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	governedURL := "https://Gateway-A.Private.Example.:443/"
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "url-match-node", Status: "OFFLINE", InternalURL: governedURL, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "b", RuntimeSequence: 1, InternalURL: "https://gateway-a.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-url-match-12345678901", raw)
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }, AllowedInternalHosts: []string{"gateway-a.private.example"}}
	stored, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw)
	if err != nil {
		t.Fatalf("matching governed internal URL was rejected: %v", err)
	}
	if stored.InternalURL != report.InternalURL {
		t.Fatalf("matching governed URL was not canonicalized: got %q want %q", stored.InternalURL, report.InternalURL)
	}
}

func TestMemoryRuntimeStoreRechecksGovernedAuthorityInsideLock(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pools := &GatewayPoolService{Store: repo}
	poolA, _ := pools.Create(context.Background(), GatewayPool{Name: "store-authority-a", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	poolB, _ := pools.Create(context.Background(), GatewayPool{Name: "store-authority-b", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "store-authority-node", Status: "OFFLINE", InternalURL: "https://gateway-a.private.example", GatewayPoolID: poolA.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: poolB.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "boot-store-authority", RuntimeSequence: 1, InternalURL: "https://gateway-b.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	if _, err := repo.ApplyRuntimeReport(context.Background(), node.ID, node.Version, report, "nonce-store-authority-12345", "hash", now.Add(time.Minute), now); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("serialized runtime store accepted governed authority drift: %v", err)
	}
	stored, _ := repo.GetNode(context.Background(), node.ID)
	if stored.GatewayPoolID != poolA.ID || stored.InternalURL != "https://gateway-a.private.example" {
		t.Fatalf("runtime store rewrote governed authority: %#v", stored)
	}
}

func TestRuntimeRegistrationBootstrapsUngovernedInternalURL(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "url-bootstrap", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "url-bootstrap-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "b", RuntimeSequence: 1, InternalURL: "https://gateway-bootstrap.private.example:2785", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-url-bootstrap-1234567", raw)
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }, AllowedInternalHosts: []string{"gateway-bootstrap.private.example"}}
	stored, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw)
	if err != nil {
		t.Fatalf("first runtime URL registration was rejected: %v", err)
	}
	if stored.InternalURL != report.InternalURL {
		t.Fatalf("first runtime URL was not governed: got %q want %q", stored.InternalURL, report.InternalURL)
	}
}

func TestRuntimeRegistrationUnassignedNodeRejectionDoesNotLaunderDeclaredPool(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	node, err := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "unassigned-node", Status: "OFFLINE"}, "maker", "register")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	declaredPoolID := "00000000-0000-4000-8000-000000000099"
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: declaredPoolID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "unassigned-boot", RuntimeSequence: 1, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-unassigned-memory-1234", raw)
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }, AllowedInternalHosts: []string{"gateway.private.example"}}
	if _, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw); err == nil {
		t.Fatal("unassigned node's nonexistent gateway pool report was accepted")
	}
	events, err := svc.Events(context.Background(), node.ID, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("expected one rejection event, got %#v %v", events, err)
	}
	if events[0].GatewayPoolID != "" {
		t.Fatalf("declared nonexistent pool was laundered into governed evidence: %#v", events[0])
	}
	if events[0].RuntimeIdentity["declaredGatewayPoolId"] != declaredPoolID {
		t.Fatalf("declared pool was not preserved in immutable runtime identity: %#v", events[0].RuntimeIdentity)
	}
}

type rejectOnceRuntimeStore struct {
	*MemoryGovernanceStore
	fail bool
}

func (s *rejectOnceRuntimeStore) RecordRuntimeRejection(ctx context.Context, nodeID string, report RuntimeReport, nonce, requestHash, reason string, nonceExpiresAt, now time.Time) error {
	if s.fail {
		s.fail = false
		return errors.New("injected rejection persistence failure")
	}
	return s.MemoryGovernanceStore.RecordRuntimeRejection(ctx, nodeID, report, nonce, requestHash, reason, nonceExpiresAt, now)
}

func TestRuntimeRegistrationRejectionPersistenceFailureDoesNotBurnNonce(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "atomic-rejection", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "atomic-rejection-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	now := time.Now().UTC()
	report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: "00000000-0000-4000-8000-000000000098", Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "atomic-boot", RuntimeSequence: 1, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: now}
	raw, _ := json.Marshal(report)
	secret := []byte("01234567890123456789012345678901")
	ts, nonce, sig, _ := SignRuntimeReport(secret, now, "nonce-atomic-rejection-1234", raw)
	store := &rejectOnceRuntimeStore{MemoryGovernanceStore: repo, fail: true}
	svc := &RuntimeRegistrationService{Store: store, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, Clock: func() time.Time { return now }, AllowedInternalHosts: []string{"gateway.private.example"}}
	if _, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw); err == nil || errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("expected injected persistence failure, got %v", err)
	}
	if _, err := svc.Register(context.Background(), node.ID, ts, nonce, sig, raw); err == nil || errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("failed rejection persistence burned the nonce: %v", err)
	}
	events, _ := svc.Events(context.Background(), node.ID, 10)
	if len(events) != 1 || events[0].EventType != "REJECTED" {
		t.Fatalf("retry did not persist exactly one rejection event: %#v", events)
	}
}

func TestRuntimeRegistrationCannotLeaveDrainingWithoutNewBoot(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "draining-terminal", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "draining-terminal-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	secret := []byte("01234567890123456789012345678901")
	base := time.Now().UTC()
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, AllowedInternalHosts: []string{"gateway.private.example"}}
	var sequence int64
	register := func(state RuntimeState, bootID, nonce string, at time.Time) error {
		sequence++
		report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: bootID, RuntimeSequence: sequence, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: state, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: at}
		raw, _ := json.Marshal(report)
		ts, signedNonce, sig, _ := SignRuntimeReport(secret, at, nonce, raw)
		svc.Clock = func() time.Time { return at }
		_, err := svc.Register(context.Background(), node.ID, ts, signedNonce, sig, raw)
		return err
	}
	if err := register(RuntimeDraining, "boot-a", "nonce-draining-terminal-1", base); err != nil {
		t.Fatalf("DRAINING registration failed: %v", err)
	}
	if err := register(RuntimeReady, "boot-a", "nonce-draining-terminal-2", base.Add(time.Second)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("same-boot READY reopened DRAINING node: %v", err)
	}
	stored, _ := repo.GetNode(context.Background(), node.ID)
	if stored.RuntimeState != RuntimeDraining || !stored.Draining {
		t.Fatalf("late READY changed terminal DRAINING state: %#v", stored)
	}
	if err := register(RuntimeReady, "boot-b", "nonce-draining-terminal-3", base.Add(2*time.Second)); err != nil {
		t.Fatalf("new boot could not leave DRAINING: %v", err)
	}
	if err := register(RuntimeReady, "boot-a", "nonce-draining-terminal-4", base.Add(3*time.Second)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("retired boot clobbered replacement boot with READY: %v", err)
	}
	if err := register(RuntimeDraining, "boot-a", "nonce-draining-terminal-5", base.Add(4*time.Second)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("retired boot clobbered replacement boot with DRAINING: %v", err)
	}
	stored, _ = repo.GetNode(context.Background(), node.ID)
	if stored.RuntimeState != RuntimeReady || stored.Draining || stored.BootID != "boot-b" {
		t.Fatalf("retired boot changed replacement runtime state: %#v", stored)
	}
	if err := register(RuntimeReady, "boot-a", "nonce-draining-terminal-6", base.Add(1500*time.Millisecond)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("stale retired-boot observation replaced newer runtime state: %v", err)
	}
	stored, _ = repo.GetNode(context.Background(), node.ID)
	if stored.RuntimeState != RuntimeReady || stored.Draining || stored.BootID != "boot-b" {
		t.Fatalf("stale observation changed replacement runtime state: %#v", stored)
	}
}

func TestRuntimeRegistrationRejectsRejectionOnlyBootAfterReplacementIsActive(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "rejection-only-retired", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "rejection-only-retired-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", InternalURL: "https://gateway.private.example"}, "maker", "register")
	secret := []byte("01234567890123456789012345678901")
	base := time.Now().UTC()
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, AllowedInternalHosts: []string{"gateway.private.example"}, RequireCanonicalRuntimeURL: true}
	register := func(bootID string, expected, sequence int64, nonce string, at time.Time) error {
		report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: expected, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: bootID, RuntimeSequence: sequence, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: at}
		raw, _ := json.Marshal(report)
		ts, signedNonce, sig, _ := SignRuntimeReport(secret, at, nonce, raw)
		svc.Clock = func() time.Time { return at }
		_, err := svc.Register(context.Background(), node.ID, ts, signedNonce, sig, raw)
		return err
	}
	if err := register("boot-a", node.Version+1, 1, "nonce-rejection-only-a1", base); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("boot-a expected initial rejection, got %v", err)
	}
	if err := register("boot-b", node.Version, 1, "nonce-rejection-only-b1", base.Add(time.Second)); err != nil {
		t.Fatalf("replacement boot-b failed: %v", err)
	}
	if err := register("boot-a", node.Version, 2, "nonce-rejection-only-a2", base.Add(2*time.Second)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("rejection-only retired boot reclaimed active replacement: %v", err)
	}
	stored, _ := repo.GetNode(context.Background(), node.ID)
	if stored.BootID != "boot-b" || stored.RuntimeState != RuntimeReady {
		t.Fatalf("rejection-only retired boot changed active replacement: %#v", stored)
	}
}

func TestRuntimeRegistrationRejectsDelayedOlderBootFirstReportAfterReplacement(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "delayed-older-boot", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo, RequireCanonicalRuntimeURL: true, RuntimeAllowedInternalHosts: []string{"gateway.private.example"}}).RegisterNode(context.Background(), Node{Name: "delayed-older-boot-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", InternalURL: "https://gateway.private.example"}, "maker", "register")
	secret := []byte("01234567890123456789012345678901")
	base := time.Now().UTC().Truncate(time.Millisecond)
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, AllowedInternalHosts: []string{"gateway.private.example"}, RequireCanonicalRuntimeURL: true}
	register := func(bootID string, sequence int64, observedAt, receivedAt time.Time, nonce string) error {
		report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: bootID, RuntimeSequence: sequence, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: observedAt}
		raw, _ := json.Marshal(report)
		ts, signedNonce, sig, _ := SignRuntimeReport(secret, receivedAt, nonce, raw)
		svc.Clock = func() time.Time { return receivedAt }
		_, err := svc.Register(context.Background(), node.ID, ts, signedNonce, sig, raw)
		return err
	}
	if err := register("boot-b", 1, base.Add(time.Second), base.Add(time.Second), "nonce-delayed-boot-b-123"); err != nil {
		t.Fatalf("replacement boot-b failed: %v", err)
	}
	if err := register("boot-a", 1, base, base.Add(2*time.Second), "nonce-delayed-boot-a-123"); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("delayed older boot first report reclaimed active replacement: %v", err)
	}
	stored, _ := repo.GetNode(context.Background(), node.ID)
	if stored.BootID != "boot-b" || stored.RuntimeState != RuntimeReady {
		t.Fatalf("delayed older boot changed active replacement: %#v", stored)
	}
}

func TestRuntimeRegistrationRejectsOlderBootRetryWithNewerObservedAt(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "older-retry", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo, RequireCanonicalRuntimeURL: true, RuntimeAllowedInternalHosts: []string{"gateway.private.example"}}).RegisterNode(context.Background(), Node{Name: "older-retry-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", InternalURL: "https://gateway.private.example"}, "maker", "register")
	secret := []byte("01234567890123456789012345678901")
	base := time.Now().UTC().Truncate(time.Millisecond)
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, AllowedInternalHosts: []string{"gateway.private.example"}, RequireCanonicalRuntimeURL: true}
	register := func(bootID string, sequence int64, observedAt, receivedAt time.Time, uptime int64, nonce string) error {
		health := completeRuntimeResourceHealth()
		health.ProcessUptimeSeconds = uptime
		report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: bootID, RuntimeSequence: sequence, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: health, ObservedAt: observedAt}
		raw, _ := json.Marshal(report)
		ts, signedNonce, sig, _ := SignRuntimeReport(secret, receivedAt, nonce, raw)
		svc.Clock = func() time.Time { return receivedAt }
		_, err := svc.Register(context.Background(), node.ID, ts, signedNonce, sig, raw)
		return err
	}
	if err := register("boot-b", 1, base.Add(time.Second), base.Add(time.Second), 5, "nonce-new-boot-b-123"); err != nil {
		t.Fatalf("newer replacement boot-b failed: %v", err)
	}
	if err := register("boot-a", 2, base.Add(2*time.Second), base.Add(2*time.Second), 300, "nonce-old-boot-a-retry-123"); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("older boot retry with newer observedAt reclaimed active replacement: %v", err)
	}
	stored, _ := repo.GetNode(context.Background(), node.ID)
	if stored.BootID != "boot-b" {
		t.Fatalf("older boot retry changed active boot: %#v", stored)
	}
}

func TestRuntimeRegistrationAcceptsSameBootClockCorrection(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "clock-correction", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "clock-correction-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	secret := []byte("01234567890123456789012345678901")
	base := time.Now().UTC()
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, AllowedInternalHosts: []string{"gateway.private.example"}}
	var sequence int64
	register := func(at time.Time, nonce string) error {
		sequence++
		report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "stable-boot", RuntimeSequence: sequence, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: RuntimeReady, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: at}
		raw, _ := json.Marshal(report)
		ts, signedNonce, sig, _ := SignRuntimeReport(secret, at, nonce, raw)
		svc.Clock = func() time.Time { return base.Add(time.Second) }
		_, err := svc.Register(context.Background(), node.ID, ts, signedNonce, sig, raw)
		return err
	}
	if err := register(base, "nonce-clock-correction-1"); err != nil {
		t.Fatalf("initial heartbeat failed: %v", err)
	}
	if err := register(base.Add(-500*time.Millisecond), "nonce-clock-correction-2"); err != nil {
		t.Fatalf("same-boot clock correction fenced a healthy runtime: %v", err)
	}
	sequence--
	if err := register(base.Add(time.Second), "nonce-clock-correction-3"); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("duplicate same-boot runtime sequence was accepted: %v", err)
	}
}

func TestRuntimeRegistrationTreatsRejectedDrainAsBootTerminal(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "rejected-drain-terminal", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "rejected-drain-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	secret := []byte("01234567890123456789012345678901")
	base := time.Now().UTC()
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, AllowedInternalHosts: []string{"gateway.private.example"}}
	register := func(state RuntimeState, bootID string, sequence int64, nonce string, at time.Time) error {
		report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: node.Version, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: bootID, RuntimeSequence: sequence, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: state, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: at}
		raw, _ := json.Marshal(report)
		ts, signedNonce, sig, _ := SignRuntimeReport(secret, at, nonce, raw)
		svc.Clock = func() time.Time { return at }
		_, err := svc.Register(context.Background(), node.ID, ts, signedNonce, sig, raw)
		return err
	}
	if err := register(RuntimeReady, "boot-a", 1, "nonce-rejected-drain-a-123", base); err != nil {
		t.Fatal(err)
	}
	if err := register(RuntimeDraining, "boot-c", 1, "nonce-rejected-drain-c1-12", base.Add(time.Second)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("replacement boot unexpectedly entered DRAINING: %v", err)
	}
	if err := register(RuntimeReady, "boot-b", 1, "nonce-rejected-drain-b-123", base.Add(2*time.Second)); err != nil {
		t.Fatalf("replacement boot failed: %v", err)
	}
	if err := register(RuntimeReady, "boot-c", 2, "nonce-rejected-drain-c2-12", base.Add(3*time.Second)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("boot left an authenticated DRAINING declaration after rejection: %v", err)
	}
}

func TestRuntimeRegistrationTreatsVersionConflictDrainAsBootTerminal(t *testing.T) {
	repo := NewMemoryGovernanceStore()
	pool, _ := (&GatewayPoolService{Store: repo}).Create(context.Background(), GatewayPool{Name: "version-conflict-drain", Provider: GatewayProviderOpenWA, Engine: GatewayEngineBaileys, AdapterVersion: "a1", Status: GatewayPoolActive, Capabilities: []Capability{CapabilitySendText}, MinimumHealthyNodes: 1}, "maker", "bootstrap")
	node, _ := (&GovernanceService{Store: repo}).RegisterNode(context.Background(), Node{Name: "version-conflict-drain-node", Status: "OFFLINE", GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1"}, "maker", "register")
	secret := []byte("01234567890123456789012345678901")
	base := time.Now().UTC()
	svc := &RuntimeRegistrationService{Store: repo, GatewayPools: repo, Secret: secret, MaximumSkew: time.Minute, AllowedInternalHosts: []string{"gateway.private.example"}}
	register := func(state RuntimeState, expected, sequence int64, nonce string, at time.Time) error {
		report := RuntimeReport{NodeID: node.ID, ExpectedNodeVersion: expected, GatewayPoolID: pool.ID, Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "a1", GatewayVersion: "v", WorkerVersion: "w", ConfigurationVersion: "c", BootID: "version-conflict-boot", RuntimeSequence: sequence, InternalURL: "https://gateway.private.example", Capabilities: []Capability{CapabilitySendText}, RuntimeState: state, Capacity: 1, ResourceHealth: completeRuntimeResourceHealth(), ObservedAt: at}
		raw, _ := json.Marshal(report)
		ts, signedNonce, sig, _ := SignRuntimeReport(secret, at, nonce, raw)
		svc.Clock = func() time.Time { return at }
		_, err := svc.Register(context.Background(), node.ID, ts, signedNonce, sig, raw)
		return err
	}
	if err := register(RuntimeReady, node.Version, 1, "nonce-version-conflict-ready-1", base); err != nil {
		t.Fatal(err)
	}
	governed, err := (&GovernanceService{Store: repo}).TransitionNode(context.Background(), node.ID, node.Version, "OFFLINE", "operator", "maintenance version bump")
	if err != nil {
		t.Fatal(err)
	}
	if err := register(RuntimeDraining, node.Version, 2, "nonce-version-conflict-drain-1", base.Add(time.Second)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("stale-version DRAINING report did not use the auditable rejection path: %v", err)
	}
	if err := register(RuntimeDraining, node.Version, 2, "nonce-version-conflict-drain-1", base.Add(time.Second)); !errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("stale-version DRAINING rejection did not consume its nonce: %v", err)
	}
	if err := register(RuntimeReady, governed.Version, 3, "nonce-version-conflict-ready-2", base.Add(2*time.Second)); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("same boot left an authenticated version-conflict DRAINING declaration: %v", err)
	}
	events, _ := svc.Events(context.Background(), node.ID, 10)
	found := false
	for _, event := range events {
		if event.EventType == "REJECTED" && event.BootID == "version-conflict-boot" && fmt.Sprint(event.RuntimeIdentity["runtimeState"]) == string(RuntimeDraining) && strings.Contains(event.Reason, "version") {
			found = true
		}
	}
	if !found {
		t.Fatalf("version-conflict DRAINING rejection evidence is missing: %#v", events)
	}
}

func TestCanonicalRuntimeInternalURLRejectsMalformedURLWithoutPanic(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("malformed runtime URL must be rejected without panic: %v", recovered)
		}
	}()

	canonical, valid := canonicalRuntimeInternalURLForMode("%", nil, nil, false)
	if valid || canonical != "" {
		t.Fatalf("malformed runtime URL canonicalized to %q valid=%v", canonical, valid)
	}
}
