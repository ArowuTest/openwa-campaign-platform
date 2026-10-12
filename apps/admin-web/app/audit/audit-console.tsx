'use client';

import { FormEvent, useCallback, useEffect, useState } from 'react';

import { APIError, apiRequest } from '../../lib/api';

type AuditEvent = {
  id: string;
  sequence: number;
  actorType: string;
  actorId: string;
  action: string;
  objectType: string;
  objectId: string;
  organisationId?: string;
  outcome?: string;
  sensitivity?: string;
  before?: unknown;
  after?: unknown;
  reasonCode?: string;
  reason?: string;
  ipAddress?: string;
  device?: string;
  correlationId: string;
  occurredAt: string;
  previousHash?: string;
  hash?: string;
};

type AuditPage = {
  items: AuditEvent[];
  count: number;
  hasMore: boolean;
  nextCursor?: string | number;
};

type AuditFilters = {
  actorId: string;
  action: string;
  objectType: string;
  objectId: string;
  organisationId: string;
  outcome: string;
  sensitivity: string;
  correlationId: string;
  from: string;
  to: string;
};

const emptyFilters: AuditFilters = {
  actorId: '',
  action: '',
  objectType: '',
  objectId: '',
  organisationId: '',
  outcome: '',
  sensitivity: '',
  correlationId: '',
  from: '',
  to: ''
};

function safeJSON(value: unknown): string {
  if (value === undefined || value === null) return '—';
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return 'Unavailable';
  }
}

function readPage(value: AuditPage): AuditPage {
  if (!value || !Array.isArray(value.items) || !Number.isInteger(value.count) || value.count < 0 || typeof value.hasMore !== 'boolean') {
    throw new Error('The audit response was malformed.');
  }
  const items = value.items.filter((item) => item && typeof item.id === 'string' && Number.isSafeInteger(item.sequence) && typeof item.occurredAt === 'string');
  if (items.length !== value.items.length) throw new Error('The audit response contained an invalid event.');
  if (value.hasMore && value.nextCursor === undefined) throw new Error('The audit response omitted its continuation cursor.');
  return value;
}

function makeQuery(filters: AuditFilters, afterSequence?: string): string {
  const query = new URLSearchParams({ limit: '100' });
  const values: Record<string, string> = {
    actorId: filters.actorId,
    action: filters.action,
    objectType: filters.objectType,
    objectId: filters.objectId,
    organisationId: filters.organisationId,
    outcome: filters.outcome,
    sensitivity: filters.sensitivity,
    correlationId: filters.correlationId
  };
  for (const [key, value] of Object.entries(values)) {
    if (value.trim()) query.set(key, value.trim());
  }
  for (const [key, value] of [['from', filters.from], ['to', filters.to]] as const) {
    if (value) {
      const parsed = new Date(value);
      if (Number.isNaN(parsed.valueOf())) throw new Error('Audit date filters are invalid.');
      query.set(key, parsed.toISOString());
    }
  }
  if (afterSequence) query.set('afterSequence', afterSequence);
  return '?' + query.toString();
}

export function AuditConsole() {
  const [filters, setFilters] = useState<AuditFilters>(emptyFilters);
  const [applied, setApplied] = useState<AuditFilters>(emptyFilters);
  const [items, setItems] = useState<AuditEvent[]>([]);
  const [nextCursor, setNextCursor] = useState('');
  const [hasMore, setHasMore] = useState(false);
  const [selected, setSelected] = useState<AuditEvent>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async (queryFilters: AuditFilters, append = false, cursor = '') => {
    setLoading(true);
    setError('');
    try {
      const page = readPage(await apiRequest<AuditPage>('/v1/operations/audit-events' + makeQuery(queryFilters, cursor)));
      setItems((current) => append ? [...current, ...page.items.filter((item) => !current.some((existing) => existing.id === item.id))] : page.items);
      setNextCursor(page.nextCursor === undefined ? '' : String(page.nextCursor));
      setHasMore(page.hasMore);
      if (!append) setSelected(page.items[0]);
    } catch (cause) {
      setError(cause instanceof APIError && cause.status === 403 ? 'Audit access is restricted to compliance and audit readers.' : cause instanceof Error ? cause.message : 'Audit evidence could not be loaded.');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    // The initial load synchronizes the authoritative audit inventory into local state.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load(emptyFilters);
  }, [load]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setApplied({ ...filters });
    void load(filters);
  }

  return (
    <div className="workspace-stack">
      <section className="card">
        <div className="section-heading">
          <div>
            <h2>Immutable audit log</h2>
            <p className="muted">Searches are recorded as high-sensitivity audit events. This read-only view shows the business audit ledger; authentication-event integration is tracked as the next IAM-011 backend step.</p>
          </div>
          <span className="pill">{items.length} loaded</span>
        </div>
        <form className="filter-layout" onSubmit={submit}>
          <label>Actor ID<input value={filters.actorId} onChange={(event) => setFilters({ ...filters, actorId: event.target.value })} /></label>
          <label>Action<input value={filters.action} onChange={(event) => setFilters({ ...filters, action: event.target.value })} /></label>
          <label>Object type<input value={filters.objectType} onChange={(event) => setFilters({ ...filters, objectType: event.target.value })} /></label>
          <label>Object ID<input value={filters.objectId} onChange={(event) => setFilters({ ...filters, objectId: event.target.value })} /></label>
          <label>Organisation ID<input value={filters.organisationId} onChange={(event) => setFilters({ ...filters, organisationId: event.target.value })} /></label>
          <label>Correlation ID<input value={filters.correlationId} onChange={(event) => setFilters({ ...filters, correlationId: event.target.value })} /></label>
          <label>Outcome<input value={filters.outcome} onChange={(event) => setFilters({ ...filters, outcome: event.target.value })} /></label>
          <label>Sensitivity<input value={filters.sensitivity} onChange={(event) => setFilters({ ...filters, sensitivity: event.target.value })} /></label>
          <label>From<input type="datetime-local" value={filters.from} onChange={(event) => setFilters({ ...filters, from: event.target.value })} /></label>
          <label>To<input type="datetime-local" value={filters.to} onChange={(event) => setFilters({ ...filters, to: event.target.value })} /></label>
          <button className="primary" type="submit" disabled={loading}>Search audit</button>
        </form>
        {error ? <div className="alert alert-danger" role="alert">{error}</div> : null}
        {loading ? <div className="alert alert-information" role="status">Loading immutable audit evidence…</div> : null}
      </section>

      <section className="card">
        <h2>Audit events</h2>
        {items.length === 0 && !loading ? <div className="empty-state"><strong>No audit events</strong><p>No events match the applied filters.</p></div> : (
          <div className="table-wrap">
            <table>
              <caption className="sr-only">Immutable audit events</caption>
              <thead><tr><th>Event</th><th>Actor</th><th>Object</th><th>Outcome</th><th>Occurred</th></tr></thead>
              <tbody>
                {items.map((item) => (
                  <tr key={item.id} aria-selected={selected?.id === item.id}>
                    <td><button className="link-button" type="button" onClick={() => setSelected(item)}>{item.action}<small>Sequence {item.sequence}</small></button></td>
                    <td>{item.actorId || item.actorType}<small>{item.actorType}</small></td>
                    <td>{item.objectType}<small>{item.objectId}</small></td>
                    <td>{item.outcome || '—'}</td>
                    <td>{new Date(item.occurredAt).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {hasMore ? <button className="secondary" type="button" disabled={loading || !nextCursor} onClick={() => void load(applied, true, nextCursor)}>Load more audit events</button> : null}
      </section>

      {selected ? <section className="card" aria-live="polite">
        <h2>Selected audit evidence</h2>
        <dl className="definition-list">
          <div><dt>Action</dt><dd>{selected.action}</dd></div>
          <div><dt>Actor</dt><dd>{selected.actorId || selected.actorType}</dd></div>
          <div><dt>Correlation</dt><dd>{selected.correlationId}</dd></div>
          <div><dt>Reason</dt><dd>{selected.reason || selected.reasonCode || '—'}</dd></div>
          <div><dt>Occurred</dt><dd>{new Date(selected.occurredAt).toLocaleString()}</dd></div>
          <div><dt>Chain hash</dt><dd className="mono-small">{selected.hash || '—'}</dd></div>
        </dl>
        <details><summary>Before snapshot</summary><pre>{safeJSON(selected.before)}</pre></details>
        <details><summary>After snapshot</summary><pre>{safeJSON(selected.after)}</pre></details>
      </section> : null}
    </div>
  );
}
