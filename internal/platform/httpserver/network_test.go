package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestNetworkPolicyDoesNotTrustSpoofedForwardingHeader(t *testing.T) {
	policy := NetworkPolicy{AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, TrustedProxyCIDRs: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	r.RemoteAddr = "198.51.100.10:4567"
	r.Header.Set("X-Forwarded-For", "10.1.2.3")
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
