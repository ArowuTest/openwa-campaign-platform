'use client';

import Link from 'next/link';
import { useEffect, useState } from 'react';

import { apiRequest } from '../lib/api';

type Dashboard = {
  generatedAt: string;
  campaigns: Record<string, number>;
  recipients: Record<string, number>;
  senders: Record<string, number>;
  openIncidents: number;
  criticalIncidents: number;
  unknownOutcomes: number;
  queueDepth: number;
  oldestQueuedAt?: string;
  staleWorkerNodes: number;
  unavailableGatewayNodes: number;
  unhealthySenderSessions: number;
  campaignsAtRisk: number;
  capacityShortfallPools: number;
  reconciliationBacklog: number;
};

function sum(values: Record<string, number> | undefined, keys: string[]) {
  if (!values) return 0;
  return keys.reduce((total, key) => total + Number(values[key] ?? 0), 0);
}

export function DashboardConsole() {
  const [dashboard, setDashboard] = useState<Dashboard>();
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    void apiRequest<Dashboard>('/v1/operations/dashboard')
      .then((value) => {
        if (cancelled) return;
        setDashboard(value);
        setError('');
      })
      .catch((cause) => {
        if (!cancelled) setError(cause instanceof Error ? cause.message : 'Operational dashboard is unavailable.');
      });
    return () => { cancelled = true; };
  }, []);

  const activeCampaigns = sum(dashboard?.campaigns, ['SCHEDULED', 'DISPATCHING', 'PAUSED']);
  const healthySenders = Number(dashboard?.senders?.READY ?? 0);

  return (
    <div className="workspace-stack">
      <div className="toolbar">
        <div>
          <h1>Operations dashboard</h1>
          <p className="muted">Authoritative campaign, queue, reconciliation and sender health for the internal control plane.</p>
        </div>
        <div className="toolbar-actions">
          <Link className="secondary link-button" href="/operations">Open operations</Link>
          <Link className="primary link-button" href="/campaigns">Create or manage campaign</Link>
        </div>
      </div>

      {error ? <div className="alert alert-danger" role="alert">{error}</div> : null}

      <section className="grid grid-4" aria-label="Operational metrics">
        <article className="card metric"><span className="muted">Active campaigns</span><strong>{activeCampaigns}</strong></article>
        <article className="card metric"><span className="muted">Queue depth</span><strong>{dashboard?.queueDepth ?? '—'}</strong></article>
        <article className="card metric"><span className="muted">READY sender sessions</span><strong>{healthySenders}</strong></article>
        <article className="card metric"><span className="muted">UNKNOWN outcomes</span><strong>{dashboard?.unknownOutcomes ?? '—'}</strong></article>
      </section>

      <section className="grid grid-2">
        <article className="card">
          <div className="section-heading">
            <div><h2>Release attention</h2><p className="muted">Signals that can block or degrade governed campaign execution.</p></div>
          </div>
          <div className="gate-list">
            <div><span>Campaigns at risk</span><strong>{dashboard?.campaignsAtRisk ?? '—'}</strong></div>
            <div><span>Capacity-shortfall pools</span><strong>{dashboard?.capacityShortfallPools ?? '—'}</strong></div>
            <div><span>Unavailable gateway nodes</span><strong>{dashboard?.unavailableGatewayNodes ?? '—'}</strong></div>
            <div><span>Unhealthy sender sessions</span><strong>{dashboard?.unhealthySenderSessions ?? '—'}</strong></div>
            <div><span>Critical incidents</span><strong>{dashboard?.criticalIncidents ?? '—'}</strong></div>
          </div>
        </article>

        <article className="card">
          <div className="section-heading">
            <div><h2>Reconciliation</h2><p className="muted">UNKNOWN is evidence-led and never automatically resent through another route.</p></div>
          </div>
          <div className="gate-list">
            <div><span>UNKNOWN outcomes</span><strong>{dashboard?.unknownOutcomes ?? '—'}</strong></div>
            <div><span>Reconciliation backlog</span><strong>{dashboard?.reconciliationBacklog ?? '—'}</strong></div>
            <div><span>Open incidents</span><strong>{dashboard?.openIncidents ?? '—'}</strong></div>
            <div><span>Stale worker nodes</span><strong>{dashboard?.staleWorkerNodes ?? '—'}</strong></div>
          </div>
          <Link className="text-link dashboard-deep-link" href="/operations">Review operational evidence →</Link>
        </article>
      </section>

      <section className="card">
        <div className="section-heading">
          <div>
            <h2>Campaign state</h2>
            <p className="muted">{dashboard ? 'Snapshot generated ' + new Date(dashboard.generatedAt).toLocaleString() : 'Loading live control-plane snapshot…'}</p>
          </div>
        </div>
        <div className="mini-metric-grid">
          {Object.entries(dashboard?.campaigns ?? {}).map(([status, count]) => (
            <div className="metric-tile" key={status}><span>{status.replaceAll('_', ' ')}</span><strong>{count.toLocaleString()}</strong></div>
          ))}
          {!dashboard ? <div className="metric-tile"><span>Waiting for control API</span><strong>…</strong></div> : null}
        </div>
      </section>
    </div>
  );
}
