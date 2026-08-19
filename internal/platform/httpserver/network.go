package httpserver

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"campaign-platform/internal/shared/httpx"
)

// NetworkPolicy is an optional deployment boundary. Forwarding headers are
// considered only when the directly connected peer is in TrustedProxyCIDRs.
// This prevents callers from spoofing an allowed source through X-Forwarded-For.
type NetworkPolicy struct {
	AllowedCIDRs      []netip.Prefix
	TrustedProxyCIDRs []netip.Prefix
}

func (p NetworkPolicy) Enabled() bool { return len(p.AllowedCIDRs) > 0 }

func (p NetworkPolicy) ClientIP(r *http.Request) (netip.Addr, bool) {
	peer, ok := remoteAddress(r.RemoteAddr)
	if !ok {
		return netip.Addr{}, false
	}
	if !containsAddress(p.TrustedProxyCIDRs, peer) {
		return peer, true
	}

	// Walk from the nearest hop toward the original client. Trusted proxy hops
	// are skipped; the first non-trusted address is the effective client.
	values := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(values) - 1; i >= 0; i-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(values[i]))
		if err != nil {
			continue
		}
		candidate = candidate.Unmap()
		if !containsAddress(p.TrustedProxyCIDRs, candidate) {
			return candidate, true
		}
	}
	return peer, true
}

func (p NetworkPolicy) Allows(r *http.Request) (netip.Addr, bool) {
	client, ok := p.ClientIP(r)
	if !ok {
		return netip.Addr{}, false
	}
	return client, containsAddress(p.AllowedCIDRs, client)
}

func (s *Server) networkAdmission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.deps.NetworkPolicy.Enabled() || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || strings.HasPrefix(r.URL.Path, "/api/v1/webhooks/meta/") {
			next.ServeHTTP(w, r)
			return
		}
		client, allowed := s.deps.NetworkPolicy.Allows(r)
		if allowed {
			next.ServeHTTP(w, r)
			return
		}
		if s.deps.Identity != nil {
			s.deps.Identity.RecordNetworkDenied(r.Context(), client.String(), authenticationAttempt(r, ""))
		}
		httpx.WriteError(w, r, http.StatusForbidden, "NETWORK_ACCESS_DENIED", "Access from this network is not permitted.", nil)
	})
}

func remoteAddress(value string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		host = strings.TrimSpace(value)
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func containsAddress(prefixes []netip.Prefix, address netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
