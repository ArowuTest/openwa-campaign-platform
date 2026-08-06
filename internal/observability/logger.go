package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

var (
	credentialURL = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s:]+:[^/@\s]+@`)
	phoneNumber   = regexp.MustCompile(`\+[1-9][0-9]{7,14}`)
)

func NewLogger(output io.Writer, service, environment string) *slog.Logger {
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{ReplaceAttr: redactAttribute})
	return slog.New(handler).With("service", sanitiseMetricLabel(service), "environment", sanitiseMetricLabel(environment))
}

func redactAttribute(groups []string, attribute slog.Attr) slog.Attr {
	if attribute.Key == slog.TimeKey || attribute.Key == slog.LevelKey || attribute.Key == slog.MessageKey || attribute.Key == slog.SourceKey {
		return attribute
	}
	key := strings.ToLower(strings.Join(append(groups, attribute.Key), "."))
	if sensitiveKey(key) {
		return slog.String(attribute.Key, "[REDACTED]")
	}
	attribute.Value = sanitiseValue(attribute.Value)
	return attribute
}

func sanitiseValue(value slog.Value) slog.Value {
	value = value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		return slog.StringValue(sanitiseLogString(value.String()))
	case slog.KindAny:
		if err, ok := value.Any().(error); ok {
			return slog.StringValue(sanitiseLogString(err.Error()))
		}
		return slog.StringValue(sanitiseLogString(fmt.Sprint(value.Any())))
	case slog.KindGroup:
		group := value.Group()
		for index := range group {
			group[index] = redactAttribute(nil, group[index])
		}
		return slog.GroupValue(group...)
	default:
		return value
	}
}

func sanitiseLogString(value string) string {
	value = credentialURL.ReplaceAllString(value, `${1}[REDACTED]@`)
	value = phoneNumber.ReplaceAllString(value, "[REDACTED_MSISDN]")
	if len(value) > 4096 {
		value = value[:4096] + "...[TRUNCATED]"
	}
	return value
}

func sensitiveKey(key string) bool {
	for _, fragment := range []string{"password", "secret", "token", "authorization", "cookie", "msisdn", "recipient", "message.body", "message.content", "rawbody", "private_key", "encryption_key", "database_url", "dsn"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}

func LoggerFromContext(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if trace, ok := TraceFromContext(ctx); ok {
		return fallback.With("trace_id", trace.TraceID, "span_id", trace.SpanID)
	}
	return fallback
}
