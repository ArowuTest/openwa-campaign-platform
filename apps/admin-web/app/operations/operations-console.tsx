'use client';

import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';

import { StatusBadge } from '../../components/status-badge';
import { useAuth } from '../../components/auth-provider';
import { APIError, apiRequest, collectBoundedPages, type ListEnvelope } from '../../lib/api';
import { hasPermission } from '../../lib/session';

type Dashboard = {
  generatedAt: string;
  campaigns: Record<string, number>;
  recipients: Record<string, number>;
  senders: Record<string, number>;
  openIncidents: number;
  criticalIncidents: number;
  unknownOutcomes: number;
  queueDepth: number;
  staleWorkerNodes: number;
  unavailableGatewayNodes: number;
  unhealthySenderSessions: number;
  campaignsAtRisk: number;
  capacityShortfallPools: number;
  reconciliationBacklog: number;
};

type Incident = {
  id: string;
  campaignId?: string;
  senderSessionId?: string;
  category: string;
  severity: string;
  status: string;
  summary: string;
  ownerId?: string;
  resolution?: string;
  updatedAt: string;
  version: number;
};

type DeliveryException = {
  recipientId: string;
  campaignId: string;
  status: string;
  assignedSessionId?: string;
  providerMessageId?: string;
  attemptCount: number;
  errorCode?: string;
  updatedAt: string;
};

type Job = {
  id: string;
  type: string;
  status: string;
  attemptCount: number;
  maxAttempts: number;
  availableAt: string;
  leaseOwner?: string;
  leaseExpiresAt?: string;
  lastErrorCode?: string;
  createdAt: string;
  updatedAt: string;
};

type QueueSummary = {
  asAt: string;
  countsByStatus: Record<string, number>;
  countsByType: Record<string, number>;
  oldestPendingAt?: string;
  processingLeases: number;
  expiredLeases: number;
};

function isStepUp(error: unknown) {
  return error instanceof APIError && error.status === 403 && error.code.includes('STEP_UP');
}

export function OperationsConsole() {
  const router = useRouter();
  const { session } = useAuth();
  const [dashboard, setDashboard] = useState<Dashboard>();
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [exceptions, setExceptions] = useState<DeliveryException[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [queue, setQueue] = useState<QueueSummary>();
  const [selectedException, setSelectedException] = useState('');
  const [selectedJob, setSelectedJob] = useState('');
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');

  const canWrite = hasPermission(session?.permissions, 'operations.write');
  const selectedExceptionItem = useMemo(() => exceptions.find((item) => item.recipientId === selectedException), [exceptions, selectedException]);
  const selectedJobItem = useMemo(() => jobs.find((item) => item.id === selectedJob), [jobs, selectedJob]);

  const fetchOperations = useCallback(async () => Promise.all([
    apiRequest<Dashboard>('/v1/operations/dashboard'),
    collectBoundedPages<Incident>((cursor) => apiRequest<ListEnvelope<Incident>>('/v1/operations/incidents?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20),
    collectBoundedPages<DeliveryException>((cursor) => apiRequest<ListEnvelope<DeliveryException>>('/v1/operations/delivery-exceptions?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20),
    collectBoundedPages<Job>((cursor) => apiRequest<ListEnvelope<Job>>('/v1/operations/jobs?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20),
    apiRequest<QueueSummary>('/v1/operations/jobs/summary')
  ]), []);

  const applyOperations = useCallback((values: Awaited<ReturnType<typeof fetchOperations>>) => {
    const [dashboardValue, incidentItems, exceptionItems, jobItems, summary] = values;
    setDashboard(dashboardValue);
    setIncidents(incidentItems);
    setExceptions(exceptionItems);
    setJobs(jobItems);
    setQueue(summary);
  }, []);

  const load = useCallback(async () => {
    applyOperations(await fetchOperations());
  }, [applyOperations, fetchOperations]);

  useEffect(() => {
    let cancelled = false;
    void fetchOperations()
      .then((values) => {
        if (!cancelled) applyOperations(values);
      })
      .catch((cause) => {
        if (!cancelled) setMessage(cause instanceof Error ? cause.message : 'Operations evidence could not be loaded.');
      });
    return () => { cancelled = true; };
  }, [applyOperations, fetchOperations]);

  async function mutate(label: string, work: () => Promise<unknown>) {
    if (busy) return;
    setBusy(label);
    setMessage('');
    try {
      await work();
      setMessage(label + ' completed. Operations evidence refreshed.');
      await load();
    } catch (cause) {
      if (isStepUp(cause)) router.push('/step-up?returnTo=' + encodeURIComponent('/operations'));
      else setMessage(cause instanceof Error ? cause.message : label + ' failed.');
    } finally {
      setBusy('');
    }
  }

  async function resolveException(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedExceptionItem) return;
    const data = new FormData(event.currentTarget);
    const action = String(data.get('action') ?? '');
    await mutate('Resolve delivery exception', () => apiRequest('/v1/operations/delivery-exceptions/' + selectedExceptionItem.recipientId + '/resolve', {
      method: 'POST',
      body: JSON.stringify({
        action,
        evidenceRef: String(data.get('evidenceRef') ?? '').trim(),
        reason: String(data.get('reason') ?? '').trim(),
        ...(action === 'CONFIRM_NOT_SUBMITTED' ? { duplicateRiskAccepted: Boolean(data.get('duplicateRiskAccepted')) } : {})
      })
    }));
  }

  async function performJobAction(action: 'retry' | 'cancel', event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedJobItem) return;
    const reason = String(new FormData(event.currentTarget).get('reason') ?? '').trim();
    await mutate(action === 'retry' ? 'Retry dead-letter job' : 'Cancel pending job', () => apiRequest('/v1/operations/jobs/' + selectedJobItem.id + '/' + action, {
      method: 'POST',
      body: JSON.stringify({ reason })
    }));
  }

  const statusCounts = queue?.countsByStatus ?? {};

  return (
    <div className="workspace-stack">
      {message ? <div className={message.includes('failed') ? 'alert alert-danger' : 'alert alert-information'} role="status">{message}</div> : null}

      <section className="grid grid-4" aria-label="Operational health">
        <article className="card metric"><span className="muted">Queue depth</span><strong>{dashboard?.queueDepth ?? 0}</strong></article>
        <article className="card metric"><span className="muted">UNKNOWN outcomes</span><strong>{dashboard?.unknownOutcomes ?? 0}</strong></article>
        <article className="card metric"><span className="muted">Open incidents</span><strong>{dashboard?.openIncidents ?? 0}</strong></article>
        <article className="card metric"><span className="muted">Campaigns at risk</span><strong>{dashboard?.campaignsAtRisk ?? 0}</strong></article>
      </section>

      <section className="grid grid-2">
        <article className="card">
          <h2>Platform readiness signals</h2>
          <dl className="definition-list">
            <div><dt>Critical incidents</dt><dd>{dashboard?.criticalIncidents ?? 0}</dd></div>
            <div><dt>Unavailable gateway nodes</dt><dd>{dashboard?.unavailableGatewayNodes ?? 0}</dd></div>
            <div><dt>Unhealthy sender sessions</dt><dd>{dashboard?.unhealthySenderSessions ?? 0}</dd></div>
            <div><dt>Capacity-shortfall pools</dt><dd>{dashboard?.capacityShortfallPools ?? 0}</dd></div>
            <div><dt>Reconciliation backlog</dt><dd>{dashboard?.reconciliationBacklog ?? 0}</dd></div>
            <div><dt>Stale worker nodes</dt><dd>{dashboard?.staleWorkerNodes ?? 0}</dd></div>
          </dl>
        </article>
        <article className="card">
          <h2>Durable job queue</h2>
          <div className="gate-list">
            {Object.entries(statusCounts).map(([status, count]) => <div key={status}><span>{status.replaceAll('_', ' ')}</span><strong>{count}</strong></div>)}
          </div>
          <p className="muted">Processing leases: {queue?.processingLeases ?? 0} · expired leases: {queue?.expiredLeases ?? 0}</p>
          {queue?.oldestPendingAt ? <small>Oldest pending: {new Date(queue.oldestPendingAt).toLocaleString()}</small> : null}
        </article>
      </section>

      <section className="card">
        <div className="section-heading"><div><h2>Delivery exceptions</h2><p className="muted">UNKNOWN is sticky. Do not resend unless reviewed evidence proves provider submission did not occur and duplicate risk is explicitly accepted.</p></div></div>
        {exceptions.length === 0 ? <div className="empty-state"><strong>No delivery exceptions</strong><p>No UNKNOWN, retryable or permanent outcomes currently require operator review.</p></div> : (
          <div className="table-wrap"><table><thead><tr><th>Status</th><th>Campaign</th><th>Evidence</th><th>Attempts</th><th>Updated</th></tr></thead><tbody>{exceptions.map((item) => <tr key={item.recipientId} className={selectedException === item.recipientId ? 'selected-row' : undefined} onClick={() => setSelectedException(item.recipientId)}><td><StatusBadge status={item.status} /><small>{item.errorCode || 'No error code'}</small></td><td>{item.campaignId}</td><td>{item.providerMessageId ? 'Provider ID recorded' : 'No provider ID'}<small>{item.assignedSessionId ? 'Session ' + item.assignedSessionId : 'No assigned session'}</small></td><td>{item.attemptCount}</td><td>{new Date(item.updatedAt).toLocaleString()}</td></tr>)}</tbody></table></div>
        )}
      </section>

      <section className="grid grid-2">
        <form className="card form-stack" onSubmit={resolveException}>
          <h2>Reconcile selected exception</h2>
          <p className="muted">{selectedExceptionItem ? selectedExceptionItem.status + ' · ' + selectedExceptionItem.recipientId : 'Select an exception above.'}</p>
          <label>Reviewed outcome<select name="action" defaultValue="CONFIRM_SENT"><option value="CONFIRM_SENT">Confirm sent</option><option value="CONFIRM_DELIVERED">Confirm delivered</option><option value="CONFIRM_READ">Confirm read</option><option value="MARK_FAILED_PERMANENT">Mark failed permanently</option><option value="CONFIRM_NOT_SUBMITTED">Confirm not submitted</option></select></label>
          <label>Evidence reference<input name="evidenceRef" minLength={6} required /></label>
          <label>Reason<textarea name="reason" minLength={8} rows={3} required /></label>
          <label className="check"><input type="checkbox" name="duplicateRiskAccepted" /><span>I explicitly accept possible duplicate-send risk only when confirming that provider submission did not occur.</span></label>
          <button className="primary" type="submit" disabled={!canWrite || !selectedExceptionItem || Boolean(busy)}>Record reviewed outcome</button>
        </form>

        <form className="card form-stack" onSubmit={(event) => {
          const action = selectedJobItem?.status === 'DEAD_LETTER' ? 'retry' : 'cancel';
          void performJobAction(action, event);
        }}>
          <h2>Durable job action</h2>
          <p className="muted">{selectedJobItem ? selectedJobItem.type + ' · ' + selectedJobItem.status + ' · attempt ' + selectedJobItem.attemptCount + '/' + selectedJobItem.maxAttempts : 'Select a job below.'}</p>
          <label>Administrative reason<textarea name="reason" minLength={8} rows={3} required /></label>
          <button className="primary" type="submit" disabled={!canWrite || !selectedJobItem || !['PENDING','DEAD_LETTER'].includes(selectedJobItem.status) || Boolean(busy)}>{selectedJobItem?.status === 'DEAD_LETTER' ? 'Retry dead-letter job' : 'Cancel pending job'}</button>
        </form>
      </section>

      <section className="card">
        <h2>Durable jobs</h2>
        {jobs.length === 0 ? <div className="empty-state"><strong>No jobs</strong><p>The durable queue is empty.</p></div> : (
          <div className="table-wrap"><table><thead><tr><th>Job</th><th>Status</th><th>Attempts</th><th>Lease / error</th><th>Updated</th></tr></thead><tbody>{jobs.map((job) => <tr key={job.id} className={selectedJob === job.id ? 'selected-row' : undefined} onClick={() => setSelectedJob(job.id)}><td><strong>{job.type}</strong><small>{job.id}</small></td><td><StatusBadge status={job.status} /></td><td>{job.attemptCount}/{job.maxAttempts}</td><td>{job.leaseOwner || job.lastErrorCode || '—'}</td><td>{new Date(job.updatedAt).toLocaleString()}</td></tr>)}</tbody></table></div>
        )}
      </section>

      <section className="card">
        <h2>Incidents</h2>
        {incidents.length === 0 ? <div className="empty-state"><strong>No incidents</strong><p>No governed operational incidents are open in the current inventory.</p></div> : (
          <div className="table-wrap"><table><thead><tr><th>Severity</th><th>Incident</th><th>Status</th><th>Scope</th><th>Updated</th></tr></thead><tbody>{incidents.map((incident) => <tr key={incident.id}><td><StatusBadge status={incident.severity} /></td><td><strong>{incident.summary}</strong><small>{incident.category}</small></td><td><StatusBadge status={incident.status} /></td><td>{incident.campaignId || incident.senderSessionId || 'Platform'}</td><td>{new Date(incident.updatedAt).toLocaleString()}</td></tr>)}</tbody></table></div>
        )}
      </section>
    </div>
  );
}
