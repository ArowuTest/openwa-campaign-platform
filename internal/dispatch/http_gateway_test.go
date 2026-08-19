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

func validGatewayRequest(now time.Time) GatewayRequest {
	return GatewayRequest{
		IdempotencyKey: "1234567890123456", Provider: "OPENWA", Engine: "WHATSAPP_WEB_JS",
		GatewayPoolID: "pool-1", GatewayPoolVersion: 1, GatewayAdapterVersion: "0.13.0",
		GatewayNodeID: "node-1", GatewayNodeVersion: 1, SessionID: "s", SessionLeaseVersion: 1,
		SessionConfigurationVersion: 1, AuthorityExpiresAt: now.Add(10 * time.Minute), RouteReference: "campaign:recipient",
		RecipientE164: "+2348012345678", MessageType: "text", Body: "hello",
	}
}
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
	result, err := gateway.Send(context.Background(), validGatewayRequest(clock()))
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
	_, err := (&HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client()}).Send(context.Background(), validGatewayRequest(time.Now().UTC()))
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailureOutcomeUnknown {
		t.Fatalf("expected unknown, got %v", err)
	}
}
func TestHTTPGatewayTreatsServiceUnavailableAfterSubmitAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "draining", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, err := (&HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client()}).Send(context.Background(), validGatewayRequest(time.Now().UTC()))
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailureOutcomeUnknown {
		t.Fatalf("post-submit 503 must be UNKNOWN, got %v", err)
	}
}
func TestHTTPGatewayFailsClosedWithoutSigningSecret(t *testing.T) {
	_, err := (&HTTPGateway{BaseURL: "https://gateway.example"}).Send(context.Background(), validGatewayRequest(time.Now().UTC()))
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != "GATEWAY_COMMAND_SIGNING_INVALID" || gatewayErr.Safety != FailurePermanent {
		t.Fatalf("expected fail-closed signing error, got %v", err)
	}
}

func TestHTTPGatewayTreatsTimeoutAfterRequestAcceptanceAsUnknown(t *testing.T) {
	requestAccepted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestAccepted)
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 50 * time.Millisecond
	_, err := (&HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: client}).Send(
		context.Background(), validGatewayRequest(time.Now().UTC()),
	)
	select {
	case <-requestAccepted:
	default:
		t.Fatal("gateway never accepted the request before timeout")
	}
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailureOutcomeUnknown {
		t.Fatalf("post-acceptance timeout must be UNKNOWN, got %v", err)
	}
}

func TestHTTPGatewayPreparedSendDoesNotRecheckAuthorityExpiryAfterPreflight(t *testing.T) {
	now := time.Date(2026, 8, 15, 18, 0, 0, 0, time.UTC)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true,"providerMessageId":"openwa-prepared","acceptedAt":"2026-08-15T18:00:02Z"}`))
	}))
	defer server.Close()
	gateway := &HTTPGateway{
		BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client(),
		Clock: func() time.Time { return now }, Nonce: func() (string, error) { return "prepared-fixed-nonce", nil },
	}
	request := validGatewayRequest(now)
	request.AuthorityExpiresAt = now.Add(time.Second)
	if err := gateway.Preflight(context.Background(), request); err != nil {
		t.Fatalf("OpenWA preflight rejected current authority: %v", err)
	}
	now = now.Add(2 * time.Second)
	result, err := gateway.SendPrepared(context.Background(), request)
	if err != nil || !result.Accepted || calls != 1 {
		t.Fatalf("prepared OpenWA send result=%#v calls=%d err=%v", result, calls, err)
	}
}

func TestHTTPGatewayTreatsConflictAfterSubmitAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	}))
	defer server.Close()
	_, err := (&HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client()}).Send(context.Background(), validGatewayRequest(time.Now().UTC()))
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailureOutcomeUnknown {
		t.Fatalf("post-submit 409 must be UNKNOWN, got %v", err)
	}
}

func TestHTTPGatewayPreflightTreatsExpiredAuthorityAsSafeRetry(t *testing.T) {
	now := time.Date(2026, 8, 15, 23, 0, 0, 0, time.UTC)
	request := validGatewayRequest(now)
	request.AuthorityExpiresAt = now.Add(-time.Second)
	gateway := &HTTPGateway{BaseURL: "https://gateway.example.test", CommandSecret: testGatewaySecret, Clock: func() time.Time { return now }}
	err := gateway.Preflight(context.Background(), request)
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != "GATEWAY_AUTHORITY_EXPIRED" || gatewayErr.Safety != FailureSafeToRetry {
		t.Fatalf("expired pre-submit authority must be safely retryable, got %#v err=%v", gatewayErr, err)
	}
}

func TestHTTPGatewayTreatsAcceptedWithoutProviderMessageIDAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer server.Close()
	_, err := (&HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client()}).Send(context.Background(), validGatewayRequest(time.Now().UTC()))
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != "GATEWAY_ACCEPTANCE_UNKNOWN" || gatewayErr.Safety != FailureOutcomeUnknown {
		t.Fatalf("acceptance without provider id must be UNKNOWN, got %#v err=%v", gatewayErr, err)
	}
}

func TestHTTPGatewayNonceFailureBeforeSubmitIsSafeToRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	gateway := &HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client(), Nonce: func() (string, error) { return "", errors.New("entropy unavailable") }}
	_, err := gateway.Send(context.Background(), validGatewayRequest(time.Now().UTC()))
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Code != "GATEWAY_NONCE_FAILED" || gatewayErr.Safety != FailureSafeToRetry {
		t.Fatalf("nonce failure before client.Do must be safe to retry, got %#v err=%v", gatewayErr, err)
	}
	if calls != 0 {
		t.Fatalf("nonce failure reached gateway %d times", calls)
	}
}

func TestRedactedGatewayErrorDoesNotExposeProviderBody(t *testing.T) {
	raw := []byte(`{"error":"recipient +2348012345678 access_token=secret-value"}`)
	if got := redactedGatewayError(raw); got != "gateway rejected request" {
		t.Fatalf("gateway provider body leaked through error detail: %q", got)
	}
}
