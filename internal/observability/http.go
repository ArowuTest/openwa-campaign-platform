package observability

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	count, err := w.ResponseWriter.Write(payload)
	w.bytes += count
	return count, err
}

func HTTPMiddleware(registry *Registry, logger *slog.Logger, next http.Handler) http.Handler {
	if registry == nil {
		registry = NewRegistry("unknown")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		started := time.Now()
		parent, _ := ParseTraceParent(request.Header.Get("traceparent"))
		trace := NewTrace(parent)
		request = request.WithContext(WithTrace(request.Context(), trace))
		w.Header().Set("Traceparent", trace.TraceParent())
		wrapped := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, request)
		if wrapped.status == 0 {
			wrapped.status = http.StatusOK
		}
		route := request.Pattern
		if route == "" {
			route = "unmatched"
		}
		route = boundedRoute(route)
		statusClass := strconv.Itoa(wrapped.status/100) + "xx"
		labels := Labels{"method": request.Method, "route": route, "status_class": statusClass}
		registry.Inc("campaign_platform_http_requests_total", "Completed HTTP requests.", labels)
		registry.Observe("campaign_platform_http_request_duration_seconds", "HTTP request duration in seconds.", labels, time.Since(started).Seconds())
		registry.Add("campaign_platform_http_response_bytes_total", "HTTP response bytes written.", labels, float64(wrapped.bytes))
		if logger != nil {
			LoggerFromContext(request.Context(), logger).Info("http request completed", "method", request.Method, "route", route, "status", wrapped.status, "duration_ms", time.Since(started).Milliseconds(), "response_bytes", wrapped.bytes)
		}
	})
}

func InjectTrace(request *http.Request) {
	if request == nil || request.Header == nil {
		return
	}
	if trace, ok := TraceFromContext(request.Context()); ok {
		child := NewTrace(trace)
		request.Header.Set("Traceparent", child.TraceParent())
	}
}

func boundedRoute(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 200 {
		value = value[:200]
	}
	return sanitiseMetricLabel(value)
}
