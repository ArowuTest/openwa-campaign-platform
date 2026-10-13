'use client';

import { FormEvent, useCallback, useEffect, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';

import { useAuth } from '../../../components/auth-provider';
import { StatusBadge } from '../../../components/status-badge';
import { APIError, apiRequest, type ListEnvelope } from '../../../lib/api';
import { hasPermission } from '../../../lib/session';

type AlertStatus = 'ACTIVE' | 'ACKNOWLEDGED' | 'RESOLVED';
type AlertSeverity = 'INFO' | 'WARNING' | 'CRITICAL';

type OperationalAlert = {
  id: string;
  policyId: string;
  status: AlertStatus;
  severity: AlertSeverity;
  observedValue: number;
  threshold: number;
  occurrenceCount: number;
  firstTriggeredAt: string;
  lastObservedAt: string;
  acknowledgedBy?: string;
  acknowledgedAt?: string | null;
  resolvedAt?: string | null;
  linkedIncidentId?: string;
  escalationIndex: number;
  nextEscalationAt?: string | null;
  version: number;
  updatedAt: string;
};

type AlertEvent = {
  id: string;
  eventType: string;
  actorId?: string;
  detail: string;
  occurredAt: string;
};

const pageLimit = 50;
const maxPages = 20;

function dateTime(value?: string | null): string {
  if (!value) return 'Not recorded';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? 'Invalid timestamp' : date.toLocaleString();
}

function errorMessage(error: unknown, fallback: string) {
  if (error instanceof APIError && error.status === 409) {
    return 'This alert changed since it was loaded. Refresh and inspect the current server version before another action.';
  }
  return error instanceof Error ? error.message : fallback;
}

function isStepUp(error: unknown) {
  return error instanceof APIError && error.status === 403 && error.code.includes('STEP_UP');
}

function shortReference(value?: string) {
  return value ? '…' + value.slice(-8) : 'Not recorded';
}

export function AlertCentre() {
  const { session } = useAuth();
  const canRead = hasPermission(session?.permissions, 'operations.read');
  const generation = JSON.stringify([
    session?.id,
    session?.sessionId,
    [...(session?.permissions ?? [])].sort()
  ]);
  if (!canRead) return <p role="status">Alert Centre requires operations.read permission.</p>;
  return <AlertCentreGeneration key={generation} />;
}

function AlertCentreGeneration() {
  const router = useRouter();
  const { session } = useAuth();
  const canAcknowledge = hasPermission(session?.permissions, 'operations.write');

  const mounted = useRef(false);
  const listRequest = useRef(0);
  const eventRequest = useRef(0);
  const selectedRef = useRef('');
  const listPageCount = useRef(0);
  const eventPageCount = useRef(0);
  const listCursors = useRef(new Set<string>());
  const eventCursors = useRef(new Set<string>());
  const conflictedVersions = useRef(new Map<string, number>());
  const acknowledgementBusy = useRef(false);

  const [alerts, setAlerts] = useState<OperationalAlert[]>([]);
  const [statusFilter, setStatusFilter] = useState<AlertStatus | ''>('ACTIVE');
  const [severityFilter, setSeverityFilter] = useState<AlertSeverity | ''>('');
  const [selectedId, setSelectedId] = useState('');
  const [events, setEvents] = useState<AlertEvent[]>([]);
  const [conflictedIDs, setConflictedIDs] = useState<string[]>([]);
  const [listLoading, setListLoading] = useState(false);
  const [listLoaded, setListLoaded] = useState(false);
  const [eventLoading, setEventLoading] = useState(false);
  const [moreList, setMoreList] = useState(false);
  const [moreEvents, setMoreEvents] = useState(false);
  const [nextListCursor, setNextListCursor] = useState('');
  const [nextEventCursor, setNextEventCursor] = useState('');
  const [listHasMore, setListHasMore] = useState(false);
  const [eventHasMore, setEventHasMore] = useState(false);
  const [lastFetchedAt, setLastFetchedAt] = useState('');
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [eventError, setEventError] = useState('');

  const selected = alerts.find((alert) => alert.id === selectedId);
  const conflict = Boolean(selected && conflictedIDs.includes(selected.id));

  const toStepUp = useCallback(() => {
    if (mounted.current) router.push('/step-up?returnTo=' + encodeURIComponent('/operations/alerts'));
  }, [router]);

  const loadAlerts = useCallback(async (cursor = '', reset = false) => {
    const sequence = ++listRequest.current;
    if (reset) {
      listPageCount.current = 0;
      listCursors.current.clear();
      setListLoading(true);
      setSelectedId('');
      selectedRef.current = '';
      eventRequest.current += 1;
      setEvents([]);
      setReason('');
      setEventError('');
    } else {
      if (listPageCount.current >= maxPages) {
        setListHasMore(false);
        setError('The loaded alert register is capped at 1,000 entries. Change the server filters or refresh.');
        return;
      }
      setMoreList(true);
    }
    setError('');
    const search = new URLSearchParams({ limit: String(pageLimit) });
    if (statusFilter) search.set('status', statusFilter);
    if (severityFilter) search.set('severity', severityFilter);
    if (cursor) search.set('cursor', cursor);

    try {
      const page = await apiRequest<ListEnvelope<OperationalAlert>>('/v1/operations/alerts?' + search.toString());
      if (!mounted.current || listRequest.current !== sequence) return;
      if (!Array.isArray(page.items) || typeof page.hasMore !== 'boolean' || (page.hasMore && !page.nextCursor)) {
        throw new Error('The alert register returned an invalid page; no record has been modified.');
      }
      if (page.hasMore && (page.nextCursor === cursor || listCursors.current.has(page.nextCursor || ''))) {
        throw new Error('Repeated server alert cursor; further pagination has been blocked.');
      }
      listPageCount.current += 1;
      if (page.hasMore && page.nextCursor) listCursors.current.add(page.nextCursor);
      if (reset) {
        const newlyValid: string[] = [];
        for (const item of page.items) {
          const staleVersion = conflictedVersions.current.get(item.id);
          if (staleVersion !== undefined && item.version > staleVersion) {
            conflictedVersions.current.delete(item.id);
            newlyValid.push(item.id);
          }
        }
        if (newlyValid.length) setConflictedIDs((prior) => prior.filter((id) => !newlyValid.includes(id)));
      }
      setAlerts((prior) => {
        if (reset) return page.items;
        const existing = new Set(prior.map((item) => item.id));
        return [...prior, ...page.items.filter((item) => !existing.has(item.id))];
      });
      setListHasMore(page.hasMore && listPageCount.current < maxPages);
      setNextListCursor(page.nextCursor || '');
      setLastFetchedAt(new Date().toISOString());
      setListLoaded(true);
    } catch (cause) {
      if (!mounted.current || sequence !== listRequest.current) return;
      if (isStepUp(cause)) toStepUp();
      else setError(errorMessage(cause, 'The alert register could not be loaded.'));
    } finally {
      if (mounted.current && sequence === listRequest.current) {
        setListLoading(false);
        setMoreList(false);
      }
    }
  }, [severityFilter, statusFilter, toStepUp]);

  const loadEvents = useCallback(async (alertId: string, cursor = '', reset = false) => {
    const sequence = ++eventRequest.current;
    if (reset) {
      eventPageCount.current = 0;
      eventCursors.current.clear();
      setEventLoading(true);
      setEvents([]);
    } else {
      if (eventPageCount.current >= maxPages) {
        setEventHasMore(false);
        setEventError('The displayed event history is capped at 1,000 entries.');
        return;
      }
      setMoreEvents(true);
    }
    setEventError('');
    try {
      const search = new URLSearchParams({ limit: String(pageLimit) });
      if (cursor) search.set('cursor', cursor);
      const page = await apiRequest<ListEnvelope<AlertEvent>>(
        '/v1/operations/alerts/' + encodeURIComponent(alertId) + '/events?' + search.toString()
      );
      if (!mounted.current || selectedRef.current !== alertId || sequence !== eventRequest.current) return;
      if (!Array.isArray(page.items) || typeof page.hasMore !== 'boolean' || (page.hasMore && !page.nextCursor)) {
        throw new Error('The alert audit history returned an invalid page.');
      }
      if (page.hasMore && (page.nextCursor === cursor || eventCursors.current.has(page.nextCursor || ''))) {
        throw new Error('Repeated alert-event cursor; the history cannot continue safely.');
      }
      eventPageCount.current += 1;
      if (page.hasMore && page.nextCursor) eventCursors.current.add(page.nextCursor);
      // Raw event evidence blobs are deliberately not retained or displayed.
      const safe = page.items.map((item) => ({
        id: item.id,
        eventType: item.eventType,
        actorId: item.actorId,
        detail: item.detail,
        occurredAt: item.occurredAt
      }));
      setEvents((prior) => {
        if (reset) return safe;
        const existing = new Set(prior.map((item) => item.id));
        return [...prior, ...safe.filter((item) => !existing.has(item.id))];
      });
      setEventHasMore(page.hasMore && eventPageCount.current < maxPages);
      setNextEventCursor(page.nextCursor || '');
    } catch (cause) {
      if (!mounted.current || selectedRef.current !== alertId || sequence !== eventRequest.current) return;
      if (isStepUp(cause)) toStepUp();
      else setEventError(errorMessage(cause, 'Unable to load the selected alert audit history.'));
    } finally {
      if (mounted.current && sequence === eventRequest.current) {
        setEventLoading(false);
        setMoreEvents(false);
      }
    }
  }, [toStepUp]);

  useEffect(() => {
    mounted.current = true;
    const conflicts = conflictedVersions.current;
    // An initial server fetch is required for an authenticated register.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadAlerts('', true);
    return () => {
      mounted.current = false;
      listRequest.current += 1;
      eventRequest.current += 1;
      selectedRef.current = '';
      conflicts.clear();
    };
  }, [loadAlerts]);

  function selectAlert(item: OperationalAlert) {
    if (busy) return;
    selectedRef.current = item.id;
    setSelectedId(item.id);
    setReason('');
    setNotice('');
    setError('');
    void loadEvents(item.id, '', true);
  }

  async function acknowledge(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected || selected.status !== 'ACTIVE' || !canAcknowledge || reason.trim().length < 8 ||
      busy || acknowledgementBusy.current || conflictedVersions.current.has(selected.id)) return;

    const target = selected;
    acknowledgementBusy.current = true;
    setBusy(true);
    setNotice('');
    setError('');
    try {
      const updated = await apiRequest<OperationalAlert>(
        '/v1/operations/alerts/' + encodeURIComponent(target.id) + '/acknowledge',
        { method: 'POST', body: JSON.stringify({ expectedVersion: target.version, reason: reason.trim() }) }
      );
      if (!mounted.current || selectedRef.current !== target.id) return;
      setAlerts((prior) => prior
        .map((item) => item.id === target.id ? updated : item)
        .filter((item) => statusFilter !== 'ACTIVE' || item.status === 'ACTIVE'));
      if (statusFilter === 'ACTIVE') {
        setSelectedId('');
        selectedRef.current = '';
        eventRequest.current += 1;
        setEvents([]);
      }
      setReason('');
      setNotice('Acknowledged successfully; this does not resolve the underlying condition. Continue the incident response workflow.');
    } catch (cause) {
      if (!mounted.current || selectedRef.current !== target.id) return;
      if (cause instanceof APIError && cause.status === 409) {
        conflictedVersions.current.set(target.id, target.version);
        setConflictedIDs((prior) => prior.includes(target.id) ? prior : [...prior, target.id]);
      }
      if (isStepUp(cause)) toStepUp();
      else setError(errorMessage(cause, 'Alert acknowledgement could not be recorded.'));
    } finally {
      acknowledgementBusy.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  return (
    <div className="workspace-stack">
      {notice ? <div className="alert alert-information" role="status">{notice}</div> : null}
      {error ? <div className="alert alert-danger" role="alert">{error}</div> : null}

      <section className="card form-stack">
        <div className="section-heading">
          <div><h2>Operational alert register</h2>
            <p className="muted">Server-filtered alerts, newest observations first. Only the loaded records are counted; this is not the full platform alert total.</p>
          </div>
          <button type="button" disabled={listLoading || busy} onClick={() => void loadAlerts('', true)}>Refresh alerts</button>
        </div>
        <div className="grid grid-2">
          <label>Status
            <select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value as AlertStatus | '')} disabled={busy}>
              <option value="ACTIVE">Active</option>
              <option value="">All statuses</option>
              <option value="ACKNOWLEDGED">Acknowledged</option>
              <option value="RESOLVED">Resolved</option>
            </select>
          </label>
          <label>Severity
            <select value={severityFilter} onChange={(event) => setSeverityFilter(event.target.value as AlertSeverity | '')} disabled={busy}>
              <option value="">All severities</option>
              <option value="CRITICAL">Critical</option>
              <option value="WARNING">Warning</option>
              <option value="INFO">Information</option>
            </select>
          </label>
        </div>
        {lastFetchedAt ? <p className="muted">Last browser refresh: {dateTime(lastFetchedAt)}. This is not the last alert event timestamp.</p> : null}
        <p className="muted" aria-live="polite">{alerts.length} loaded alert records</p>
        {listLoading && !listLoaded ? <p role="status">Loading operational alerts…</p> : null}
        {listLoaded && !listLoading && alerts.length === 0 && !error ? (
          <div className="empty-state">
            <strong>No alerts matched the selected server filters</strong>
            <p>This means no items were returned for the requested page, not that all platform risks have disappeared.</p>
          </div>
        ) : null}
        {alerts.length ? <div className="table-wrap">
          <table>
            <thead><tr><th>Alert</th><th>Severity</th><th>Status</th><th>Observed</th><th>Threshold</th><th>Last observed</th><th>Action</th></tr></thead>
            <tbody>{alerts.map((item) => <tr key={item.id} className={selectedId === item.id ? 'selected-row' : undefined}>
              <td><strong>{shortReference(item.id)}</strong><small>Policy {shortReference(item.policyId)}</small></td>
              <td><StatusBadge status={item.severity} /></td>
              <td><StatusBadge status={item.status} /></td>
              <td>{item.observedValue} <small>{item.occurrenceCount} observations</small></td>
              <td>{item.threshold}</td>
              <td>{dateTime(item.lastObservedAt)}</td>
              <td><button type="button" onClick={() => selectAlert(item)} disabled={busy} aria-label={'Inspect alert ' + item.id}>Inspect</button></td>
            </tr>)}</tbody>
          </table>
        </div> : null}
        {listHasMore ? <button type="button" disabled={listLoading || moreList || busy} onClick={() => void loadAlerts(nextListCursor)}>{moreList ? 'Loading…' : 'Load older alerts'}</button> : null}
      </section>

      {selected ? (
        <section className="grid grid-2">
          <article className="card form-stack">
            <div className="section-heading"><h2>Alert detail and history</h2><StatusBadge status={selected.status} /></div>
            <dl className="definition-list">
              <div><dt>Alert reference</dt><dd>{selected.id}</dd></div>
              <div><dt>Policy</dt><dd>{shortReference(selected.policyId)}</dd></div>
              <div><dt>Severity</dt><dd>{selected.severity}</dd></div>
              <div><dt>First triggered</dt><dd>{dateTime(selected.firstTriggeredAt)}</dd></div>
              <div><dt>Last observed</dt><dd>{dateTime(selected.lastObservedAt)}</dd></div>
              <div><dt>Observed vs threshold</dt><dd>{selected.observedValue} / {selected.threshold}</dd></div>
              <div><dt>Escalation count</dt><dd>{selected.escalationIndex}</dd></div>
              <div><dt>Next escalation</dt><dd>{dateTime(selected.nextEscalationAt)}</dd></div>
              <div><dt>Linked incident</dt><dd>{shortReference(selected.linkedIncidentId)}</dd></div>
              <div><dt>Alert version</dt><dd>{selected.version}</dd></div>
            </dl>
            <h3>Immutable alert events</h3>
            {eventLoading ? <p role="status">Loading alert history…</p> : null}
            {eventError ? <p role="alert">{eventError}</p> : null}
            {events.length ? <div className="table-wrap"><table>
              <thead><tr><th>Event</th><th>Detail</th><th>Observed at</th></tr></thead>
              <tbody>{events.map((item) => <tr key={item.id}>
                <td><strong>{item.eventType}</strong><small>{shortReference(item.actorId)}</small></td>
                <td>{item.detail || 'No detail'}</td>
                <td>{dateTime(item.occurredAt)}</td>
              </tr>)}</tbody>
            </table></div> : !eventLoading && !eventError ? <p>No events on the loaded page.</p> : null}
            {eventHasMore ? <button type="button" disabled={eventLoading || moreEvents || busy} onClick={() => void loadEvents(selected.id, nextEventCursor)}>{moreEvents ? 'Loading…' : 'Load earlier events'}</button> : null}
          </article>

          <form className="card form-stack" onSubmit={(event) => void acknowledge(event)}>
            <h2>Acknowledge active alert</h2>
            <p className="muted">Acknowledgement stops the configured escalation; it does not resolve the underlying condition, close a linked incident, or certify restored service health.</p>
            <label htmlFor="alert-ack-reason">Acknowledgement reason</label>
            <textarea id="alert-ack-reason" rows={4} value={reason} onChange={(event) => setReason(event.target.value)} minLength={8} maxLength={1000} aria-describedby="alert-ack-hint" disabled={!canAcknowledge || selected.status !== 'ACTIVE' || busy || conflict} />
            <small id="alert-ack-hint">Required audit reason (8–1,000 characters). Describe the incident response without storing credentials or personal data.</small>
            {conflict ? <p className="muted">Acknowledgement remains disabled until refreshed evidence shows a newer version.</p> : null}
            <button className="primary" type="submit" disabled={!canAcknowledge || selected.status !== 'ACTIVE' || reason.trim().length < 8 || busy || conflict}>
              {busy ? 'Acknowledging…' : 'Acknowledge alert'}
            </button>
            {!canAcknowledge ? <p className="muted">Acknowledge requires operations.write permission; viewing requires operations.read.</p> : null}
            {selected.status !== 'ACTIVE' ? <p className="muted">Only active alerts can be acknowledged.</p> : null}
          </form>
        </section>
      ) : <section className="card"><div className="empty-state"><strong>No alert selected</strong><p>Choose an alert to inspect its current state and chronological evidence.</p></div></section>}
    </div>
  );
}
