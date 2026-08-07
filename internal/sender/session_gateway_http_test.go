package sender

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	_, err := gateway.Start(context.Background(), Node{
		ID: "node-1", InternalURL: server.URL, GatewayPoolID: "pool-1",
		Provider: "OPENWA", Engine: "BAILEYS", AdapterVersion: "0.13.0", Version: 7,
	}, GovernedSession{ID: "session-1"}, proxy)
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
}
