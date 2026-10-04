package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"campaign-platform/internal/metacloud"
)

func TestNetworkPolicyDoesNotTrustSpoofedForwardingHeader(t *testing.T) {
	policy := NetworkPolicy{AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, TrustedProxyCIDRs: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	r.RemoteAddr = "198.51.100.10:4567"
	r.Header.Set("X-Forwarded-For", "10.1.2.3")
	r.Header.Set("X-Railway-Edge", "ord1")
	client, allowed := policy.Allows(r)
	if allowed || client.String() != "198.51.100.10" {
		t.Fatalf("spoofed forwarding header trusted: client=%s allowed=%v", client, allowed)
	}
}

func TestNetworkPolicyResolvesClientThroughExplicitTrustedProxy(t *testing.T) {
	policy := NetworkPolicy{AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, TrustedProxyCIDRs: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	r.RemoteAddr = "192.0.2.20:443"
	r.Header.Set("X-Forwarded-For", "10.1.2.3, 192.0.2.19")
	client, allowed := policy.Allows(r)
	if !allowed || client.String() != "10.1.2.3" {
		t.Fatalf("trusted proxy chain not resolved: client=%s allowed=%v", client, allowed)
	}
}

func TestNetworkPolicyResolvesClientFromTrustedRailwayForwardingChain(t *testing.T) {
	policy := NetworkPolicy{
		AllowedCIDRs:      []netip.Prefix{netip.MustParsePrefix("187.7.31.238/32")},
		TrustedProxyCIDRs: []netip.Prefix{netip.MustParsePrefix("fc00::/7")},
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway-nodes/node-1/runtime", nil)
	r.RemoteAddr = "[fd12:0:8:0:1000::1]:8080"
	r.Header.Set("X-Railway-Edge", "ord1")
	r.Header.Set("X-Real-IP", "152.233.29.4")
	r.Header.Set("X-Forwarded-For", "187.7.31.238, 152.233.29.4")
	client, allowed := policy.Allows(r)
	if !allowed || client.String() != "187.7.31.238" {
		t.Fatalf("trusted Railway forwarding chain was not resolved: client=%s allowed=%v", client, allowed)
	}
}

func TestNetworkPolicyFailsClosedForMalformedRailwayForwardingChain(t *testing.T) {
	policy := NetworkPolicy{
		AllowedCIDRs:      []netip.Prefix{netip.MustParsePrefix("187.7.31.238/32")},
		TrustedProxyCIDRs: []netip.Prefix{netip.MustParsePrefix("fc00::/7")},
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/internal/gateway-nodes/node-1/runtime", nil)
	r.RemoteAddr = "[fd12:0:8:0:1000::1]:8080"
	r.Header.Set("X-Railway-Edge", "ord1")
	r.Header.Set("X-Forwarded-For", ", 187.7.31.238")
	client, allowed := policy.Allows(r)
	if allowed || client.IsValid() {
		t.Fatalf("malformed Railway forwarding chain was accepted: client=%s allowed=%v", client, allowed)
	}
}

func TestNetworkAdmissionLeavesHealthChecksAvailable(t *testing.T) {
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{NetworkPolicy: NetworkPolicy{AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}}).Handler()
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.RemoteAddr = "198.51.100.10:4567"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("health check blocked: %d %s", w.Code, w.Body.String())
	}
}

func TestNetworkAdmissionAllowsOnlyAuthenticatedMetaWebhookPublicBoundary(t *testing.T) {
	credentials, err := metacloud.ParseCredentialSet(`[{"key":"meta-ng","accessToken":"token-value-abcdefghijklmnopqrstuvwxyz","appSecret":"meta-app-secret-0123456789","verifyToken":"verify-token-012345"}]`)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
		NetworkPolicy:   NetworkPolicy{AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
		MetaCredentials: credentials,
	}).Handler()
	webhook := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/meta/meta-ng?hub.mode=subscribe&hub.verify_token=verify-token-012345&hub.challenge=1158201444", nil)
	webhook.RemoteAddr = "198.51.100.10:4567"
	webhookResponse := httptest.NewRecorder()
	handler.ServeHTTP(webhookResponse, webhook)
	if webhookResponse.Code != http.StatusOK || webhookResponse.Body.String() != "1158201444" {
		t.Fatalf("Meta webhook boundary blocked or unauthenticated: %d %s", webhookResponse.Code, webhookResponse.Body.String())
	}
	private := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	private.RemoteAddr = "198.51.100.10:4567"
	privateResponse := httptest.NewRecorder()
	handler.ServeHTTP(privateResponse, private)
	if privateResponse.Code != http.StatusForbidden {
		t.Fatalf("non-webhook route escaped network policy: %d %s", privateResponse.Code, privateResponse.Body.String())
	}
}
