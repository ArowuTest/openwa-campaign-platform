package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

type traceContextKey struct{}

type TraceContext struct {
	TraceID string
	SpanID  string
	Flags   string
}

func ParseTraceParent(value string) (TraceContext, bool) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return TraceContext{}, false
	}
	if !validHex(parts[1]) || !validHex(parts[2]) || !validHex(parts[3]) || allZero(parts[1]) || allZero(parts[2]) {
		return TraceContext{}, false
	}
	return TraceContext{TraceID: strings.ToLower(parts[1]), SpanID: strings.ToLower(parts[2]), Flags: strings.ToLower(parts[3])}, true
}

func NewTrace(parent TraceContext) TraceContext {
	traceID := parent.TraceID
	flags := parent.Flags
	if traceID == "" {
		traceID = randomHex(16)
		flags = "01"
	}
	return TraceContext{TraceID: traceID, SpanID: randomHex(8), Flags: flags}
}

func (t TraceContext) TraceParent() string {
	if t.TraceID == "" || t.SpanID == "" {
		return ""
	}
	flags := t.Flags
	if flags == "" {
		flags = "01"
	}
	return "00-" + t.TraceID + "-" + t.SpanID + "-" + flags
}

func WithTrace(ctx context.Context, trace TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, trace)
}

func TraceFromContext(ctx context.Context) (TraceContext, bool) {
	trace, ok := ctx.Value(traceContextKey{}).(TraceContext)
	return trace, ok && trace.TraceID != "" && trace.SpanID != ""
}

func randomHex(bytesCount int) string {
	value := make([]byte, bytesCount)
	if _, err := rand.Read(value); err != nil {
		panic("cryptographic trace identifier generation failed: " + err.Error())
	}
	return hex.EncodeToString(value)
}

func validHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func allZero(value string) bool { return strings.Trim(value, "0") == "" }
