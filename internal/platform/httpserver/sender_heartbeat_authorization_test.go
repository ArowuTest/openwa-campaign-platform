package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campaign-platform/internal/sender"
)

func TestLegacySenderNodeHeartbeatRouteIsNotExposed(t *testing.T) {
	server := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/internal/sender-nodes/node-1/heartbeat", nil)
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("legacy node heartbeat route remains exposed: status=%d body=%s", response.Code, response.Body.String())
	}
}

func heartbeatHTTPFixture(t *testing.T) (*Server, sender.Node, sender.GovernedSession, time.Time, []byte) {
	t.Helper()
	ctx := context.Background()
	store := sender.NewMemoryGovernanceStore()
	governance := &sender.GovernanceService{Store: store}
	node, err := governance.RegisterNode(ctx, sender.Node{Name: "http-heartbeat-node", Status: "READY", Capacity: 2}, "actor", "approved node")
	if err != nil {
		t.Fatal(err)
	}
	session, err := governance.RegisterSession(ctx, sender.GovernedSession{
		NodeID: node.ID, MaskedMSISDN: "+234 *** 3000", OwnerReference: "http-heartbeat",
		RegistrationCountryISO2: "NG", ProfileDisplayName: "HTTP heartbeat sender",
		RecoveryReference: "vault://task6/http-heartbeat", EngineType: "WHATSAPP_WEB_JS",
		SafeMessagesPerMinute: 25, SafeDailyCapacity: 500, InFlightLimit: 2,
	}, []byte("cipher"), "actor", "approved sender")
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []sender.Status{sender.StatusPairing, sender.StatusConnecting, sender.StatusReady} {
		session, err = governance.TransitionSession(ctx, session.ID, session.Version, status, "actor", "approved lifecycle transition")
		if err != nil {
			t.Fatal(err)
		}
	}
	fixed := time.Now().UTC().Truncate(time.Second)
	secret := bytes.Repeat([]byte{0x51}, 32)
	runtime := &sender.RuntimeRegistrationService{Store: store, Secret: secret, Clock: func() time.Time { return fixed }}
	server := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		SenderSessionHeartbeat: &sender.SessionHeartbeatService{Governance: governance, Runtime: runtime},
	})
	return server, node, session, fixed, secret
}

func signedHeartbeatRequest(t *testing.T, methodURL string, fixed time.Time, secret []byte, nonce string, report sender.SessionHeartbeatReport) *http.Request {
	t.Helper()
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	timestamp, signedNonce, signature, err := sender.SignRuntimeReport(secret, fixed, nonce, raw)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, methodURL, bytes.NewReader(raw))
	request.Header.Set(sender.RuntimeTimestampHeader, timestamp)
	request.Header.Set(sender.RuntimeNonceHeader, signedNonce)
	request.Header.Set(sender.RuntimeSignatureHeader, signature)
	return request
}

func TestSenderSessionHeartbeatRequiresMachineSignatureAndRejectsReplay(t *testing.T) {
	server, node, session, fixed, secret := heartbeatHTTPFixture(t)
	report := sender.SessionHeartbeatReport{
		NodeID: node.ID, SessionID: session.ID, Status: sender.StatusReady,
		EngineVersion: "engine-http-1", SentToday: 44,
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	url := "/api/v1/internal/sender-sessions/" + session.ID + "/heartbeat"
	unsigned := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	unsigned.Header.Set("Authorization", "Bearer ordinary-operator-token")
	unsignedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unsignedResponse, unsigned)
	if unsignedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned heartbeat was not rejected: status=%d body=%s", unsignedResponse.Code, unsignedResponse.Body.String())
	}

	signed := signedHeartbeatRequest(t, url, fixed, secret, "http-session-heartbeat-0001", report)
	signedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(signedResponse, signed)
	if signedResponse.Code != http.StatusOK {
		t.Fatalf("signed heartbeat was rejected: status=%d body=%s", signedResponse.Code, signedResponse.Body.String())
	}

	replay := signedHeartbeatRequest(t, url, fixed, secret, "http-session-heartbeat-0001", report)
	replayResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusConflict {
		t.Fatalf("heartbeat nonce replay was not rejected: status=%d body=%s", replayResponse.Code, replayResponse.Body.String())
	}
}
