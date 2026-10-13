package libpqdriver

import (
	"context"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func withConnectTimeout(ctx context.Context, dsn string) string {
	timeout := 10 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	seconds := int(math.Ceil(timeout.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	if parsed, err := url.Parse(dsn); err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		query := parsed.Query()
		if strings.TrimSpace(query.Get("connect_timeout")) != "" {
			return dsn
		}
		// libpq uses URI decoding, where a literal + remains a plus.
		// Re-encoding url.Values would replace %20 spaces with + and
		// corrupt options. Preserve every existing query byte and append
		// only the generated numeric timeout parameter.
		if parsed.RawQuery != "" && !strings.HasSuffix(parsed.RawQuery, "&") {
			parsed.RawQuery += "&"
		}
		parsed.RawQuery += "connect_timeout=" + strconv.Itoa(seconds)
		return parsed.String()
	}
	for _, field := range strings.Fields(dsn) {
		key, _, ok := strings.Cut(field, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "connect_timeout") {
			return dsn
		}
	}
	return dsn + " connect_timeout=" + strconv.Itoa(seconds)
}
