'use client';

import { FormEvent, useCallback, useEffect, useState } from 'react';

import { useAuth } from '../../components/auth-provider';
import { StatusBadge } from '../../components/status-badge';
import { apiRequest, type ListEnvelope } from '../../lib/api';
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
  const [exportReason, setExportReason] = useState('');
  const [exportFormat, setExportFormat] = useState('JSON');
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');

  const canWrite = hasPermission(session?.permissions, 'privacy.write');
  const canApprove = hasPermission(session?.permissions, 'privacy.approve');
  const canExecute = hasPermission(session?.permissions, 'privacy.execute');

  const load = useCallback(async () => {
    try {
      const page = await apiRequest<ListEnvelope<PrivacyCase>>('/v1/privacy/cases?limit=100');
      setItems(Array.isArray(page.items) ? page.items : []);
    } catch (error) {
      setMessage(errorMessage(error, 'Privacy cases could not be loaded.'));
    }
  }, []);

  useEffect(() => {
    // The initial load synchronizes the server-side privacy register into local state.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [load]);

  async function selectCase(item: PrivacyCase) {
    setBusy(item.id);
    setMessage('');
    try {
      const [detail, history] = await Promise.all([
        apiRequest<PrivacyCase>('/v1/privacy/cases/' + encodeURIComponent(item.id)),
        apiRequest<ListEnvelope<PrivacyEvent>>('/v1/privacy/cases/' + encodeURIComponent(item.id) + '/events?limit=100')
      ]);
      setSelected(detail);
      setEvents(history.items ?? []);
    } catch (error) {
      setMessage(errorMessage(error, 'Privacy case detail could not be loaded.'));
    } finally {
      setBusy('');
    }
  }

  async function createCase(event: FormEvent) {
    event.preventDefault();
    if (!canWrite || !msisdn.trim() || reason.trim().length < 8) return;
    setBusy('create');
    setMessage('');
    try {
      const created = await apiRequest<PrivacyCase>('/v1/privacy/cases', {
        method: 'POST',
        body: JSON.stringify({
          type,
          msisdn: msisdn.trim(),
          organisationId: organisationId.trim() || undefined,
          reason: reason.trim(),
          dueAt: dueAt ? new Date(dueAt).toISOString() : undefined
        })
      });
      setMsisdn('');
      setOrganisationId('');
      setReason('');
      setDueAt('');
      setSelected(created);
      setEvents([]);
      setMessage('Privacy case created. The subject identifier remains masked in the register.');
      await load();
    } catch (error) {
      setMessage(errorMessage(error, 'Privacy case could not be created.'));
    } finally {
      setBusy('');
    }
  }

  async function mutate(path: string, body: Record<string, unknown>, success: string) {
    if (!selected) return;
    setBusy(selected.id);
    setMessage('');
    try {
      const value = await apiRequest<PrivacyCase>('/v1/privacy/cases/' + encodeURIComponent(selected.id) + path, {
        method: 'POST',
        body: JSON.stringify(body)
      });
      setSelected(value);
      setMessage(success);
      await load();
    } catch (error) {
      setMessage(errorMessage(error, 'Privacy case action could not be completed.'));
    } finally {
      setBusy('');
    }
  }

  async function requestExport(event: FormEvent) {
    event.preventDefault();
    if (!selected || exportReason.trim().length < 8) return;
    setBusy(selected.id);
    setMessage('');
    try {
      await apiRequest('/v1/privacy/cases/' + encodeURIComponent(selected.id) + '/exports', {
        method: 'POST',
        body: JSON.stringify({ format: exportFormat, reason: exportReason.trim() })
      });
      setExportReason('');
      setMessage('Privacy package requested for independent approval.');
    } catch (error) {
      setMessage(errorMessage(error, 'Privacy package could not be requested.'));
    } finally {
      setBusy('');
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
          <label>Due date<input type="datetime-local" value={dueAt} onChange={(event) => setDueAt(event.target.value)} disabled={!canWrite} /></label>
          <button className="primary" type="submit" disabled={!canWrite || busy !== '' || reason.trim().length < 8}>{busy === 'create' ? 'Creating…' : 'Create privacy case'}</button>
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
                    <button type="button" className="primary" onClick={() => void mutate('/submit', { reason: submissionReason.trim(), expectedVersion: selected.version }, 'Privacy case submitted for independent approval.')} disabled={!canWrite || busy !== '' || submissionReason.trim().length < 5}>Submit for approval</button>
                  </div>
                </fieldset>
              ) : null}

              {selected.status === 'PENDING_APPROVAL' && selected.createdBy !== session?.id ? (
                <fieldset><legend>Independent checker decision</legend>
                  <label>Decision reason<textarea value={decisionReason} onChange={(event) => setDecisionReason(event.target.value)} disabled={!canApprove} /></label>
                  <div className="button-row"><button type="button" className="primary" onClick={() => void mutate('/decision', { approve: true, reason: decisionReason.trim(), expectedVersion: selected.version }, 'Privacy case approved.')} disabled={!canApprove || busy !== '' || decisionReason.trim().length < 5}>Approve case</button><button type="button" onClick={() => void mutate('/decision', { approve: false, reason: decisionReason.trim(), expectedVersion: selected.version }, 'Privacy case rejected.')} disabled={!canApprove || busy !== '' || decisionReason.trim().length < 5}>Reject case</button></div>
                </fieldset>
              ) : null}
              {selected.status === 'PENDING_APPROVAL' && selected.createdBy === session?.id ? <p className="muted">Awaiting independent checker approval. The creator cannot approve this case.</p> : null}

              {selected.status === 'APPROVED' ? <fieldset><legend>Execute approved case</legend><label>Execution reason<textarea value={executionReason} onChange={(event) => setExecutionReason(event.target.value)} disabled={!canExecute} /></label><button type="button" className="primary" onClick={() => void mutate('/execute', { reason: executionReason.trim(), expectedVersion: selected.version }, 'Privacy case execution requested.')} disabled={!canExecute || busy !== '' || executionReason.trim().length < 5}>Execute case</button></fieldset> : null}

              {selected.status === 'APPROVED' || selected.status === 'COMPLETED' ? <form className="form-stack" onSubmit={requestExport}><fieldset><legend>Controlled privacy package</legend><label>Format<select value={exportFormat} onChange={(event) => setExportFormat(event.target.value)}><option value="JSON">JSON</option><option value="PDF">PDF</option><option value="CSV">CSV</option><option value="XLSX">XLSX</option></select></label><label>Export reason<textarea value={exportReason} onChange={(event) => setExportReason(event.target.value)} minLength={8} required /></label><button type="submit" disabled={busy !== '' || exportReason.trim().length < 8}>Request privacy package</button></fieldset></form> : null}

              <section><h3>Immutable case history</h3>{events.length ? <ol className="process-list">{events.map((event) => <li key={event.id}><strong>{event.type}</strong><span>{event.actorId} · v{event.caseVersion} · {displayDate(event.occurredAt)}</span>{event.reason ? <small>{event.reason}</small> : null}</li>)}</ol> : <p className="muted">No history loaded.</p>}</section>
            </>
          )}
        </article>
      </section>
    </div>
  );
}
