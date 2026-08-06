package observability

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Labels map[string]string

type metricSample struct {
	labels Labels
	value  float64
}

type histogramSample struct {
	labels  Labels
	buckets []float64
	counts  []uint64
	count   uint64
	sum     float64
}

type Registry struct {
	service   string
	startedAt time.Time
	mu        sync.RWMutex
	help      map[string]string
	types     map[string]string
	counters  map[string]map[string]*metricSample
	gauges    map[string]map[string]*metricSample
	hist      map[string]map[string]*histogramSample
}

func NewRegistry(service string) *Registry {
	service = sanitiseMetricLabel(service)
	if service == "" {
		service = "unknown"
	}
	return &Registry{
		service: service, startedAt: time.Now().UTC(),
		help: make(map[string]string), types: make(map[string]string),
		counters: make(map[string]map[string]*metricSample),
		gauges:   make(map[string]map[string]*metricSample), hist: make(map[string]map[string]*histogramSample),
	}
}

func (r *Registry) Service() string { return r.service }

func (r *Registry) Inc(name, help string, labels Labels) { r.Add(name, help, labels, 1) }

func (r *Registry) Add(name, help string, labels Labels, value float64) {
	name = normaliseMetricName(name)
	if name == "" || value < 0 {
		return
	}
	labels = r.withService(labels)
	key := labelKey(labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.help[name], r.types[name] = help, "counter"
	if r.counters[name] == nil {
		r.counters[name] = make(map[string]*metricSample)
	}
	if r.counters[name][key] == nil {
		r.counters[name][key] = &metricSample{labels: cloneLabels(labels)}
	}
	r.counters[name][key].value += value
}

func (r *Registry) Set(name, help string, labels Labels, value float64) {
	name = normaliseMetricName(name)
	if name == "" {
		return
	}
	labels = r.withService(labels)
	key := labelKey(labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.help[name], r.types[name] = help, "gauge"
	if r.gauges[name] == nil {
		r.gauges[name] = make(map[string]*metricSample)
	}
	r.gauges[name][key] = &metricSample{labels: cloneLabels(labels), value: value}
}

func (r *Registry) Observe(name, help string, labels Labels, value float64, buckets ...float64) {
	name = normaliseMetricName(name)
	if name == "" || value < 0 {
		return
	}
	if len(buckets) == 0 {
		buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	}
	buckets = append([]float64(nil), buckets...)
	sort.Float64s(buckets)
	labels = r.withService(labels)
	key := labelKey(labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.help[name], r.types[name] = help, "histogram"
	if r.hist[name] == nil {
		r.hist[name] = make(map[string]*histogramSample)
	}
	sample := r.hist[name][key]
	if sample == nil {
		sample = &histogramSample{labels: cloneLabels(labels), buckets: buckets, counts: make([]uint64, len(buckets))}
		r.hist[name][key] = sample
	}
	for index, upper := range sample.buckets {
		if value <= upper {
			sample.counts[index]++
		}
	}
	sample.count++
	sample.sum += value
}

func (r *Registry) Handler(db *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		r.writePrometheus(w, db)
	})
}

func (r *Registry) writePrometheus(w io.Writer, db *sql.DB) {
	runtimeStats := runtime.MemStats{}
	runtime.ReadMemStats(&runtimeStats)
	_, _ = fmt.Fprintf(w, "# HELP campaign_platform_process_uptime_seconds Process uptime in seconds.\n# TYPE campaign_platform_process_uptime_seconds gauge\ncampaign_platform_process_uptime_seconds{service=%s} %.0f\n", quoteLabel(r.service), time.Since(r.startedAt).Seconds())
	_, _ = fmt.Fprintf(w, "# HELP campaign_platform_go_goroutines Current goroutine count.\n# TYPE campaign_platform_go_goroutines gauge\ncampaign_platform_go_goroutines{service=%s} %d\n", quoteLabel(r.service), runtime.NumGoroutine())
	_, _ = fmt.Fprintf(w, "# HELP campaign_platform_go_heap_alloc_bytes Current heap allocation.\n# TYPE campaign_platform_go_heap_alloc_bytes gauge\ncampaign_platform_go_heap_alloc_bytes{service=%s} %d\n", quoteLabel(r.service), runtimeStats.HeapAlloc)
	if db != nil {
		stats := db.Stats()
		writeGauge(w, "campaign_platform_db_open_connections", "Open database connections.", r.service, float64(stats.OpenConnections))
		writeGauge(w, "campaign_platform_db_in_use_connections", "Database connections currently in use.", r.service, float64(stats.InUse))
		writeGauge(w, "campaign_platform_db_idle_connections", "Idle database connections.", r.service, float64(stats.Idle))
		writeGauge(w, "campaign_platform_db_wait_count_total", "Database pool wait count.", r.service, float64(stats.WaitCount))
		writeGauge(w, "campaign_platform_db_wait_duration_seconds_total", "Database pool wait duration.", r.service, stats.WaitDuration.Seconds())
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.types))
	for name := range r.types {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, sanitiseHelp(r.help[name]), name, r.types[name])
		switch r.types[name] {
		case "counter":
			writeSamples(w, name, r.counters[name])
		case "gauge":
			writeSamples(w, name, r.gauges[name])
		case "histogram":
			keys := sortedKeys(r.hist[name])
			for _, key := range keys {
				sample := r.hist[name][key]
				for i, upper := range sample.buckets {
					labels := cloneLabels(sample.labels)
					labels["le"] = strconv.FormatFloat(upper, 'g', -1, 64)
					_, _ = fmt.Fprintf(w, "%s_bucket%s %d\n", name, renderLabels(labels), sample.counts[i])
				}
				labels := cloneLabels(sample.labels)
				labels["le"] = "+Inf"
				_, _ = fmt.Fprintf(w, "%s_bucket%s %d\n", name, renderLabels(labels), sample.count)
				_, _ = fmt.Fprintf(w, "%s_sum%s %s\n", name, renderLabels(sample.labels), strconv.FormatFloat(sample.sum, 'g', -1, 64))
				_, _ = fmt.Fprintf(w, "%s_count%s %d\n", name, renderLabels(sample.labels), sample.count)
			}
		}
	}
}

func writeGauge(w io.Writer, name, help, service string, value float64) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s{service=%s} %s\n", name, help, name, name, quoteLabel(service), strconv.FormatFloat(value, 'g', -1, 64))
}

func writeSamples(w io.Writer, name string, samples map[string]*metricSample) {
	for _, key := range sortedKeys(samples) {
		sample := samples[key]
		_, _ = fmt.Fprintf(w, "%s%s %s\n", name, renderLabels(sample.labels), strconv.FormatFloat(sample.value, 'g', -1, 64))
	}
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (r *Registry) withService(labels Labels) Labels {
	out := cloneLabels(labels)
	out["service"] = r.service
	return out
}

func cloneLabels(labels Labels) Labels {
	out := make(Labels, len(labels)+1)
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, original := range keys {
		key := normaliseLabelName(original)
		if key == "" {
			continue
		}
		// A pair of caller-supplied keys can normalise to the same Prometheus
		// name. Keep the first deterministic key rather than emitting duplicate
		// labels, which would make the exposition invalid.
		if _, exists := out[key]; exists {
			continue
		}
		out[key] = sanitiseMetricLabel(labels[original])
	}
	return out
}

func labelKey(labels Labels) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(labels[key])
		builder.WriteByte('\x00')
	}
	return builder.String()
}

func renderLabels(labels Labels) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+quoteLabel(labels[key]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func quoteLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return "\"" + value + "\""
}

func normaliseMetricName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for index, current := range value {
		valid := current == '_' || current == ':' || current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z' || index > 0 && current >= '0' && current <= '9'
		if valid {
			builder.WriteRune(current)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func normaliseLabelName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for index, current := range value {
		first := index == 0
		valid := current == '_' || current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z' || !first && current >= '0' && current <= '9'
		if valid {
			builder.WriteRune(current)
			continue
		}
		if first && current >= '0' && current <= '9' {
			builder.WriteByte('_')
			builder.WriteRune(current)
		} else {
			builder.WriteByte('_')
		}
	}
	name := builder.String()
	if len(name) > 128 {
		name = name[:128]
	}
	return name
}

func sanitiseMetricLabel(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 160 {
		value = value[:160]
	}
	return strings.Map(func(current rune) rune {
		if current < 0x20 || current == 0x7f {
			return -1
		}
		return current
	}, value)
}

func sanitiseHelp(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	if value == "" {
		return "Campaign platform metric."
	}
	return value
}
