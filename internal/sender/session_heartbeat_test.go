package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"campaign-platform/internal/gateway"
)

func readyHeartbeatSession(t *testing.T, ctx context.Context, governance *GovernanceService) (Node, GovernedSession) {
	t.Helper()
	node, err := governance.RegisterNode(ctx, Node{Name: "heartbeat-node", BootID: "boot-a", Status: "READY", Capacity: 4}, "actor", "approved node")
	if err != nil {
		t.Fatal(err)
	}
	session, err := governance.RegisterSession(ctx, GovernedSession{
		NodeID: node.ID, MaskedMSISDN: "+234 *** 2000", OwnerReference: "task6-heartbeat",
		RegistrationCountryISO2: "NG", ProfileDisplayName: "Heartbeat sender",
		RecoveryReference: "vault://task6/session-heartbeat", EngineType: "WHATSAPP_WEB_JS",
		SafeMessagesPerMinute: 25, SafeDailyCapacity: 500, InFlightLimit: 2,
	}, []byte("cipher"), "actor", "approved sender")
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []Status{StatusPairing, StatusConnecting, StatusReady} {
		session, err = governance.TransitionSession(ctx, session.ID, session.Version, status, "actor", "approved lifecycle transition")
		if err != nil {
			t.Fatal(err)
		}
	}
	return node, session
}

func signedSessionHeartbeat(t *testing.T, secret []byte, when time.Time, nonce string, report SessionHeartbeatReport) ([]byte, string, string, string) {
	t.Helper()
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	timestamp, signedNonce, signature, err := SignRuntimeReport(secret, when, nonce, raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw, timestamp, signedNonce, signature
}

func TestSessionHeartbeatRequiresSignedNodeBoundReplaySafeTelemetry(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	governance := &GovernanceService{Store: store}
	node, session := readyHeartbeatSession(t, ctx, governance)
	fixed := time.Now().UTC().Truncate(time.Second)
	secret := bytes.Repeat([]byte{0x42}, 32)
	leases := gateway.NewMemoryLeaseStore()
	service := &SessionHeartbeatService{
		Governance: governance,
		Runtime:    &RuntimeRegistrationService{Store: store, Secret: secret, Clock: func() time.Time { return fixed }},
		Leases:     leases,
		LeaseTTL:   90 * time.Second,
	}
	report := SessionHeartbeatReport{
		NodeID: node.ID, SessionID: session.ID, BootID: node.BootID, Status: StatusReady,
		EngineVersion: "engine-1", SentToday: 42,
	}
	raw, timestamp, nonce, signature := signedSessionHeartbeat(t, secret, fixed, "session-heartbeat-0001", report)
	updated, err := service.Heartbeat(ctx, session.ID, timestamp, nonce, signature, raw)
	if err != nil {
		t.Fatal(err)
	}
	if updated.NodeID != node.ID || updated.Status != StatusReady || updated.EngineVersion != "engine-1" || updated.SentToday != 42 {
		t.Fatalf("unexpected signed heartbeat result: %+v", updated)
	}
	if updated.SafeMessagesPerMinute != 25 || updated.SafeDailyCapacity != 500 || updated.InFlightLimit != 2 {
		t.Fatalf("signed heartbeat rewrote governed capacity: %+v", updated)
	}
	if _, err := service.Heartbeat(ctx, session.ID, timestamp, nonce, signature, raw); !errors.Is(err, ErrRuntimeReplay) {
		t.Fatalf("expected replay rejection, got %v", err)
	}
	otherNode, err := governance.RegisterNode(ctx, Node{Name: "other-heartbeat-node", Status: "READY", Capacity: 1}, "actor", "approved second node")
	if err != nil {
		t.Fatal(err)
	}
	mismatch := report
	mismatch.NodeID = otherNode.ID
	mismatchRaw, mismatchTS, mismatchNonce, mismatchSig := signedSessionHeartbeat(t, secret, fixed.Add(time.Second), "session-heartbeat-0002", mismatch)
	if _, err := service.Heartbeat(ctx, session.ID, mismatchTS, mismatchNonce, mismatchSig, mismatchRaw); !errors.Is(err, ErrSessionHeartbeatIdentity) {
		t.Fatalf("expected node/session identity rejection, got %v", err)
	}
	staleRaw, staleTS, staleNonce, staleSig := signedSessionHeartbeat(t, secret, fixed.Add(-time.Minute), "session-heartbeat-0003", report)
	if _, err := service.Heartbeat(ctx, session.ID, staleTS, staleNonce, staleSig, staleRaw); !errors.Is(err, ErrSessionHeartbeatStale) {
		t.Fatalf("expected out-of-order heartbeat rejection, got %v", err)
	}
	tooOldRaw, tooOldTS, tooOldNonce, tooOldSig := signedSessionHeartbeat(t, secret, fixed.Add(-10*time.Minute), "session-heartbeat-0004", report)
	if _, err := service.Heartbeat(ctx, session.ID, tooOldTS, tooOldNonce, tooOldSig, tooOldRaw); !errors.Is(err, ErrRuntimeAuthentication) {
		t.Fatalf("expected stale signature rejection, got %v", err)
	}
	if _, err := service.Heartbeat(ctx, session.ID, "", "", "", raw); !errors.Is(err, ErrRuntimeAuthentication) {
		t.Fatalf("expected unsigned heartbeat rejection, got %v", err)
	}
	pathRaw, pathTS, pathNonce, pathSig := signedSessionHeartbeat(t, secret, fixed.Add(2*time.Second), "session-heartbeat-0005", report)
	if _, err := service.Heartbeat(ctx, "different-session", pathTS, pathNonce, pathSig, pathRaw); !errors.Is(err, ErrSessionHeartbeatIdentity) {
		t.Fatalf("expected path/body session mismatch rejection, got %v", err)
	}
}

func TestSessionHeartbeatAcquiresRenewsAndRequiresSafeRecoveryForNewBoot(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryGovernanceStore()
	governance := &GovernanceService{Store: store}
	node, session := readyHeartbeatSession(t, ctx, governance)
	secret := bytes.Repeat([]byte{0x53}, 32)
	leases := gateway.NewMemoryLeaseStore()
	clock := time.Now().UTC().Truncate(time.Second)
	service := &SessionHeartbeatService{
		Governance: governance,
		Runtime:    &RuntimeRegistrationService{Store: store, Secret: secret, Clock: func() time.Time { return clock }},
		Leases:     leases,
		LeaseTTL:   90 * time.Second,
	}
	report := SessionHeartbeatReport{
		NodeID: node.ID, SessionID: session.ID, BootID: node.BootID, Status: StatusReady,
		EngineVersion: "adapter-v1", SentToday: 10,
	}

	raw, timestamp, nonce, signature := signedSessionHeartbeat(t, secret, clock, "lease-heartbeat-0001", report)
	if _, err := service.Heartbeat(ctx, session.ID, timestamp, nonce, signature, raw); err != nil {
		t.Fatal(err)
	}
	first, ok, err := leases.Get(ctx, session.ID)
	if err != nil || !ok {
		t.Fatalf("initial heartbeat did not acquire lease: lease=%+v ok=%v err=%v", first, ok, err)
	}
	if first.WorkerID != node.ID || first.Token != node.BootID || first.Version != 1 || !first.ExpiresAt.Equal(clock.Add(90*time.Second)) {
		t.Fatalf("unexpected initial heartbeat lease: %+v", first)
	}

	clock = clock.Add(30 * time.Second)
	raw, timestamp, nonce, signature = signedSessionHeartbeat(t, secret, clock, "lease-heartbeat-0002", report)
	if _, err := service.Heartbeat(ctx, session.ID, timestamp, nonce, signature, raw); err != nil {
		t.Fatal(err)
	}
	second, ok, err := leases.Get(ctx, session.ID)
	if err != nil || !ok {
		t.Fatalf("renewed heartbeat lease missing: lease=%+v ok=%v err=%v", second, ok, err)
	}
	if second.Version != first.Version+1 || second.Token != node.BootID || !second.ExpiresAt.Equal(clock.Add(90*time.Second)) {
		t.Fatalf("heartbeat did not renew the current boot lease: first=%+v second=%+v", first, second)
	}

	node, err = store.HeartbeatNode(ctx, node.ID, node.Version, Node{BootID: "boot-b", Status: "READY", Capacity: 4}, clock.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	report.BootID = node.BootID
	clock = clock.Add(30 * time.Second)
	raw, timestamp, nonce, signature = signedSessionHeartbeat(t, secret, clock, "lease-heartbeat-0003", report)
	if _, err := service.Heartbeat(ctx, session.ID, timestamp, nonce, signature, raw); !errors.Is(err, gateway.ErrLeaseHeld) {
		t.Fatalf("new boot took over a live lease: %v", err)
	}

	clock = second.ExpiresAt.Add(time.Second)
	raw, timestamp, nonce, signature = signedSessionHeartbeat(t, secret, clock, "lease-heartbeat-0004", report)
	if _, err := service.Heartbeat(ctx, session.ID, timestamp, nonce, signature, raw); !errors.Is(err, ErrSessionHeartbeatRecoveryRequired) {
		t.Fatalf("new boot reactivated READY directly after lease expiry: %v", err)
	}

	report.Status = StatusDisconnected
	clock = clock.Add(time.Second)
	raw, timestamp, nonce, signature = signedSessionHeartbeat(t, secret, clock, "lease-heartbeat-0005", report)
	updated, err := service.Heartbeat(ctx, session.ID, timestamp, nonce, signature, raw)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != StatusDisconnected {
		t.Fatalf("controlled takeover did not enter a non-sending state: %+v", updated)
	}
	takenOver, ok, err := leases.Get(ctx, session.ID)
	if err != nil || !ok {
		t.Fatalf("takeover lease missing: lease=%+v ok=%v err=%v", takenOver, ok, err)
	}
	if takenOver.WorkerID != node.ID || takenOver.Token != node.BootID || takenOver.Version <= second.Version {
		t.Fatalf("controlled takeover did not establish the new boot fence: before=%+v after=%+v", second, takenOver)
	}
}
