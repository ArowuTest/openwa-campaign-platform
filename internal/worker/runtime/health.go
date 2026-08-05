package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

type Health struct {
	Service   string
	StartedAt time.Time
	DB        *sql.DB
	active    func() int64
	ready     atomic.Bool
}

func NewHealth(service string, db *sql.DB, active func() int64) *Health {
	return &Health{Service: service, StartedAt: time.Now().UTC(), DB: db, active: active}
}

func (h *Health) SetReady(value bool) { h.ready.Store(value) }

func (h *Health) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeHealth(w, http.StatusOK, map[string]any{"status": "ok", "service": h.Service, "uptimeSeconds": int64(time.Since(h.StartedAt).Seconds())})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if !h.ready.Load() || h.DB == nil {
			writeHealth(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "service": h.Service})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := h.DB.PingContext(ctx); err != nil {
			writeHealth(w, http.StatusServiceUnavailable, map[string]any{"status": "database_unavailable", "service": h.Service})
			return
		}
		active := int64(0)
		if h.active != nil {
			active = h.active()
		}
		writeHealth(w, http.StatusOK, map[string]any{"status": "ready", "service": h.Service, "active": active})
	})
	return mux
}

func writeHealth(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
