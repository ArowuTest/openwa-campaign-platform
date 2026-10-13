'use client';

import { FormEvent, useCallback, useRef, useState } from 'react';

import { useAuth } from '../../components/auth-provider';
import { StatusBadge } from '../../components/status-badge';
import { APIError, apiRequest, collectBoundedPages, type ListEnvelope } from '../../lib/api';
import { hasPermission } from '../../lib/session';

type Hold = {
  id: string;
  subjectMasked?: string;
  organisationId?: string;
  scope: string;
  status: string;
  reason: string;
  createdBy: string;
  submittedBy?: string;
  decidedBy?: string;
  decisionReason?: string;
  expiresAt?: string;
  releasedAt?: string;
  releaseReason?: string;
  version: number;
  createdAt: string;
};

type HoldEvent = {
  id: string;
  type: string;
  actorId: string;
  reason?: string;
  holdVersion: number;
  occurredAt: string;
};

function displayDate(value?: string) {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString();
}

function maskMsisdn(value: string) {
  const normalized = value.trim();
  if (normalized.length < 8) return '***';
  return normalized.slice(0, 4) + '******' + normalized.slice(-4);
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : 'Legal-hold action could not be completed.';
}

function requiresStepUp(error: unknown) {
  return error instanceof APIError && (error.code === 'STEP_UP_REQUIRED' || error.code === 'MFA_STEP_UP_REQUIRED');
}

function goToStepUp() {
  window.location.assign('/step-up?returnTo=' + encodeURIComponent('/privacy'));
}

export function LegalHoldManager() {
  const { session } = useAuth();
  const [items, setItems] = useState<Hold[]>([]);
  const [selected, setSelected] = useState<Hold>();
  const [events, setEvents] = useState<HoldEvent[]>([]);
  const [searchMsisdn, setSearchMsisdn] = useState('');
  const [activeLookup, setActiveLookup] = useState('');
  const [msisdn, setMsisdn] = useState('');
  const [organisationId, setOrganisationId] = useState('');
  const [scope, setScope] = useState('ALL');
  const [reason, setReason] = useState('');
  const [expiresAt, setExpiresAt] = useState('');
  const [submissionReason, setSubmissionReason] = useState('');
  const [decisionReason, setDecisionReason] = useState('');
  const [releaseReason, setReleaseReason] = useState('');
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');
  const selectionRequest = useRef(0);
  const lookupRequest = useRef(0);

  const canHold = hasPermission(session?.permissions, 'privacy.hold');
  const canApprove = hasPermission(session?.permissions, 'privacy.approve');

  const loadEvents = useCallback(async (holdId: string, request: number) => {
    try {
      const all = await collectBoundedPages(
        (cursor) => apiRequest<ListEnvelope<HoldEvent>>(
          '/v1/privacy/legal-holds/' + encodeURIComponent(holdId) + '/events?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
        ),
        20
      );
      if (request !== selectionRequest.current) return;
      setEvents(all);
    } catch (error) {
      if (requiresStepUp(error)) {
        goToStepUp();
        return;
      }
      setMessage(errorMessage(error));
    }
  }, []);

  const load = useCallback(async (query: string, clearSelection = true) => {
    const request = ++lookupRequest.current;
    const lookup = query.trim();
    setItems([]);
    if (clearSelection) {
      selectionRequest.current += 1;
      setSelected(undefined);
      setEvents([]);
    }
    if (!lookup) return;
    try {
      const holds = await collectBoundedPages(
        (cursor) => apiRequest<ListEnvelope<Hold>>(
          '/v1/privacy/legal-holds?activeOnly=false&limit=100&msisdn=' + encodeURIComponent(lookup) +
            (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
        ),
        20
      );
      if (request === lookupRequest.current) setItems(holds.map((item) => ({ ...item, subjectMasked: item.subjectMasked ?? maskMsisdn(lookup) })));
    } catch (error) {
      if (request === lookupRequest.current) setItems([]);
      if (clearSelection && request === lookupRequest.current) {
        setSelected(undefined);
        setEvents([]);
      }
      if (requiresStepUp(error)) {
        goToStepUp();
        return;
      }
      setMessage(errorMessage(error));
    }
  }, []);

  async function search(event: FormEvent) {
    event.preventDefault();
    if (!canHold || !searchMsisdn.trim()) return;
    setBusy('search');
    setMessage('');
    selectionRequest.current += 1;
    setSelected(undefined);
    setEvents([]);
    const lookup = searchMsisdn.trim();
    setActiveLookup(lookup);
    try {
      await load(lookup);
    } finally {
      setBusy('');
    }
  }

  async function selectHold(item: Hold) {
    const request = ++selectionRequest.current;
    setSelected(item);
    setEvents([]);
    setSubmissionReason('');
    setDecisionReason('');
    setReleaseReason('');
    await loadEvents(item.id, request);
  }

  async function createHold(event: FormEvent) {
    event.preventDefault();
    if (!canHold || msisdn.trim() === '' || reason.trim().length < 8) return;
    setBusy('create');
    setMessage('');
    try {
      const lookup = msisdn.trim();
      const created = await apiRequest<Hold>('/v1/privacy/legal-holds', {
        method: 'POST',
        body: JSON.stringify({
          msisdn: lookup,
          organisationId: organisationId.trim() || undefined,
          scope,
          reason: reason.trim(),
          expiresAt: expiresAt ? new Date(expiresAt).toISOString() : undefined
        })
      });
      const selectedHold = { ...created, subjectMasked: created.subjectMasked ?? maskMsisdn(lookup) };
      const request = ++selectionRequest.current;
      setSearchMsisdn(lookup);
      setActiveLookup(lookup);
      setSelected(selectedHold);
      setMsisdn('');
      setOrganisationId('');
      setReason('');
      setExpiresAt('');
      setMessage('Legal hold created as a draft.');
      await load(lookup, false);
      await loadEvents(created.id, request);
    } catch (error) {
      if (requiresStepUp(error)) {
        goToStepUp();
        return;
      }
      setMessage(errorMessage(error));
    } finally {
      setBusy('');
    }
  }

  async function mutate(path: string, body: Record<string, unknown>, success: string) {
    if (!selected) return;
    const request = selectionRequest.current;
    const selectedId = selected.id;
    setBusy(selectedId);
    setMessage('');
    try {
      const updated = await apiRequest<Hold>(
        '/v1/privacy/legal-holds/' + encodeURIComponent(selectedId) + path,
        { method: 'POST', body: JSON.stringify(body) }
      );
      if (request !== selectionRequest.current) return;
      const updatedHold = { ...updated, subjectMasked: updated.subjectMasked ?? selected.subjectMasked };
      setSelected(updatedHold);
      setMessage(success);
      await load(activeLookup, false);
      await loadEvents(updated.id, request);
    } catch (error) {
      if (request !== selectionRequest.current) return;
      if (requiresStepUp(error)) {
        goToStepUp();
        return;
      }
      setMessage(errorMessage(error));
    } finally {
      if (request === selectionRequest.current) setBusy('');
    }
  }

  return (
    <section className="card">
      <div className="section-heading">
        <div>
          <h2>Legal holds</h2>
          <p className="muted">Preserve governed records for an exact masked subject lookup with optional organisation context. Holds block eligible retention actions until independently released.</p>
        </div>
        <span className="pill">{items.length} holds</span>
      </div>

      {message ? <div className="alert alert-information" role="status">{message}</div> : null}

      <form className="form-stack" onSubmit={search}>
        <label>Lookup subject MSISDN
          <input
            value={searchMsisdn}
            onChange={(event) => {
              const value = event.target.value;
              setSearchMsisdn(value);
              if (value.trim() !== activeLookup) {
                selectionRequest.current += 1;
                setItems([]);
                setSelected(undefined);
                setEvents([]);
              }
            }}
            inputMode="tel"
            autoComplete="off"
            required
            disabled={!canHold}
          />
        </label>
        <button type="submit" disabled={!canHold || busy !== '' || !searchMsisdn.trim()}>
          {busy === 'search' ? 'Searching...' : 'Search holds'}
        </button>
      </form>

      {!activeLookup || activeLookup !== searchMsisdn.trim() ? (
        <div className="empty-state"><strong>Search a subject MSISDN</strong><p>Submit the exact lookup before the protected legal-hold register can be displayed.</p></div>
      ) : !items.length ? (
        <div className="empty-state"><strong>No legal holds</strong><p>No governed holds were found for this subject.</p></div>
      ) : (
        <div className="table-wrap">
          <table>
            <thead><tr><th>Hold</th><th>Subject</th><th>Scope</th><th>Expires</th><th>Status</th><th>Version</th></tr></thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} className={selected?.id === item.id ? 'selected-row' : undefined}>
                  <td><button className="table-link" type="button" onClick={() => void selectHold(item)}>{item.id}<small>{item.createdBy}</small></button></td>
                  <td>{item.subjectMasked ?? maskMsisdn(activeLookup)}</td>
                  <td>{item.scope}</td>
                  <td>{displayDate(item.expiresAt)}</td>
                  <td><StatusBadge status={item.status} /></td>
                  <td>{item.version}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="grid grid-2">
        <form className="form-stack" onSubmit={createHold}>
          <h3>Create legal hold</h3>
          <p className="muted">The full MSISDN is sent only to the protected backend lookup boundary; the UI renders a masked subject.</p>
          <label>Hold subject MSISDN<input value={msisdn} onChange={(event) => setMsisdn(event.target.value)} inputMode="tel" autoComplete="off" required disabled={!canHold} /></label>
          <label>Organisation ID<input value={organisationId} onChange={(event) => setOrganisationId(event.target.value)} disabled={!canHold} /></label>
          <label>Hold scope<select value={scope} onChange={(event) => setScope(event.target.value)} disabled={!canHold}><option value="ALL">All governed data</option><option value="CONTACT">Contact</option><option value="CONSENT">Consent</option><option value="CAMPAIGN">Campaign</option><option value="AUDIT">Audit</option><option value="COMMERCIAL">Commercial</option></select></label>
          <label>Hold reason<textarea value={reason} onChange={(event) => setReason(event.target.value)} minLength={8} maxLength={1000} required disabled={!canHold} /></label>
          <label>Expiry (optional)<input type="datetime-local" value={expiresAt} onChange={(event) => setExpiresAt(event.target.value)} disabled={!canHold} /></label>
          <button className="primary" type="submit" disabled={!canHold || busy !== '' || reason.trim().length < 8}>{busy === 'create' ? 'Creating...' : 'Create legal hold'}</button>
        </form>

        <article className="form-stack">
          <h3>Hold actions</h3>
          {!selected ? <p className="muted">Select a hold to submit, approve, or release it.</p> : (
            <>
              <dl className="definition-list">
                <div><dt>Subject</dt><dd>{selected.subjectMasked ?? maskMsisdn(activeLookup)}</dd></div>
                <div><dt>Status</dt><dd><StatusBadge status={selected.status} /></dd></div>
                <div><dt>Expires</dt><dd>{displayDate(selected.expiresAt)}</dd></div>
                <div><dt>Scope</dt><dd>{selected.scope}</dd></div>
                <div><dt>Organisation</dt><dd>{selected.organisationId || 'Platform scope'}</dd></div>
                <div><dt>Reason</dt><dd>{selected.reason}</dd></div>
                <div><dt>Created</dt><dd>{displayDate(selected.createdAt)} by {selected.createdBy}</dd></div>
              </dl>
              {selected.status === 'DRAFT' ? (
                <fieldset>
                  <legend>Submit for independent approval</legend>
                  <label>Submission reason<textarea value={submissionReason} onChange={(event) => setSubmissionReason(event.target.value)} disabled={!canHold} minLength={8} required /></label>
                  <button type="button" className="primary" onClick={() => void mutate('/submit', { expectedVersion: selected.version, reason: submissionReason.trim() }, 'Legal hold submitted for approval.')} disabled={!canHold || busy !== '' || submissionReason.trim().length < 8}>Submit hold</button>
                </fieldset>
              ) : null}
              {selected.status === 'PENDING_APPROVAL' && selected.createdBy !== session?.id && selected.submittedBy !== session?.id ? (
                <>
                  <label>Decision reason<textarea value={decisionReason} onChange={(event) => setDecisionReason(event.target.value)} disabled={!canApprove} /></label>
                  <div className="button-row">
                    <button type="button" className="primary" onClick={() => void mutate('/decision', { approve: true, expectedVersion: selected.version, reason: decisionReason.trim() }, 'Legal hold approved.')} disabled={!canApprove || busy !== '' || decisionReason.trim().length < 8}>Approve hold</button>
                    <button type="button" onClick={() => void mutate('/decision', { approve: false, expectedVersion: selected.version, reason: decisionReason.trim() }, 'Legal hold rejected.')} disabled={!canApprove || busy !== '' || decisionReason.trim().length < 8}>Reject hold</button>
                  </div>
                </>
              ) : null}
              {selected.status === 'PENDING_APPROVAL' && (selected.createdBy === session?.id || selected.submittedBy === session?.id) ? <p className="muted">Awaiting independent checker approval; the creator or submitter cannot approve this hold.</p> : null}
              {selected.status === 'ACTIVE' ? (
                <>
                  <label>Release reason<textarea value={releaseReason} onChange={(event) => setReleaseReason(event.target.value)} disabled={!canHold} /></label>
                  <button type="button" onClick={() => void mutate('/release', { expectedVersion: selected.version, reason: releaseReason.trim() }, 'Legal hold released.')} disabled={!canHold || busy !== '' || releaseReason.trim().length < 8}>Release hold</button>
                </>
              ) : null}
              <section>
                <h3>Immutable hold history</h3>
                {events.length ? <ol className="process-list">{events.map((event) => <li key={event.id}><strong>{event.type}</strong><span>{event.actorId} - v{event.holdVersion} - {displayDate(event.occurredAt)}</span>{event.reason ? <small>{event.reason}</small> : null}</li>)}</ol> : <p className="muted">No history loaded.</p>}
              </section>
            </>
          )}
        </article>
      </div>
    </section>
  );
}
