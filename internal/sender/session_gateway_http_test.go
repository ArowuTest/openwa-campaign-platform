package sender

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPSessionGatewayStartCarriesGovernedProxy(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sessions/session-1/start" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"session-1","status":"initializing"}`))
	}))
	defer server.Close()

	gateway := &HTTPSessionGateway{CommandSecret: "01234567890123456789012345678901"}
	proxy := &SessionProxyConfiguration{URL: "socks5://user:secret@proxy.example:1080", Type: ProxySOCKS5}
	runtime := &SessionTransportRuntimeConfiguration{
		ReconnectMode: ReconnectBounded, ReconnectMaxAttempts: 4, ReconnectBaseDelayMs: 7000,
		ReconnectStabilityResetMs: 240000, WatchdogProbeTimeoutMs: 9000, WatchdogFailureThreshold: 3,
		EngineTeardownTimeoutMs: 45000, Source: "GOVERNED_CONFIGURATION", ConfigurationID: "config-1",
		ScopeType: "SENDER_SESSION", ScopeID: "session-1", Version: 7,
	}
	_, err := gateway.Start(context.Background(), Node{
		ID: "node-1", InternalURL: server.URL, GatewayPoolID: "pool-1",
		Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "0.13.0", Version: 7,
	}, GovernedSession{ID: "session-1"}, proxy, runtime)
	if err != nil {
		t.Fatal(err)
	}
	proxyBody, ok := got["proxy"].(map[string]any)
	if !ok {
		t.Fatalf("proxy body missing: %#v", got)
	}
	if proxyBody["url"] != proxy.URL || proxyBody["type"] != string(proxy.Type) {
		t.Fatalf("proxy body=%#v", proxyBody)
	}
	runtimeBody, ok := got["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("runtime body missing: %#v", got)
	}
	if runtimeBody["reconnectMode"] != string(ReconnectBounded) || runtimeBody["configurationId"] != "config-1" || runtimeBody["scopeType"] != "SENDER_SESSION" {
		t.Fatalf("runtime body=%#v", runtimeBody)
	}
}

func TestHTTPSessionGatewayDoesNotFollowSignedRedirects(t *testing.T) {
	redirectHits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"session-1","status":"ready"}`))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/capture", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	gateway := &HTTPSessionGateway{CommandSecret: "01234567890123456789012345678901"}
	_, err := gateway.Start(context.Background(), Node{ID: "node-1", InternalURL: origin.URL, GatewayPoolID: "pool-1", Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "0.13.0", Version: 7}, GovernedSession{ID: "session-1"}, nil, nil)
	if err == nil {
		t.Fatal("signed session command followed redirect and reported success")
	}
	if redirectHits != 0 {
		t.Fatalf("signed session command reached redirect target %d time(s)", redirectHits)
	}
}

func TestHTTPSessionGatewayRejectsNonHTTPNodeURL(t *testing.T) {
	gateway := &HTTPSessionGateway{CommandSecret: "01234567890123456789012345678901"}
	_, err := gateway.Health(context.Background(), Node{
		ID: "node-1", InternalURL: "ftp://gateway.example", GatewayPoolID: "pool-1",
		Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "0.13.0", Version: 7,
	}, GovernedSession{ID: "session-1"})
	if err == nil || !strings.Contains(err.Error(), "valid governed gateway node URL") {
		t.Fatalf("non-HTTP governed node URL was not rejected at validation: %v", err)
	}
}
