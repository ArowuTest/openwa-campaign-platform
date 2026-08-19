package sender

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSessionGatewayCommandSignatureBindsTargetIdentity(t *testing.T) {
	gateway := &HTTPSessionGateway{
		CommandSecret: "01234567890123456789012345678901",
		Clock:         func() time.Time { return time.Date(2026, 8, 17, 5, 0, 0, 0, time.UTC) },
		Nonce:         func() (string, error) { return "fixed-r16-command-nonce", nil },
	}
	newRequest := func(nodeID string) *http.Request {
		req, err := http.NewRequest(http.MethodPost, "http://gateway.local/v1/sessions/session-1/start", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Gateway-Target-Node-ID", nodeID)
		req.Header.Set("X-Gateway-Target-Node-Version", "7")
		req.Header.Set("X-Gateway-Target-Pool-ID", "pool-1")
		req.Header.Set("X-Gateway-Target-Provider", "OPENWA")
		req.Header.Set("X-Gateway-Target-Engine", "BAILEYS")
		req.Header.Set("X-Gateway-Target-Adapter-Version", "0.13.0")
		return req
	}
	first := newRequest("node-a")
	second := newRequest("node-b")
	if err := gateway.sign(first, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := gateway.sign(second, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	firstSignature := first.Header.Get("X-Gateway-Signature")
	secondSignature := second.Header.Get("X-Gateway-Signature")
	if firstSignature == "" || secondSignature == "" {
		t.Fatal("gateway signatures were not generated")
	}
	if firstSignature == secondSignature {
		t.Fatalf("target node identity was not bound into command MAC: %s", firstSignature)
	}
}
