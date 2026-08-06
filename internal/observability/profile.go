package observability

import (
	"crypto/subtle"
	"net/http"
	"net/http/pprof"
	"strings"
)

func RegisterProfiling(mux *http.ServeMux, token string) {
	if mux == nil || strings.TrimSpace(token) == "" {
		return
	}
	protect := func(handler http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if len(provided) != len(token) || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="profiling"`)
				http.Error(w, "profiling authorisation required", http.StatusUnauthorized)
				return
			}
			handler(w, r)
		}
	}
	mux.HandleFunc("GET /debug/pprof/", protect(pprof.Index))
	mux.HandleFunc("GET /debug/pprof/cmdline", protect(pprof.Cmdline))
	mux.HandleFunc("GET /debug/pprof/profile", protect(pprof.Profile))
	mux.HandleFunc("GET /debug/pprof/symbol", protect(pprof.Symbol))
	mux.HandleFunc("POST /debug/pprof/symbol", protect(pprof.Symbol))
	mux.HandleFunc("GET /debug/pprof/trace", protect(pprof.Trace))
}
