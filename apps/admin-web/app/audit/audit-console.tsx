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

type AuthenticationEvent = {
  source: 'authentication';
  id: string;
  actorId?: string;
  eventType: string;
  correlationId: string;
  occurredAt: string;
};

type AuthenticationPage = {
  items: AuthenticationEvent[];
  count: number;
  hasMore: boolean;
  nextCursor?: string;
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

function readAuthenticationPage(value: AuthenticationPage): AuthenticationPage {
  if (!value || !Array.isArray(value.items) || !Number.isInteger(value.count) || value.count < 0 || typeof value.hasMore !== 'boolean') {
    throw new Error('The authentication-event response was malformed.');
  }
  const items = value.items.filter((item) => item && item.source === 'authentication' && typeof item.id === 'string' && typeof item.eventType === 'string' && typeof item.correlationId === 'string' && typeof item.occurredAt === 'string');
  if (items.length !== value.items.length) throw new Error('The authentication-event response contained an invalid event.');
  if (value.hasMore && !value.nextCursor) throw new Error('The authentication-event response omitted its continuation cursor.');
  return value;
}

function makeAuthenticationQuery(filters: AuditFilters, cursor?: string): string {
  const query = new URLSearchParams({ limit: '100' });
  const actor = filters.actorId.trim();
  if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(actor)) query.set('userId', actor);
  if (filters.correlationId.trim()) query.set('correlationId', filters.correlationId.trim());
  for (const [key, value] of [['from', filters.from], ['to', filters.to]] as const) {
    if (value) {
      const parsed = new Date(value);
      if (Number.isNaN(parsed.valueOf())) throw new Error('Audit date filters are invalid.');
      query.set(key, parsed.toISOString());
    }
  }
  if (cursor) query.set('cursor', cursor);
  return '?' + query.toString();
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
  const [authenticationItems, setAuthenticationItems] = useState<AuthenticationEvent[]>([]);
  const [authenticationNextCursor, setAuthenticationNextCursor] = useState('');
  const [authenticationHasMore, setAuthenticationHasMore] = useState(false);
  const [selected, setSelected] = useState<AuditEvent>();
  const [loading, setLoading] = useState(false);
  const [authenticationLoading, setAuthenticationLoading] = useState(false);
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

  const loadAuthentication = useCallback(async (queryFilters: AuditFilters, append = false, cursor = '') => {
    setAuthenticationLoading(true);
    setError('');
    try {
      const page = readAuthenticationPage(await apiRequest<AuthenticationPage>('/v1/operations/authentication-events' + makeAuthenticationQuery(queryFilters, cursor)));
      setAuthenticationItems((current) => append ? [...current, ...page.items.filter((item) => !current.some((existing) => existing.id === item.id))] : page.items);
      setAuthenticationNextCursor(page.nextCursor || '');
      setAuthenticationHasMore(page.hasMore);
    } catch (cause) {
      setError(cause instanceof APIError && cause.status === 403 ? 'Audit access is restricted to compliance and audit readers.' : cause instanceof Error ? cause.message : 'Authentication evidence could not be loaded.');
    } finally {
      setAuthenticationLoading(false);
    }
  }, []);

  useEffect(() => {
    // The initial load synchronizes both authoritative evidence inventories into local state.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load(emptyFilters);
    void loadAuthentication(emptyFilters);
  }, [load, loadAuthentication]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setApplied({ ...filters });
    void load(filters);
    void loadAuthentication(filters);
  }

  return (
    <div className="workspace-stack">
      <section className="card">
        <div className="section-heading">
          <div>
            <h2>Immutable audit log</h2>
            <p className="muted">Searches are recorded as high-sensitivity audit events. Business evidence is hash-chained; authentication evidence is shown in a separate redacted stream with its own keyset cursor.</p>
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
          <button className="primary" type="submit" disabled={loading || authenticationLoading}>Search audit</button>
        </form>
        {error ? <div className="alert alert-danger" role="alert">{error}</div> : null}
        {loading || authenticationLoading ? <div className="alert alert-information" role="status">Loading immutable audit evidence…</div> : null}
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

      <section className="card">
        <div className="section-heading">
          <div>
            <h2>Authentication events</h2>
            <p className="muted">Redacted login, MFA, session, permission and network events. These records are searchable by actor, correlation and time; email hashes, network addresses, user agents and metadata stay server-side.</p>
          </div>
          <span className="pill">{authenticationItems.length} loaded</span>
        </div>
        {authenticationItems.length === 0 && !authenticationLoading ? <div className="empty-state"><strong>No authentication events</strong><p>No events match the applied filters.</p></div> : (
          <div className="table-wrap">
            <table>
              <caption className="sr-only">Redacted authentication events</caption>
              <thead><tr><th>Event</th><th>Actor</th><th>Correlation</th><th>Occurred</th></tr></thead>
              <tbody>
                {authenticationItems.map((item) => (
                  <tr key={item.id}>
                    <td>{item.eventType}<small>Authentication</small></td>
                    <td>{item.actorId || 'Unknown subject'}</td>
                    <td className="mono-small">{item.correlationId}</td>
                    <td>{new Date(item.occurredAt).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {authenticationHasMore ? <button className="secondary" type="button" disabled={authenticationLoading || !authenticationNextCursor} onClick={() => void loadAuthentication(applied, true, authenticationNextCursor)}>Load more authentication events</button> : null}
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
