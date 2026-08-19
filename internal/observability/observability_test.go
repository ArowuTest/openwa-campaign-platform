package observability

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsHandlerUsesBoundedControlledLabels(t *testing.T) {
	registry := NewRegistry("control-api")
	handler := HTTPMiddleware(registry, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/contacts/2d89f9f7", nil)
	request.Pattern = "POST /api/v1/contacts/{id}"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	metrics := httptest.NewRecorder()
	registry.Handler(nil).ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metrics.Body.String()
	if !strings.Contains(body, `route="POST /api/v1/contacts/{id}"`) || strings.Contains(body, "2d89f9f7") {
		t.Fatalf("unexpected metrics: %s", body)
	}
}

func TestTraceParentPropagation(t *testing.T) {
	parent, ok := ParseTraceParent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if !ok {
		t.Fatal("valid traceparent rejected")
	}
	child := NewTrace(parent)
	if child.TraceID != parent.TraceID || child.SpanID == parent.SpanID {
		t.Fatalf("unexpected child trace: %+v", child)
	}
	ctx := WithTrace(context.Background(), child)
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid", nil)
	InjectTrace(request)
	if request.Header.Get("Traceparent") == "" {
		t.Fatal("traceparent not injected")
	}
}

func TestLoggerRedactsSecretsCredentialsAndMSISDN(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, "service", "test")
	logger.Error("failed", "DATABASE_URL", "postgres://user:password@example/db", "recipient_msisdn", "+2348012345678", "email", "bootstrap-admin@example.invalid", "error", slog.StringValue("postgres://u:p@host/db"))
	text := output.String()
	if strings.Contains(text, "password") || strings.Contains(text, "+2348012345678") || strings.Contains(text, "bootstrap-admin@example.invalid") || strings.Contains(text, "u:p") {
		t.Fatalf("sensitive log content leaked: %s", text)
	}
}

func TestProfilingRequiresBearerToken(t *testing.T) {
	mux := http.NewServeMux()
	RegisterProfiling(mux, "01234567890123456789012345678901")
	unauthorised := httptest.NewRecorder()
	mux.ServeHTTP(unauthorised, httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil))
	if unauthorised.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", unauthorised.Code)
	}
}

func TestMetricLabelNamesArePrometheusSafeAndDeterministic(t *testing.T) {
	registry := NewRegistry("metrics")
	registry.Inc("test_metric_total", "test", Labels{
		"9code":   "first",
		"bad-key": "kept",
		"bad key": "discarded collision",
	})
	recorder := httptest.NewRecorder()
	registry.Handler(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `_9code="first"`) || !strings.Contains(body, `bad_key="discarded collision"`) {
		t.Fatalf("normalised labels missing: %s", body)
	}
	if strings.Contains(body, "bad-key=") || strings.Contains(body, "bad key=") || strings.Count(body, "bad_key=") != 1 {
		t.Fatalf("invalid or duplicate labels emitted: %s", body)
	}
}
