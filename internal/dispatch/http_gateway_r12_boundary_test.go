package dispatch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPGatewayTreatsPostSubmitRateLimitAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()
	_, err := (&HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client()}).SendPrepared(context.Background(), validGatewayRequest(time.Now().UTC()))
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailureOutcomeUnknown {
		t.Fatalf("post-submit 429 must be UNKNOWN to prevent automatic resend, got %#v err=%v", gatewayErr, err)
	}
}
func TestHTTPGatewayTreatsPostSubmitRequestTimeoutAsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "request timeout", http.StatusRequestTimeout)
	}))
	defer server.Close()
	_, err := (&HTTPGateway{BaseURL: server.URL, CommandSecret: testGatewaySecret, Client: server.Client()}).SendPrepared(context.Background(), validGatewayRequest(time.Now().UTC()))
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailureOutcomeUnknown {
		t.Fatalf("post-submit 408 must be UNKNOWN, got %#v err=%v", gatewayErr, err)
	}
}

func TestHTTPGatewayDoesNotFollowRedirectAfterSubmission(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true,"providerMessageId":"redirected","acceptedAt":"2026-08-16T21:30:00Z"}`))
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()
	_, err := (&HTTPGateway{BaseURL: redirector.URL, CommandSecret: testGatewaySecret, Client: redirector.Client()}).SendPrepared(context.Background(), validGatewayRequest(time.Now().UTC()))
	if targetCalls != 0 {
		t.Fatalf("gateway followed a post-submit redirect to a second authority: calls=%d", targetCalls)
	}
	var gatewayErr GatewayError
	if !errors.As(err, &gatewayErr) || gatewayErr.Safety != FailureOutcomeUnknown {
		t.Fatalf("redirect response after submission must be UNKNOWN, got %#v err=%v", gatewayErr, err)
	}
}
