'use client';

import Link from 'next/link';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';

import { useAuth } from '../../../../components/auth-provider';
import { StatusBadge } from '../../../../components/status-badge';
import { apiRequest, APIError, type ListEnvelope } from '../../../../lib/api';
import { hasPermission } from '../../../../lib/session';

type Campaign = {
  id: string; name: string; status: string; version: number;
  eligibleAudienceCount: number; updatedAt: string;
};
type Metrics = {
  authorisedTotal: number; queuedTotal: number; submittedTotal: number; gatewayAcceptedTotal: number;
  sentTotal: number; deliveredTotal: number; readTotal: number; failedTotal: number;
  unknownTotal: number; suppressedTotal: number; excludedFinalCheckTotal: number;
};
type CampaignReport = {
  campaignId: string; status: string; generatedAt: string; warnings: string[];
  privacy?: { policyId: string; policyVersion: number };
};
type Finance = {
  campaignId: string; invoiceReference?: string; reconciliationStatus: string;
  approvedRecipients: number; recipientVariance: number; warnings: string[]; generatedAt: string;
};
type DeliveryException = {
  recipientId: string; campaignId: string; status: string; attemptCount: number; updatedAt: string;
};
type CompletionResult = {
  campaign?: Campaign;
  assessment?: { state: 'IN_PROGRESS' | 'READY' | 'READY_WITH_EXCEPTIONS'; outstanding: number; failed: number; unknown: number };
};

const fields: (keyof Metrics)[] = [
  'authorisedTotal', 'queuedTotal', 'submittedTotal', 'gatewayAcceptedTotal', 'sentTotal',
  'deliveredTotal', 'readTotal', 'failedTotal', 'unknownTotal', 'suppressedTotal', 'excludedFinalCheckTotal'
];
const permittedStatuses = ['DISPATCHING', 'PAUSED'];
const terminalStatuses = ['COMPLETED', 'COMPLETED_WITH_EXCEPTIONS'];
function amount(value: number | undefined) { return Number(value ?? 0).toLocaleString(); }
function date(value?: string) {
  if (!value) return 'Not recorded';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? 'Unavailable' : parsed.toLocaleString();
}
function safeError(value: unknown) {
  return value instanceof Error ? value.message : 'The server evidence request failed.';
}
function stepUp(value: unknown) {
  return value instanceof APIError && value.status === 403 && value.code.includes('STEP_UP');
}
function masked(value: string) { return '…' + String(value).slice(-6); }

export function CompletionReconciliationPanel({ campaignId }: { campaignId: string }) {
  const { session } = useAuth();
  const allowed = hasPermission(session?.permissions, 'campaign.read');
  const generation = JSON.stringify([campaignId, session?.id, session?.sessionId, [...(session?.permissions ?? [])].sort()]);
  if (!allowed) return <p role="status">Campaign reconciliation requires campaign.read permission.</p>;
  return <CompletionReconciliationGeneration key={generation} campaignId={campaignId} />;
}

function CompletionReconciliationGeneration({ campaignId }: { campaignId: string }) {
  const { session } = useAuth();
  const router = useRouter();
  const canOperate = hasPermission(session?.permissions, 'campaign.operate');
  const canReport = hasPermission(session?.permissions, 'report.read');
  const canFinance = hasPermission(session?.permissions, 'finance.read');
  const canOperations = hasPermission(session?.permissions, 'operations.read');

  const mounted = useRef(false);
  const requestSequence = useRef(0);
  const submitting = useRef(false);
  const [campaign, setCampaign] = useState<Campaign>();
  const [metrics, setMetrics] = useState<Metrics>();
  const [report, setReport] = useState<CampaignReport>();
  const [finance, setFinance] = useState<Finance>();
  const [exceptions, setExceptions] = useState<DeliveryException[]>([]);
  const [moreExceptions, setMoreExceptions] = useState(false);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);
  const [lockedVersion, setLockedVersion] = useState<number>();
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');

  const mfa = useCallback(() => {
    if (mounted.current) router.push('/step-up?returnTo=' +
      encodeURIComponent('/campaigns/' + campaignId + '/reconciliation'));
  }, [campaignId, router]);

  const load = useCallback(async (resetNotice = true): Promise<Campaign | undefined> => {
    const sequence = ++requestSequence.current;
    setLoading(true);
    setError('');
    if (resetNotice) setNotice('');
    setCampaign(undefined);
    setMetrics(undefined);
    setReport(undefined);
    setFinance(undefined);
    setExceptions([]);
    setMoreExceptions(false);
    setWarnings([]);
    try {
      const base = '/v1/campaigns/' + encodeURIComponent(campaignId);
      const detail = await apiRequest<Campaign>(base);
      if (!mounted.current || requestSequence.current !== sequence) return undefined;
      if (!detail || detail.id !== campaignId || !Number.isSafeInteger(detail.version)) {
        throw new Error('The campaign detail is not authoritative.');
      }
      setCampaign(detail);
      setLockedVersion((prior) => prior !== undefined && detail.version > prior ? undefined : prior);
      const names = [
        'delivery metrics',
        ...(canReport ? ['campaign report'] : []),
        ...(canFinance ? ['financial reconciliation'] : []),
        ...(canOperations ? ['delivery exceptions'] : [])
      ];
      const tasks: Array<Promise<unknown>> = [
        apiRequest<Metrics>(base + '/metrics'),
        ...(canReport ? [apiRequest<CampaignReport>(base + '/report')] : []),
        ...(canFinance ? [apiRequest<Finance>(base + '/financial-reconciliation')] : []),
        ...(canOperations ? [apiRequest<ListEnvelope<DeliveryException>>(
          '/v1/operations/delivery-exceptions?campaignId=' + encodeURIComponent(campaignId) + '&limit=50'
        )] : [])
      ];
      const results = await Promise.allSettled(tasks);
      if (!mounted.current || requestSequence.current !== sequence) return undefined;
      const missing: string[] = [];
      for (let i = 0; i < names.length; i++) {
        const result = results[i];
        if (result.status === 'rejected') {
          missing.push(names[i] + ': ' + safeError(result.reason));
          continue;
        }
        if (names[i] === 'delivery metrics') {
          const value = result.value as Metrics;
          if (!value || fields.some((key) => !Number.isSafeInteger(value[key]) || value[key] < 0)) {
            missing.push('delivery metrics: invalid authoritative counters');
          } else setMetrics(value);
        } else if (names[i] === 'campaign report') {
          const value = result.value as CampaignReport;
          if (value.campaignId !== campaignId) missing.push('campaign report: campaign mismatch');
          else setReport(value);
        } else if (names[i] === 'financial reconciliation') {
          const value = result.value as Finance;
          if (value.campaignId !== campaignId) missing.push('financial reconciliation: campaign mismatch');
          else setFinance(value);
        } else {
          const page = result.value as ListEnvelope<DeliveryException>;
          if (!page || !Array.isArray(page.items) || typeof page.hasMore !== 'boolean') {
            missing.push('delivery exceptions: invalid server page');
          } else {
            setExceptions(page.items.filter((value) => value.campaignId === campaignId).map((value) => ({
              recipientId: value.recipientId, campaignId: value.campaignId, status: value.status,
              attemptCount: value.attemptCount, updatedAt: value.updatedAt
            })));
            setMoreExceptions(page.hasMore);
          }
        }
      }
      setWarnings(missing);
      return detail;
    } catch (cause) {
      if (!mounted.current || requestSequence.current !== sequence) return undefined;
      if (stepUp(cause)) mfa(); else setError(safeError(cause));
      return undefined;
    } finally {
      if (mounted.current && requestSequence.current === sequence) setLoading(false);
    }
  }, [campaignId, canReport, canFinance, canOperations, mfa]);

  useEffect(() => {
    mounted.current = true;
    // Initial read synchronises independent, server-authoritative evidence.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
    return () => {
      mounted.current = false;
      requestSequence.current += 1;
    };
  }, [load]);

  const active = metrics
    ? metrics.authorisedTotal + metrics.queuedTotal + metrics.submittedTotal + metrics.gatewayAcceptedTotal
    : 0;
  const accounted = metrics ? fields.reduce((sum, key) => sum + metrics[key], 0) : 0;
  const balanced = Boolean(metrics && campaign && campaign.eligibleAudienceCount > 0 &&
    accounted === campaign.eligibleAudienceCount);
  const preliminaryReady = Boolean(campaign && metrics && permittedStatuses.includes(campaign.status) &&
    balanced && active === 0 && !loading && lockedVersion === undefined);
  const withExceptions = Boolean(metrics && (metrics.failedTotal > 0 || metrics.unknownTotal > 0));

  async function complete() {
    if (!canOperate || !preliminaryReady || !acknowledged || !campaign || busy || submitting.current) return;
    submitting.current = true;
    const source = campaign;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const outcome = await apiRequest<CompletionResult>(
        '/v1/campaigns/' + encodeURIComponent(campaignId) + '/execution/complete',
        { method: 'POST', body: JSON.stringify({
          expectedVersion: source.version, reason: 'Operator requested a governed completion assessment'
        }) }
      );
      if (!mounted.current) return;
      const next = outcome?.campaign;
      const assessment = outcome?.assessment;
      const expectedStatus = assessment?.state === 'READY' ? 'COMPLETED' :
        assessment?.state === 'READY_WITH_EXCEPTIONS' ? 'COMPLETED_WITH_EXCEPTIONS' : '';
      if (!next || next.id !== campaignId || next.version <= source.version ||
          !terminalStatuses.includes(next.status) || next.status !== expectedStatus) {
        setLockedVersion(source.version);
        setNotice('Server assessed the campaign as still in progress or unconfirmed. No completion has been confirmed by this screen. Do not retry until evidence is reconciled.');
        return;
      }
      const verified = await apiRequest<Campaign>('/v1/campaigns/' + encodeURIComponent(campaignId));
      if (!mounted.current) return;
      if (!verified || verified.id !== campaignId || verified.version < next.version ||
          verified.status !== expectedStatus) {
        setLockedVersion(source.version);
        setNotice('Completion outcome unconfirmed by authoritative readback. Do not retry automatically.');
        return;
      }
      setCampaign(verified);
      setAcknowledged(false);
      setNotice('Completion confirmed: ' + expectedStatus.replaceAll('_', ' ') +
        '. This does not authorise resending unresolved delivery outcomes.');
    } catch (cause) {
      if (!mounted.current) return;
      setLockedVersion(source.version);
      if (stepUp(cause)) mfa();
      else setError('Completion outcome unconfirmed. Do not retry this action without authoritative reconciliation. ' + safeError(cause));
    } finally {
      submitting.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  return (
    <div className="workspace-stack">
      {error ? <p role="alert" className="alert alert-danger">{error}</p> : null}
      {notice ? <p role="status" className="alert alert-information">{notice}</p> : null}
      <section className="card form-stack">
        <div className="section-heading">
          <div><h2>Campaign close-out evidence</h2>
            <p className="muted">Only the server execution coordinator can confirm campaign completion.</p></div>
          <button type="button" onClick={() => void load()} disabled={loading || busy}>
            {loading ? 'Refreshing…' : 'Refresh evidence'}
          </button>
        </div>
        <div className="button-row">
          <Link href={'/campaigns/' + encodeURIComponent(campaignId)}>Campaign workspace</Link>
          {canReport ? <Link href="/reports">Reports and governed exports</Link> : null}
          {canOperations ? <Link href="/operations">Exception review</Link> : null}
        </div>
        {loading ? <p role="status">Loading campaign and reconciliation evidence…</p> : null}
        {campaign ? <dl className="definition-list">
          <div><dt>Campaign</dt><dd>{campaign.name}</dd></div>
          <div><dt>Status</dt><dd><StatusBadge status={campaign.status}/></dd></div>
          <div><dt>Campaign version</dt><dd>{campaign.version}</dd></div>
          <div><dt>Eligible recipients</dt><dd>{amount(campaign.eligibleAudienceCount)}</dd></div>
          <div><dt>Updated at</dt><dd>{date(campaign.updatedAt)}</dd></div>
        </dl> : !loading && !error ? <p>No campaign evidence is available.</p> : null}
      </section>
      {metrics ? <section className="grid grid-2">
        <article className="card">
          <h2>Delivery obligation accounting</h2>
          <p><strong>{amount(accounted)} accounted recipients</strong> against {amount(campaign?.eligibleAudienceCount)} expected.</p>
          {active > 0
            ? <p role="alert">{amount(active)} active delivery obligations remain. Completion cannot be requested.</p>
            : <p>No active delivery obligations in the current metrics snapshot.</p>}
          {!balanced ? <p role="alert">Delivery totals are provisional or do not reconcile with the eligible audience; completion is blocked.</p>
            : <p>The delivery totals match this campaign's eligible audience.</p>}
          <dl className="definition-list">
            <div><dt>Delivered</dt><dd>{amount(metrics.deliveredTotal)}</dd></div>
            <div><dt>Read</dt><dd>{amount(metrics.readTotal)}</dd></div>
            <div><dt>Suppressed</dt><dd>{amount(metrics.suppressedTotal)}</dd></div>
            <div><dt>Failed</dt><dd>{amount(metrics.failedTotal)}</dd></div>
            <div><dt>Unknown</dt><dd>{amount(metrics.unknownTotal)}</dd></div>
            <div><dt>Excluded at final check</dt><dd>{amount(metrics.excludedFinalCheckTotal)}</dd></div>
          </dl>
        </article>
        <article className="card">
          <h2>Unresolved delivery outcomes</h2>
          {metrics.unknownTotal > 0 ? <p role="alert">{amount(metrics.unknownTotal)} UNKNOWN outcomes require governed reconciliation.</p> : null}
          {metrics.failedTotal > 0 ? <p>{amount(metrics.failedTotal)} failed outcomes may require investigation.</p> : null}
          <p>UNKNOWN outcomes remain sticky and must not be resent automatically. Completion does not resolve them.</p>
          {withExceptions ? <p className="muted">The server may mark this as COMPLETED_WITH_EXCEPTIONS.</p> : null}
        </article>
      </section> : !loading ? <section className="card"><h2>Delivery metrics unavailable</h2>
        <p role="alert">Completion is disabled without current campaign metrics.</p></section> : null}
      <section className="grid grid-2">
        <article className="card">
          <h2>Privacy-aware campaign report</h2>
          {!canReport ? <p>Campaign report details require report.read permission.</p>
            : report ? <>
              <p>Generated {date(report.generatedAt)}</p>
              <p>Policy {report.privacy?.policyId || 'not available'}, version {report.privacy?.policyVersion ?? 'unknown'}</p>
              {report.warnings?.length ? <ul>{report.warnings.map((w, i) => <li key={i}>{w}</li>)}</ul>
                : <p>No warnings reported in this generated snapshot.</p>}
            </> : <p>{loading ? 'Loading report…' : 'Campaign report unavailable; do not infer zero warnings.'}</p>}
        </article>
        <article className="card">
          <h2>Financial reconciliation</h2>
          {!canFinance ? <p>Financial reconciliation requires finance.read permission.</p>
            : finance ? <>
              <p><StatusBadge status={finance.reconciliationStatus}/></p>
              <dl className="definition-list">
                <div><dt>Approved recipients</dt><dd>{amount(finance.approvedRecipients)}</dd></div>
                <div><dt>Recipient variance</dt><dd>{amount(finance.recipientVariance)}</dd></div>
                <div><dt>Invoice reference</dt><dd>{finance.invoiceReference || 'Not provided'}</dd></div>
              </dl>
              <p>Generated {date(finance.generatedAt)}</p>
              {finance.warnings?.length ? <ul>{finance.warnings.map((w, i) => <li key={i}>{w}</li>)}</ul> : null}
            </> : <p>{loading ? 'Loading finance evidence…' : 'Finance evidence unavailable; do not infer balanced settlement.'}</p>}
        </article>
      </section>
      <section className="card form-stack">
        <h2>Campaign-scoped delivery exceptions</h2>
        {!canOperations ? <p>Delivery exception details require operations.read permission.</p> : <>
          <p className="muted">Server-filtered to this campaign; only the first 50 records are shown. No phone numbers or provider identifiers are displayed.</p>
          {exceptions.length ? <div className="table-wrap"><table>
            <thead><tr><th>Recipient reference</th><th>Status</th><th>Attempts</th><th>Updated</th></tr></thead>
            <tbody>{exceptions.map((item) => <tr key={item.recipientId}>
              <td>{masked(item.recipientId)}</td><td><StatusBadge status={item.status}/></td>
              <td>{item.attemptCount}</td><td>{date(item.updatedAt)}</td>
            </tr>)}</tbody>
          </table></div> : <p>No exceptions on the loaded page. More may exist elsewhere.</p>}
          {moreExceptions ? <p role="status">Additional campaign exceptions exist beyond this page. Use the operational exception register.</p> : null}
        </>}
      </section>
      {warnings.length ? <section className="card"><h2>Unavailable evidence</h2>
        <ul>{warnings.map((w, i) => <li key={i}>{w}</li>)}</ul>
      </section> : null}
      {canOperate ? <section className="card form-stack">
        <h2>Governed campaign completion</h2>
        <p>The server re-assesses terminal recipients before recording completion. This action never sends or resends messages.</p>
        <p>Even if locally balanced, a campaign may remain IN_PROGRESS or complete WITH EXCEPTIONS.</p>
        {lockedVersion !== undefined ? <p role="alert">Completion at version {lockedVersion} is unresolved; obtain newer authoritative evidence before considering another submission.</p> : null}
        {campaign && !permittedStatuses.includes(campaign.status) && !terminalStatuses.includes(campaign.status)
          ? <p>Completion is not available from status {campaign.status}.</p> : null}
        <label className="checkbox-row">
          <input type="checkbox" checked={acknowledged} onChange={(event) => setAcknowledged(event.target.checked)}
            disabled={!preliminaryReady || busy}/>
          <span>I understand completion is assessed and controlled by the server</span>
        </label>
        <button type="button" className="primary" disabled={!preliminaryReady || !acknowledged || busy}
          onClick={() => void complete()}>
          {busy ? 'Assessing completion…' : 'Request server completion assessment'}
        </button>
      </section> : null}
    </div>
  );
}
