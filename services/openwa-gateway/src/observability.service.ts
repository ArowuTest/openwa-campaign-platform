import { Injectable, LoggerService } from '@nestjs/common';
import { AsyncLocalStorage } from 'node:async_hooks';
import { randomBytes } from 'node:crypto';

type TraceContext = { traceId: string; spanId: string; flags: string };
type Counter = { labels: Record<string, string>; value: number };
type Gauge = { labels: Record<string, string>; value: number };
type Observation = { labels: Record<string, string>; count: number; sum: number };

@Injectable()
export class GatewayObservabilityService {
  private readonly startedAt = Date.now();
  private readonly traceStorage = new AsyncLocalStorage<TraceContext>();
  private readonly counters = new Map<string, Map<string, Counter>>();
  private readonly gauges = new Map<string, Map<string, Gauge>>();
  private readonly observations = new Map<string, Map<string, Observation>>();
  private readonly durationBuckets = [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10];
  private readonly durations = new Map<string, { labels: Record<string, string>; counts: number[]; count: number; sum: number }>();

  run<T>(parentHeader: string | undefined, operation: () => T): T {
    const parent = parseTraceparent(parentHeader);
    const trace: TraceContext = { traceId: parent?.traceId ?? randomBytes(16).toString('hex'), spanId: randomBytes(8).toString('hex'), flags: parent?.flags ?? '01' };
    return this.traceStorage.run(trace, operation);
  }

  traceparent(): string | undefined {
    const trace = this.traceStorage.getStore();
    return trace ? `00-${trace.traceId}-${trace.spanId}-${trace.flags}` : undefined;
  }

  traceFields(): Record<string, string> {
    const trace = this.traceStorage.getStore();
    return trace ? { trace_id: trace.traceId, span_id: trace.spanId } : {};
  }

  observeHttp(method: string, route: string, statusCode: number, durationSeconds: number): void {
    const labels = { service: 'openwa-gateway', method: bounded(method, 16), route: normaliseRoute(route), status_class: `${Math.floor(statusCode / 100)}xx` };
    this.increment('campaign_platform_http_requests_total', labels);
    const key = labelKey(labels);
    let histogram = this.durations.get(key);
    if (!histogram) {
      histogram = { labels, counts: this.durationBuckets.map(() => 0), count: 0, sum: 0 };
      this.durations.set(key, histogram);
    }
    this.durationBuckets.forEach((upper, index) => { if (durationSeconds <= upper) histogram!.counts[index] += 1; });
    histogram.count += 1;
    histogram.sum += durationSeconds;
  }

  increment(name: string, labels: Record<string, string> = {}): void {
    const safeLabels = sanitiseLabels(labels);
    const key = labelKey(safeLabels);
    let samples = this.counters.get(name);
    if (!samples) { samples = new Map(); this.counters.set(name, samples); }
    const sample = samples.get(key) ?? { labels: safeLabels, value: 0 };
    sample.value += 1;
    samples.set(key, sample);
  }

  gauge(name: string, value: number, labels: Record<string, string> = {}): void {
    if (!Number.isFinite(value)) return;
    const safeLabels = sanitiseLabels(labels);
    const key = labelKey(safeLabels);
    let samples = this.gauges.get(name);
    if (!samples) {
      samples = new Map();
      this.gauges.set(name, samples);
    }
    samples.set(key, { labels: safeLabels, value });
  }

  observe(name: string, value: number, labels: Record<string, string> = {}): void {
    if (!Number.isFinite(value) || value < 0) return;
    const safeLabels = sanitiseLabels(labels);
    const key = labelKey(safeLabels);
    let samples = this.observations.get(name);
    if (!samples) {
      samples = new Map();
      this.observations.set(name, samples);
    }
    const sample = samples.get(key) ?? { labels: safeLabels, count: 0, sum: 0 };
    sample.count += 1;
    sample.sum += value;
    samples.set(key, sample);
  }

  prometheus(): string {
    const lines = [
      '# HELP campaign_platform_process_uptime_seconds Process uptime in seconds.',
      '# TYPE campaign_platform_process_uptime_seconds gauge',
      `campaign_platform_process_uptime_seconds{service="openwa-gateway"} ${Math.floor((Date.now() - this.startedAt) / 1000)}`
    ];
    for (const [name, samples] of [...this.counters.entries()].sort(([left], [right]) => left.localeCompare(right))) {
      lines.push(`# HELP ${name} OpenWA gateway operational counter.`, `# TYPE ${name} counter`);
      for (const sample of [...samples.values()].sort((left, right) => labelKey(left.labels).localeCompare(labelKey(right.labels)))) lines.push(`${name}${renderLabels(sample.labels)} ${sample.value}`);
    }
    for (const [name, samples] of [...this.gauges.entries()].sort(([left], [right]) => left.localeCompare(right))) {
      lines.push(`# HELP ${name} OpenWA gateway operational gauge.`, `# TYPE ${name} gauge`);
      for (const sample of [...samples.values()].sort((left, right) => labelKey(left.labels).localeCompare(labelKey(right.labels)))) lines.push(`${name}${renderLabels(sample.labels)} ${sample.value}`);
    }
    for (const [name, samples] of [...this.observations.entries()].sort(([left], [right]) => left.localeCompare(right))) {
      lines.push(`# HELP ${name} OpenWA gateway operational observation.`, `# TYPE ${name} summary`);
      for (const sample of [...samples.values()].sort((left, right) => labelKey(left.labels).localeCompare(labelKey(right.labels)))) {
        lines.push(`${name}_sum${renderLabels(sample.labels)} ${sample.sum}`);
        lines.push(`${name}_count${renderLabels(sample.labels)} ${sample.count}`);
      }
    }
    lines.push('# HELP campaign_platform_http_request_duration_seconds HTTP request duration in seconds.', '# TYPE campaign_platform_http_request_duration_seconds histogram');
    for (const histogram of [...this.durations.values()].sort((left, right) => labelKey(left.labels).localeCompare(labelKey(right.labels)))) {
      this.durationBuckets.forEach((upper, index) => lines.push(`campaign_platform_http_request_duration_seconds_bucket${renderLabels({ ...histogram.labels, le: String(upper) })} ${histogram.counts[index]}`));
      lines.push(`campaign_platform_http_request_duration_seconds_bucket${renderLabels({ ...histogram.labels, le: '+Inf' })} ${histogram.count}`);
      lines.push(`campaign_platform_http_request_duration_seconds_sum${renderLabels(histogram.labels)} ${histogram.sum}`);
      lines.push(`campaign_platform_http_request_duration_seconds_count${renderLabels(histogram.labels)} ${histogram.count}`);
    }
    return `${lines.join('\n')}\n`;
  }
}

export class SanitisedJsonLogger implements LoggerService {
  log(message: unknown, ...optional: unknown[]) { this.write('info', message, optional); }
  error(message: unknown, ...optional: unknown[]) { this.write('error', message, optional); }
  warn(message: unknown, ...optional: unknown[]) { this.write('warn', message, optional); }
  debug(message: unknown, ...optional: unknown[]) { this.write('debug', message, optional); }
  verbose(message: unknown, ...optional: unknown[]) { this.write('debug', message, optional); }
  fatal(message: unknown, ...optional: unknown[]) { this.write('error', message, optional); }
  private write(level: string, message: unknown, optional: unknown[]) {
    const record = { time: new Date().toISOString(), level, service: 'openwa-gateway', message: sanitise(String(message)), detail: optional.map(value => sanitise(String(value))).slice(0, 10) };
    process.stdout.write(`${JSON.stringify(record)}\n`);
  }
}

function parseTraceparent(value: string | undefined): TraceContext | undefined {
  const match = /^00-([a-f0-9]{32})-([a-f0-9]{16})-([a-f0-9]{2})$/i.exec(value?.trim() ?? '');
  if (!match || /^0+$/.test(match[1]) || /^0+$/.test(match[2])) return undefined;
  return { traceId: match[1].toLowerCase(), spanId: match[2].toLowerCase(), flags: match[3].toLowerCase() };
}
function normaliseRoute(value: string): string {
  return bounded(value.replace(/[0-9a-f]{8}-[0-9a-f-]{27,}/gi, ':id').replace(/\b[A-Za-z0-9:_-]{24,}\b/g, ':id'), 200);
}
function sanitiseLabels(labels: Record<string, string>): Record<string, string> {
  const safeLabels: Record<string, string> = { service: 'openwa-gateway' };
  for (const [rawKey, rawValue] of Object.entries(labels).sort(([left], [right]) => left.localeCompare(right))) {
    const key = normaliseLabelName(rawKey);
    if (!(key in safeLabels)) safeLabels[key] = bounded(rawValue, 160);
  }
  return safeLabels;
}
function labelKey(labels: Record<string, string>): string { return Object.keys(labels).sort().map(key => `${key}=${labels[key]}\0`).join(''); }
function renderLabels(labels: Record<string, string>): string { return `{${Object.keys(labels).sort().map(key => `${key}="${labels[key].replace(/\\/g, '\\\\').replace(/"/g, '\\"').replace(/\n/g, '\\n')}"`).join(',')}}`; }
function bounded(value: string, maximum: number): string { return value.replace(/[\u0000-\u001f\u007f]/g, '').trim().slice(0, maximum); }
function normaliseLabelName(value: string): string {
  const normalised = bounded(value, 64).replace(/[^a-zA-Z0-9_]/g, '_');
  if (!normalised) return '_label';
  return /^[a-zA-Z_]/.test(normalised) ? normalised : `_${normalised}`;
}
function sanitise(value: string): string {
  return value
    .replace(/([a-z][a-z0-9+.-]*:\/\/)[^/@\s:]+:[^/@\s]+@/gi, '$1[REDACTED]@')
    .replace(/\+[1-9][0-9]{7,14}/g, '[REDACTED_MSISDN]')
    .replace(/(password|secret|token|authorization|cookie|database_url|dsn|qr(?:_?payload)?|session(?:_?token)?|message(?:_?(?:body|content)))\s*[=:]\s*[^\s,}]+/gi, '$1=[REDACTED]')
    .slice(0, 4096);
}
