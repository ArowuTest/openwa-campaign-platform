package dispatch

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testGatewaySecret = "01234567890123456789012345678901"

func TestHTTPGatewaySignsCommandAndParsesAcceptance(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		timestamp := r.Header.Get("X-Gateway-Timestamp")
		nonce := r.Header.Get("X-Gateway-Nonce")
		canonical := signedCommandCanonical(r.Method, r.URL.RequestURI(), timestamp, nonce, body)
		mac := hmac.New(sha256.New, []byte(testGatewaySecret))
		_, _ = mac.Write([]byte(canonical))
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if r.Header.Get("X-Gateway-Signature") != expected {
			t.Fatalf("invalid signature")
		}
		if timestamp != "1785830400" || nonce != "fixed-nonce-1234567890" {
			t.Fatalf("unexpected signing metadata: %q %q", timestamp, nonce)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true,"providerMessageId":"p-1","acceptedAt":"2026-08-04T08:00:00Z"}`))
	}))
	defer server.Close()
	gateway := &HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client(), Clock: clock, Nonce: func() (string, error) { return "fixed-nonce-1234567890", nil }}
	result, err := gateway.Send(context.Background(), GatewayRequest{IdempotencyKey: "1234567890123456", SessionID: "s", RecipientE164: "+2348012345678", MessageType: "text", Body: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || result.ProviderMessageID != "p-1" {
		t.Fatalf("unexpected %#v", result)
	}
}
func TestHTTPGatewayTreatsAmbiguousServerErrorAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "failed", http.StatusInternalServerError) }))
	defer server.Close()
	_, err := (&HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client()}).Send(context.Background(), GatewayRequest{IdempotencyKey: "1234567890123456", SessionID: "s", RecipientE164: "+2348012345678", MessageType: "text", Body: "hello"})
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailureOutcomeUnknown {
		t.Fatalf("expected unknown, got %v", err)
	}
}
func TestHTTPGatewayTreatsServiceUnavailableAsSafeRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "draining", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, err := (&HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client()}).Send(context.Background(), GatewayRequest{IdempotencyKey: "1234567890123456", SessionID: "s", RecipientE164: "+2348012345678", MessageType: "text", Body: "hello"})
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailureSafeToRetry || gatewayErr.RetryAfter != 7*time.Second {
		t.Fatalf("expected safe retry, got %v", err)
	}
}
func TestHTTPGatewayFailsClosedWithoutSigningSecret(t *testing.T) {
	_, err := (&HTTPGateway{BaseURL: "https://gateway.example"}).Send(context.Background(), GatewayRequest{IdempotencyKey: "1234567890123456", SessionID: "s", RecipientE164: "+2348012345678", MessageType: "text", Body: "hello"})
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != "GATEWAY_COMMAND_SIGNING_INVALID" || gatewayErr.Safety != FailurePermanent {
		t.Fatalf("expected fail-closed signing error, got %v", err)
	}
}
