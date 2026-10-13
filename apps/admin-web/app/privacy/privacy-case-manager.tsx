'use client';

import { FormEvent, useCallback, useEffect, useRef, useState } from 'react';

import { useAuth } from '../../components/auth-provider';
import { ControlledExportManager } from '../../components/controlled-export-manager';
import { StatusBadge } from '../../components/status-badge';
import { APIError, apiRequest, collectBoundedPages, type ListEnvelope } from '../../lib/api';
import { hasPermission } from '../../lib/session';

type CaseType = 'ACCESS' | 'RECTIFICATION' | 'ERASURE' | 'PORTABILITY' | 'OBJECTION' | 'RESTRICTION';
type PrivacyCase = {
  id: string;
  type: CaseType;
  status: string;
  subjectMasked: string;
  organisationId?: string;
  requestedAt: string;
  dueAt: string;
  assignedTo?: string;
  createdBy: string;
  submittedBy?: string;
  decidedBy?: string;
  executedBy?: string;
  requestReason: string;
  decisionReason?: string;
  executionReason?: string;
  requestedChanges?: unknown;
  version: number;
  createdAt: string;
  updatedAt: string;
};
type PrivacyEvent = {
  id: string;
  type: string;
  actorId: string;
  reason?: string;
  caseVersion: number;
  occurredAt: string;
};

const caseTypes: Array<{ value: CaseType; label: string }> = [
  { value: 'ACCESS', label: 'Access request' },
  { value: 'RECTIFICATION', label: 'Rectification' },
  { value: 'ERASURE', label: 'Erasure' },
  { value: 'PORTABILITY', label: 'Portability' },
  { value: 'OBJECTION', label: 'Objection' },
  { value: 'RESTRICTION', label: 'Restriction' }
];

function displayDate(value?: string) {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString();
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

export function PrivacyCaseManager() {
  const { session } = useAuth();
  const [items, setItems] = useState<PrivacyCase[]>([]);
  const [selected, setSelected] = useState<PrivacyCase>();
  const [events, setEvents] = useState<PrivacyEvent[]>([]);
  const [type, setType] = useState<CaseType>('ACCESS');
  const [msisdn, setMsisdn] = useState('');
  const [organisationId, setOrganisationId] = useState('');
  const [reason, setReason] = useState('');
  const [dueAt, setDueAt] = useState('');
  const [submissionReason, setSubmissionReason] = useState('');
  const [decisionReason, setDecisionReason] = useState('');
  const [executionReason, setExecutionReason] = useState('');
  const [assignment, setAssignment] = useState('');
  const [assignmentReason, setAssignmentReason] = useState('');
  const [rectificationField, setRectificationField] = useState('');
  const [rectificationValue, setRectificationValue] = useState('');
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');
  const selectionRequest = useRef(0);
  const loadRequest = useRef(0);

  const canWrite = hasPermission(session?.permissions, 'privacy.write');
  const canApprove = hasPermission(session?.permissions, 'privacy.approve');
  const canExecute = hasPermission(session?.permissions, 'privacy.execute');

  function requiresStepUp(error: unknown) {
    return error instanceof APIError && (error.code === 'STEP_UP_REQUIRED' || error.code === 'MFA_STEP_UP_REQUIRED');
  }

  function goToStepUp() {
    window.location.assign('/step-up?returnTo=' + encodeURIComponent('/privacy'));
  }

  const load = useCallback(async () => {
    const request = ++loadRequest.current;
    try {
      const values = await collectBoundedPages(
        (cursor) => apiRequest<ListEnvelope<PrivacyCase>>('/v1/privacy/cases?limit=100' + (cursor ? '&after=' + encodeURIComponent(cursor) : '')),
        20
      );
      if (request === loadRequest.current) setItems(values);
    } catch (error) {
      if (requiresStepUp(error)) {
        goToStepUp();
        return;
      }
      setMessage(errorMessage(error, 'Privacy cases could not be loaded.'));
    }
  }, []);

  useEffect(() => {
    // The initial load synchronizes the server-side privacy register into local state.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [load]);

  async function selectCase(item: PrivacyCase) {
    const request = ++selectionRequest.current;
    setBusy(item.id);
    setSelected(undefined);
    setEvents([]);
    setMessage('');
    try {
      const [detail, history] = await Promise.all([
        apiRequest<PrivacyCase>('/v1/privacy/cases/' + encodeURIComponent(item.id)),
        collectBoundedPages(
          (cursor) => apiRequest<ListEnvelope<PrivacyEvent>>('/v1/privacy/cases/' + encodeURIComponent(item.id) + '/events?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')),
          20
        )
      ]);
      if (request !== selectionRequest.current) return;
      setSelected(detail);
      setEvents(history);
    } catch (error) {
      if (request !== selectionRequest.current) return;
      if (requiresStepUp(error)) {
        goToStepUp();
        return;
      }
      setMessage(errorMessage(error, 'Privacy case detail could not be loaded.'));
    } finally {
      if (request === selectionRequest.current) setBusy('');
    }
  }

  async function createCase(event: FormEvent) {
    event.preventDefault();
    if (!canWrite || !msisdn.trim() || reason.trim().length < 8) return;
    setBusy('create');
    setMessage('');
    try {
      const requestedChanges = type === 'RECTIFICATION' ? { [rectificationField.trim()]: rectificationValue.trim() } : undefined;
      const created = await apiRequest<PrivacyCase>('/v1/privacy/cases', {
        method: 'POST',
        body: JSON.stringify({
          type,
          msisdn: msisdn.trim(),
          organisationId: organisationId.trim() || undefined,
          reason: reason.trim(),
          requestedChanges,
          dueAt: dueAt ? new Date(dueAt).toISOString() : undefined
        })
      });
      setMsisdn('');
      setOrganisationId('');
      setReason('');
      setRectificationField('');
      setRectificationValue('');
      setDueAt('');
      selectionRequest.current += 1;
      setSelected(created);
      setEvents([]);
      setMessage('Privacy case created. The subject identifier remains masked in the register.');
      await load();
    } catch (error) {
      if (requiresStepUp(error)) {
        goToStepUp();
        return;
      }
      setMessage(errorMessage(error, 'Privacy case could not be created.'));
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
      const value = await apiRequest<PrivacyCase>('/v1/privacy/cases/' + encodeURIComponent(selectedId) + path, {
        method: 'POST',
        body: JSON.stringify(body)
      });
      if (request !== selectionRequest.current) return;
      setSelected(value);
      setMessage(success);
      const history = await collectBoundedPages(
        (cursor) => apiRequest<ListEnvelope<PrivacyEvent>>('/v1/privacy/cases/' + encodeURIComponent(selectedId) + '/events?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')),
        20
      );
      if (request === selectionRequest.current) setEvents(history);
      await load();
    } catch (error) {
      if (request !== selectionRequest.current) return;
      if (requiresStepUp(error)) {
        goToStepUp();
        return;
      }
      setMessage(errorMessage(error, 'Privacy case action could not be completed.'));
    } finally {
      if (request === selectionRequest.current) setBusy('');
    }
  }

  return (
    <div className="workspace-stack">
      {message ? <div className="alert alert-information" role="status">{message}</div> : null}

      <section className="card">
        <div className="section-heading">
          <div><h2>Privacy case register</h2><p className="muted">Create and progress masked data-subject requests. Exact lookup, legal holds, step-up and maker-checker rules stay server-authoritative.</p></div>
          <span className="pill">{items.length} cases</span>
        </div>
        {!items.length ? <div className="empty-state"><strong>No privacy cases</strong><p>Create a case below to begin a governed request.</p></div> : (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Case</th><th>Subject</th><th>Due</th><th>Owner</th><th>Status</th><th>Version</th></tr></thead>
              <tbody>{items.map((item) => <tr key={item.id} className={selected?.id === item.id ? 'selected-row' : undefined}>
                <td><button type="button" className="table-link" onClick={() => void selectCase(item)} disabled={busy === item.id}>{item.type}<small>{item.id}</small></button></td>
                <td>{item.subjectMasked}</td>
                <td>{displayDate(item.dueAt)}</td>
                <td>{item.assignedTo || 'Unassigned'}</td>
                <td><StatusBadge status={item.status} /></td>
                <td>{item.version}</td>
              </tr>)}</tbody>
            </table>
          </div>
        )}
      </section>

      <section className="grid grid-2">
        <form className="card form-stack" onSubmit={createCase}>
          <h2>Create privacy case</h2>
          <p className="muted">The full MSISDN is sent only to the protected backend lookup boundary; the UI renders the masked subject returned by the server.</p>
          <label>Case type<select value={type} onChange={(event) => setType(event.target.value as CaseType)} disabled={!canWrite}>{caseTypes.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label>
          <label>Subject MSISDN<input value={msisdn} onChange={(event) => setMsisdn(event.target.value)} inputMode="tel" autoComplete="off" required disabled={!canWrite} /></label>
          <label>Organisation ID<input value={organisationId} onChange={(event) => setOrganisationId(event.target.value)} disabled={!canWrite} /></label>
          <label>Request reason<textarea value={reason} onChange={(event) => setReason(event.target.value)} minLength={8} maxLength={1000} required disabled={!canWrite} /></label>
          {type === 'RECTIFICATION' ? <><label>Field to correct<input value={rectificationField} onChange={(event) => setRectificationField(event.target.value)} required disabled={!canWrite} placeholder="e.g. preferredLanguage" /></label><label>Corrected value<input value={rectificationValue} onChange={(event) => setRectificationValue(event.target.value)} required disabled={!canWrite} /></label></> : null}
          <label>Due date<input type="datetime-local" value={dueAt} onChange={(event) => setDueAt(event.target.value)} disabled={!canWrite} /></label>
          <button className="primary" type="submit" disabled={!canWrite || busy !== '' || reason.trim().length < 8 || (type === 'RECTIFICATION' && (!rectificationField.trim() || !rectificationValue.trim()))}>{busy === 'create' ? 'Creating…' : 'Create privacy case'}</button>
        </form>

        <article className="card form-stack">
          <div className="section-heading"><div><h2>Case workspace</h2><p className="muted">{selected ? selected.type + ' · ' + selected.subjectMasked : 'Select a case to inspect its lifecycle.'}</p></div>{selected ? <StatusBadge status={selected.status} /> : null}</div>
          {!selected ? <div className="empty-state"><strong>No case selected</strong><p>Case history, ownership and governed actions appear here.</p></div> : (
            <>
              <dl className="definition-list">
                <div><dt>Subject</dt><dd>{selected.subjectMasked}</dd></div>
                <div><dt>Organisation</dt><dd>{selected.organisationId || 'Platform scope'}</dd></div>
                <div><dt>Requested</dt><dd>{displayDate(selected.requestedAt)}</dd></div>
                <div><dt>Due</dt><dd>{displayDate(selected.dueAt)}</dd></div>
                <div><dt>Created by</dt><dd>{selected.createdBy}</dd></div>
                <div><dt>Reason</dt><dd>{selected.requestReason}</dd></div>
                <div><dt>Version</dt><dd>{selected.version}</dd></div>
              </dl>

              {selected.status === 'OPEN' || selected.status === 'ASSIGNED' ? (
                <fieldset><legend>Assignment and submission</legend>
                  <label>Assignee<input value={assignment} onChange={(event) => setAssignment(event.target.value)} disabled={!canWrite} placeholder="Operator user ID" /></label>
                  <label>Assignment reason<textarea value={assignmentReason} onChange={(event) => setAssignmentReason(event.target.value)} disabled={!canWrite} /></label>
                  <div className="button-row">
                    <button type="button" onClick={() => void mutate('/assign', { assignee: assignment.trim(), reason: assignmentReason.trim(), expectedVersion: selected.version }, 'Privacy case assigned.')} disabled={!canWrite || busy !== '' || !assignment.trim()}>Assign case</button>
                    <label>Submission reason<textarea aria-label="Submission reason" value={submissionReason} onChange={(event) => setSubmissionReason(event.target.value)} disabled={!canWrite} /></label>
                    <button type="button" className="primary" onClick={() => void mutate('/submit', { reason: submissionReason.trim(), expectedVersion: selected.version }, 'Privacy case submitted for independent approval.')} disabled={!canWrite || busy !== '' || submissionReason.trim().length < 8}>Submit for approval</button>
                  </div>
                </fieldset>
              ) : null}

              {selected.status === 'PENDING_APPROVAL' && selected.createdBy !== session?.id && selected.submittedBy !== session?.id ? (
                <fieldset><legend>Independent checker decision</legend>
                  <label>Decision reason<textarea value={decisionReason} onChange={(event) => setDecisionReason(event.target.value)} disabled={!canApprove} /></label>
                  <div className="button-row"><button type="button" className="primary" onClick={() => void mutate('/decision', { approve: true, reason: decisionReason.trim(), expectedVersion: selected.version }, 'Privacy case approved.')} disabled={!canApprove || busy !== '' || decisionReason.trim().length < 8}>Approve case</button><button type="button" onClick={() => void mutate('/decision', { approve: false, reason: decisionReason.trim(), expectedVersion: selected.version }, 'Privacy case rejected.')} disabled={!canApprove || busy !== '' || decisionReason.trim().length < 8}>Reject case</button></div>
                </fieldset>
              ) : null}
              {selected.status === 'PENDING_APPROVAL' && (selected.createdBy === session?.id || selected.submittedBy === session?.id) ? <p className="muted">Awaiting independent checker approval. The creator or submitter cannot approve this case.</p> : null}

              {selected.status === 'APPROVED' ? <fieldset><legend>Execute approved case</legend><label>Execution reason<textarea value={executionReason} onChange={(event) => setExecutionReason(event.target.value)} disabled={!canExecute} /></label><button type="button" className="primary" onClick={() => void mutate('/execute', { reason: executionReason.trim(), expectedVersion: selected.version }, 'Privacy case execution requested.')} disabled={!canExecute || busy !== '' || executionReason.trim().length < 8}>Execute case</button></fieldset> : null}

              {selected.status === 'COMPLETED' && (selected.type === 'ACCESS' || selected.type === 'PORTABILITY') ? <ControlledExportManager kind="PRIVACY_PACKAGE" objectId={selected.id} title="Controlled privacy package" /> : null}

              <section><h3>Immutable case history</h3>{events.length ? <ol className="process-list">{events.map((event) => <li key={event.id}><strong>{event.type}</strong><span>{event.actorId} · v{event.caseVersion} · {displayDate(event.occurredAt)}</span>{event.reason ? <small>{event.reason}</small> : null}</li>)}</ol> : <p className="muted">No history loaded.</p>}</section>
            </>
          )}
        </article>
      </section>
    </div>
  );
}
