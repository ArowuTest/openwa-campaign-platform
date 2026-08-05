package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthSeparatesLivenessFromReadiness(t *testing.T) {
	health := NewHealth("worker", nil, nil)
	server := health.Handler()
	live := httptest.NewRecorder()
	server.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if live.Code != http.StatusOK {
		t.Fatalf("liveness=%d", live.Code)
	}
	ready := httptest.NewRecorder()
	server.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness=%d", ready.Code)
	}
}
