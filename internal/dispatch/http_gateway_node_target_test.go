package dispatch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPGatewayUsesRequestSpecificGovernedNodeURL(t *testing.T) {
	staticCalls := 0
	static := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		staticCalls++
		http.Error(w, "wrong node", http.StatusConflict)
	}))
	defer static.Close()
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":true,"providerMessageId":"node-b-accepted","acceptedAt":"2026-08-17T18:00:00Z"}`))
	}))
	defer target.Close()
	now := time.Date(2026, 8, 17, 18, 0, 0, 0, time.UTC)
	request := validGatewayRequest(now)
	request.GatewayNodeURL = target.URL
	gateway := &HTTPGateway{BaseURL: static.URL, CommandSecret: testGatewaySecret, Client: target.Client(), Clock: func() time.Time { return now }}
	result, err := gateway.Send(context.Background(), request)
	if err != nil || !result.Accepted || result.ProviderMessageID != "node-b-accepted" {
		t.Fatalf("node-addressed send result=%#v err=%v", result, err)
	}
	if targetCalls != 1 || staticCalls != 0 {
		t.Fatalf("request targeted wrong gateway: target=%d static=%d", targetCalls, staticCalls)
	}
}

func TestHTTPGatewayRejectsSelectedNodeWithoutNodeURLInsteadOfStaticFallback(t *testing.T) {
	now := time.Date(2026, 8, 17, 18, 30, 0, 0, time.UTC)
	request := validGatewayRequest(now)
	request.GatewayNodeURL = ""
	gateway := &HTTPGateway{BaseURL: "https://legacy-static.example", RequireNodeURL: true, CommandSecret: testGatewaySecret, Clock: func() time.Time { return now }}
	err := gateway.Preflight(context.Background(), request)
	if err == nil {
		t.Fatal("selected governed node without its own URL fell back to the static gateway router")
	}
}
