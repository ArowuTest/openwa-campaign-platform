'use client';

import Link from 'next/link';
import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';

import { StatusBadge } from '../../../components/status-badge';
import { CampaignDraftEditor } from './campaign-draft-editor';
import { useAuth } from '../../../components/auth-provider';
import { APIError, apiRequest, type ListEnvelope } from '../../../lib/api';
import { getCampaignDetail, type CampaignDetail } from '../../../lib/campaign-preparation-model';
import { hasPermission } from '../../../lib/session';

type Campaign = CampaignDetail;

type Workspace = {
  campaignId: string;
  tags: string[];
  archive: { archived: boolean; version: number; reason?: string };
  notes?: Array<{ id: string; category: string; body: string; createdAt: string }>;
};

type TestSend = {
  id: string;
  campaignId: string;
  messageVersionId: string;
  messageContentHash: string;
  gatewayPoolId: string;
  gatewayPoolVersion: number;
  senderPoolId?: string;
  provider: string;
  engine: string;
  senderSessionId: string;
  routeReference: string;
  status: string;
  createdAt: string;
};

type RoutingPlan = {
  id: string;
  version: number;
  routingPolicyVersion: string;
  capacityEvidenceVersion: string;
  fallbackMode: string;
  routes: Array<{
    senderPoolId: string;
    gatewayPoolId?: string;
    provider: string;
    engine: string;
    providerAdapterVersion?: string;
    providerDefinitionId?: string;
    providerDefinitionVersion?: number;
    gatewayPoolVersion?: number;
    reservedMessagesPerMinute: number;
    reservedHourlyUnits: number;
    reservedDailyUnits: number;
  }>;
};

type Reservation = {
  id: string;
  senderPoolId: string;
  status: string;
  fencingVersion: number;
  reservationStart: string;
  reservationEnd: string;
  reservedMessagesPerMinute: number;
  reservedHourlyUnits: number;
  reservedDailyUnits: number;
};

type Metrics = Record<string, number>;

function isStepUp(error: unknown) {
  return error instanceof APIError && error.status === 403 && error.code.includes('STEP_UP');
}

function numberValue(data: FormData, name: string) {
  return Number(String(data.get(name) ?? '0'));
}

export function CampaignWorkspace({ campaignId }: { campaignId: string }) {
  const { session } = useAuth();
  // Campaign and authorisation changes start a fresh state/request generation.
  // Old mutation promises may still settle, but their unmounted owner cannot
  // reload, navigate, or update the new generation's status and busy state.
  const generation = JSON.stringify([campaignId, session?.id, session?.sessionId, [...(session?.permissions ?? [])].sort()]);
  return <CampaignWorkspaceGeneration key={generation} campaignId={campaignId} />;
}

function CampaignWorkspaceGeneration({ campaignId }: { campaignId: string }) {
  const router = useRouter();
  const { session } = useAuth();
  const [campaign, setCampaign] = useState<Campaign>();
  const [workspace, setWorkspace] = useState<Workspace>();
  const [metrics, setMetrics] = useState<Metrics>();
  const [tests, setTests] = useState<TestSend[]>([]);
  const [plan, setPlan] = useState<RoutingPlan>();
  const [reservations, setReservations] = useState<Reservation[]>([]);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');

  const [coreError, setCoreError] = useState<{ campaignId: string; message: string }>();
  const [supplementalState, setSupplementalState] = useState('Loading supplemental evidence…');
  const activeRequest = useRef<AbortController | undefined>(undefined);
  const mounted = useRef(false);
  const activeMutation = useRef<symbol | undefined>(undefined);
  const canRead = hasPermission(session?.permissions, 'campaign.read');

  const canApprove = hasPermission(session?.permissions, 'campaign.approve');
  const canOperate = hasPermission(session?.permissions, 'campaign.operate');
  const canWrite = hasPermission(session?.permissions, 'campaign.write');

  const fetchSnapshot = useCallback(async (controller: AbortController) => {
    const signal = controller.signal;
    const isCurrent = () => mounted.current && activeRequest.current === controller && !signal.aborted;
    const current = await getCampaignDetail(campaignId, signal);
    if (!isCurrent()) return;
    // Core detail is independently authoritative; optional evidence cannot block it.
    setCampaign(current);
    setCoreError(undefined);
    const base = '/v1/campaigns/' + encodeURIComponent(campaignId);
    const settled = await Promise.allSettled([
      apiRequest<Workspace>(base + '/workspace', { signal }),
      apiRequest<Metrics>(base + '/metrics', { signal }),
      apiRequest<ListEnvelope<TestSend>>(base + '/test-messages?limit=100', { signal }),
      apiRequest<ListEnvelope<RoutingPlan>>(base + '/routing-plans?limit=20', { signal })
    ]);
    if (!isCurrent()) return;
    const unavailable = ['workspace', 'metrics', 'controlled tests', 'routing plans'].filter((_, index) => settled[index].status === 'rejected');
    const workspaceValue = settled[0].status === 'fulfilled' ? settled[0].value : undefined;
    const metricValue = settled[1].status === 'fulfilled' ? settled[1].value : undefined;
    const testValues = settled[2].status === 'fulfilled' ? settled[2].value.items ?? [] : [];
    const latestPlan = settled[3].status === 'fulfilled' ? settled[3].value.items?.[0] : undefined;
    let reservationValues: Reservation[] = [];
    if (latestPlan) {
      try {
        reservationValues = (await apiRequest<ListEnvelope<Reservation>>('/v1/routing-plans/' + encodeURIComponent(latestPlan.id) + '/reservations', { signal })).items ?? [];
      } catch {
        unavailable.push('reservations');
      }
    }
    if (!isCurrent()) return;
    setWorkspace(workspaceValue);
    setMetrics(metricValue);
    setTests(testValues);
    setPlan(latestPlan);
    setReservations(reservationValues);
    setSupplementalState(unavailable.length ? 'Supplemental evidence unavailable: ' + unavailable.join(', ') + '.' : '');
  }, [campaignId]);

  const load = useCallback(async () => {
    // Validate ownership before aborting a controller or changing any state.
    if (!mounted.current) return;
    activeRequest.current?.abort();
    const controller = new AbortController();
    activeRequest.current = controller;
    setCampaign(undefined);
    setWorkspace(undefined);
    setMetrics(undefined);
    setTests([]);
    setPlan(undefined);
    setReservations([]);
    setCoreError(undefined);
    setMessage('');
    setSupplementalState('Loading supplemental evidence…');
    if (!canRead) {
      setCoreError({ campaignId, message: 'Campaign detail is unavailable because campaign.read permission is required.' });
      return;
    }
    try {
      await fetchSnapshot(controller);
    } catch (cause) {
      if (!mounted.current || activeRequest.current !== controller || controller.signal.aborted) return;
      setCampaign(undefined);
      const failure = cause instanceof APIError && cause.status === 404
        ? 'Campaign was not found.'
        : cause instanceof APIError && (cause.status === 401 || cause.status === 403)
          ? 'Campaign detail is unavailable because access was denied.'
          : 'Campaign detail could not be loaded.';
      setCoreError({ campaignId, message: failure });
    }
  }, [campaignId, canRead, fetchSnapshot]);

  useEffect(() => {
    mounted.current = true;
    let cancelled = false;
    // Start the external read in an async callback; rendering already masks the
    // previous campaign context before state is cleared by the new request.
    void Promise.resolve().then(() => { if (!cancelled) return load(); });
    return () => {
      cancelled = true;
      mounted.current = false;
      activeMutation.current = undefined;
      activeRequest.current?.abort();
    };
  }, [load]);

  const acceptedExactTest = useMemo(() => {
    if (!campaign) return undefined;
    return tests.find((test) =>
      test.status === 'ACCEPTED' &&
      test.messageVersionId === campaign.messageVersionId &&
      test.messageContentHash === campaign.messageContentHash &&
      test.gatewayPoolId === campaign.transport.gatewayPoolId &&
      test.gatewayPoolVersion === campaign.transport.gatewayPoolVersion &&
      test.senderPoolId === campaign.transport.senderPoolId &&
      test.provider === campaign.transport.provider &&
      test.engine === campaign.transport.engine
    );
  }, [campaign, tests]);

  const heldReservation = reservations.find((item) => item.status === 'HELD');
  const activeReservation = reservations.find((item) => item.status === 'ACTIVE');

  function requireStepUp(error: unknown) {
    if (!isStepUp(error)) return false;
    router.push('/step-up?returnTo=' + encodeURIComponent('/campaigns/' + campaignId));
    return true;
  }

  async function mutate(label: string, work: () => Promise<unknown>) {
    if (!mounted.current || activeMutation.current) return;
    const mutation = Symbol(label);
    activeMutation.current = mutation;
    const isCurrent = () => mounted.current && activeMutation.current === mutation;
    setBusy(label);
    setMessage('');
    try {
      await work();
      if (!isCurrent()) return;
      await load();
      if (!isCurrent()) return;
      setMessage(label + ' completed. Server evidence has been refreshed.');
    } catch (cause) {
      if (!isCurrent()) return;
      if (!requireStepUp(cause)) setMessage(cause instanceof Error ? cause.message : label + ' failed.');
    } finally {
      if (isCurrent()) {
        activeMutation.current = undefined;
        setBusy('');
      }
    }
  }

  async function transition(action: string, reason: string, extra: Record<string, unknown> = {}) {
    if (!campaign) return;
    await mutate(action, () => apiRequest('/v1/campaigns/' + campaign.id + '/transition', {
      method: 'POST',
      body: JSON.stringify({ action, expectedVersion: campaign.version, reason, ...extra })
    }));
  }

  async function release() {
    if (!campaign) return;
    await mutate('Release recipients', () => apiRequest('/v1/campaigns/' + campaign.id + '/release', {
      method: 'POST',
      body: JSON.stringify({ expectedVersion: campaign.version })
    }));
  }

  async function execute(action: 'start' | 'pause' | 'resume' | 'cancel') {
    if (!campaign) return;
    await mutate(action.charAt(0).toUpperCase() + action.slice(1) + ' campaign', () => apiRequest('/v1/campaigns/' + campaign.id + '/execution/' + action, {
      method: 'POST',
      body: JSON.stringify({ expectedVersion: campaign.version, reason: 'Operator action from governed campaign workspace' })
    }));
  }

  async function createRoutingPlan(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!campaign) return;
    const data = new FormData(event.currentTarget);
    const route = {
      senderPoolId: campaign.transport.senderPoolId,
      gatewayPoolId: campaign.transport.gatewayPoolId,
      provider: campaign.transport.provider,
      engine: campaign.transport.engine,
      allocationWeight: 100,
      maximumRecipients: campaign.maximumUniqueRecipients,
      reservedMessagesPerMinute: numberValue(data, 'mpm'),
      reservedHourlyUnits: numberValue(data, 'hourly'),
      reservedDailyUnits: numberValue(data, 'daily'),
      allowReallocationIn: false,
      allowReallocationOut: false
    };
    await mutate('Create exact-route pilot hold', () => apiRequest('/v1/campaigns/' + campaign.id + '/routing-plans', {
      method: 'POST',
      body: JSON.stringify({
        distributionMode: 'WEIGHTED',
        routes: [route],
        routingPolicyVersion: campaign.transport.routingPolicyVersion,
        capacityEvidenceVersion: campaign.transport.capacityEvidenceVersion,
        pacingPolicyVersion: String(data.get('pacingPolicyVersion') ?? '').trim(),
        fallbackMode: 'NONE',
        idempotencyKey: crypto.randomUUID()
      })
    }));
  }

  async function sendControlledTest(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!campaign?.messageVersionId) return;
    const data = new FormData(event.currentTarget);
    await mutate('Schedule controlled test', () => apiRequest('/v1/campaigns/' + campaign.id + '/test-messages', {
      method: 'POST',
      body: JSON.stringify({
        messageVersionId: campaign.messageVersionId,
        testRecipientId: String(data.get('testRecipientId') ?? '').trim(),
        gatewayPoolId: campaign.transport.gatewayPoolId,
        senderPoolId: campaign.transport.senderPoolId,
        provider: campaign.transport.provider,
        engine: campaign.transport.engine,
        senderSessionId: String(data.get('senderSessionId') ?? '').trim(),
        variableValues: {},
        reason: String(data.get('reason') ?? '').trim(),
        idempotencyKey: crypto.randomUUID()
      })
    }));
  }

  if (!canRead || !campaign || campaign.id !== campaignId) {
    const failure = !canRead
      ? 'Campaign detail is unavailable because campaign.read permission is required.'
      : coreError?.campaignId === campaignId ? coreError.message : undefined;
    return <div className="card">{failure ? <div className="alert alert-danger" role="alert">{failure}</div> : <p className="muted" role="status">Loading authoritative campaign evidence…</p>}</div>;
  }

  const routeSummary = campaign.transport.provider + ' / ' + campaign.transport.engine;

  return (
    <div className="workspace-stack">
      <section className="card workspace-hero">
        <div>
          <Link className="text-link" href="/campaigns">← Campaign register</Link>
          <div className="workspace-title-row">
            <div><h2>{campaign.name}</h2><small>{campaign.id}</small></div>
            <StatusBadge status={campaign.status} />
          </div>
          <p className="muted">{routeSummary} · {campaign.transport.routingMode} · fallback {campaign.transport.fallbackMode}</p>
        </div>
        <div className="evidence-strip" aria-label="Pilot admission evidence">
          <span className={acceptedExactTest ? 'evidence-chip evidence-ok' : 'evidence-chip'}>Controlled test {acceptedExactTest ? 'accepted' : 'required'}</span>
          <span className={heldReservation || activeReservation ? 'evidence-chip evidence-ok' : 'evidence-chip'}>Capacity hold {activeReservation ? 'active' : heldReservation ? 'held' : 'required'}</span>
          <span className={plan?.routes?.length === 1 ? 'evidence-chip evidence-ok' : 'evidence-chip'}>Exact route {plan?.routes?.length === 1 ? 'bound' : 'required'}</span>
        </div>
      </section>

      {supplementalState ? <div className="alert alert-information" role="status">{supplementalState}</div> : null}

      {message ? <div className={message.includes('failed') || message.includes('required') ? 'alert alert-danger' : 'alert alert-information'} role="status">{message}</div> : null}

      <CampaignDraftEditor campaign={campaign} onSaved={saved => { if (mounted.current) setCampaign(saved); }} />

      <section className="grid grid-4" aria-label="Campaign metrics">
        <article className="card metric"><span className="muted">Campaign version</span><strong>{campaign.version}</strong></article>
        <article className="card metric"><span className="muted">Eligible audience</span><strong>{campaign.eligibleAudienceCount.toLocaleString()}</strong></article>
        <article className="card metric"><span className="muted">Unknown outcomes</span><strong>{metrics ? Number(metrics.unknownTotal ?? metrics.unknown ?? 0).toLocaleString() : 'Unavailable'}</strong></article>
        <article className="card metric"><span className="muted">Workspace notes</span><strong>{workspace?.notes?.length ?? 0}</strong></article>
      </section>

      <section className="grid grid-2">
        <article className="card">
          <div className="section-heading"><div><h2>Authoritative route</h2><p className="muted">Day one permits one OpenWA engine-specific pool with multiple READY sessions inside it.</p></div></div>
          <dl className="definition-list">
            <div><dt>Provider / engine</dt><dd>{routeSummary}</dd></div>
            <div><dt>Sender pool</dt><dd>{campaign.transport.senderPoolId || 'Not bound'}</dd></div>
            <div><dt>Gateway pool</dt><dd>{campaign.transport.gatewayPoolId || 'Not bound'}</dd></div>
            <div><dt>Adapter</dt><dd>{campaign.transport.adapterVersion}</dd></div>
            <div><dt>Routing policy</dt><dd>{campaign.transport.routingPolicyVersion}</dd></div>
            <div><dt>Capacity evidence</dt><dd>{campaign.transport.capacityEvidenceVersion}</dd></div>
          </dl>
        </article>

        <article className="card">
          <h2>Pilot gate</h2>
          <div className="gate-list">
            <div><span>Accepted exact-route controlled test</span><strong>{acceptedExactTest ? 'PASS' : 'OPEN'}</strong></div>
            <div><span>Single routing plan route</span><strong>{plan?.routes?.length === 1 ? 'PASS' : 'OPEN'}</strong></div>
            <div><span>Server capacity reservation</span><strong>{heldReservation ? 'HELD' : activeReservation ? 'ACTIVE' : 'OPEN'}</strong></div>
            <div><span>Fallback mode</span><strong>{campaign.transport.fallbackMode}</strong></div>
          </div>
          <p className="muted">These indicators are explanatory. Final approval, release and execution are enforced again by the server.</p>
        </article>
      </section>

      <section className="grid grid-2">
        <form className="card form-stack" onSubmit={createRoutingPlan}>
          <h2>Create Day‑1 capacity hold</h2>
          <p className="muted">Creates one server-issued HELD reservation for the campaign’s exact sender/gateway route. Use measured, approved values only.</p>
          <label>Reserved messages per minute<input name="mpm" type="number" min="1" required /></label>
          <label>Reserved hourly units<input name="hourly" type="number" min="1" required /></label>
          <label>Reserved daily units<input name="daily" type="number" min="1" required /></label>
          <label>Pacing policy version<input name="pacingPolicyVersion" required /></label>
          <button className="primary" type="submit" disabled={!canApprove || Boolean(busy) || Boolean(heldReservation) || Boolean(activeReservation)}>Create governed hold</button>
        </form>

        <form className="card form-stack" onSubmit={sendControlledTest}>
          <h2>Controlled test</h2>
          <p className="muted">The server validates the approved test recipient and a live READY session on the exact campaign route.</p>
          <label>Approved test-recipient ID<input name="testRecipientId" required /></label>
          <label>READY sender-session ID<input name="senderSessionId" required /></label>
          <label>Reason<textarea name="reason" minLength={8} rows={3} required /></label>
          <button className="primary" type="submit" disabled={!canWrite || !campaign.messageVersionId || Boolean(busy)}>Send controlled test</button>
          {!campaign.messageVersionId ? <small>An approved campaign message version is required first.</small> : null}
        </form>
      </section>

      <section className="card">
        <div className="section-heading">
          <div><h2>Governance & execution</h2><p className="muted">Actions use optimistic campaign versions and server-side evidence checks.</p></div>
          {busy ? <span className="pill">{busy}…</span> : null}
        </div>
        <div className="action-grid">
          {campaign.status === 'COMMERCIAL_APPROVED' && canApprove ? <button className="secondary" type="button" onClick={() => void transition('REQUEST_FINAL_APPROVAL', 'Ready for final campaign approval')}>Request final approval</button> : null}
          {campaign.status === 'FINAL_APPROVAL_PENDING' && canApprove ? <button className="primary" type="button" onClick={() => void transition('APPROVE_FINAL', 'Approved after controlled test and capacity review')}>Final approve</button> : null}
          {campaign.status === 'SCHEDULED' && canOperate ? <button className="secondary" type="button" onClick={() => void release()}>Release recipients</button> : null}
          {campaign.status === 'SCHEDULED' && canOperate ? <button className="primary" type="button" onClick={() => void execute('start')}>Start dispatch</button> : null}
          {campaign.status === 'DISPATCHING' && canOperate ? <button className="secondary" type="button" onClick={() => void execute('pause')}>Pause</button> : null}
          {campaign.status === 'PAUSED' && canOperate ? <button className="primary" type="button" onClick={() => void execute('resume')}>Resume</button> : null}
          {['DRAFT','CONSENT_REVIEW_PENDING','CONSENT_APPROVED','AUDIENCE_BUILDING','AUDIENCE_VALIDATED','MESSAGE_REVIEW_PENDING','MESSAGE_APPROVED','COMMERCIAL_APPROVED','FINAL_APPROVAL_PENDING','SCHEDULED','PAUSED'].includes(campaign.status) && canOperate ? <button className="danger-ghost" type="button" onClick={() => void execute('cancel')}>Cancel campaign</button> : null}
        </div>
      </section>

      <section className="grid grid-2">
        <article className="card">
          <h2>Controlled-test evidence</h2>
          {tests.length === 0 ? <div className="empty-state"><strong>No controlled tests</strong><p>Run a test against an approved test recipient before final approval.</p></div> : (
            <div className="table-wrap" tabIndex={0} aria-label="Controlled-test evidence table"><table><thead><tr><th>Status</th><th>Session</th><th>Route</th><th>Created</th></tr></thead><tbody>{tests.map((test) => <tr key={test.id}><td><StatusBadge status={test.status} /></td><td>{test.senderSessionId}</td><td>{test.provider} / {test.engine}<small>{test.senderPoolId}</small></td><td>{new Date(test.createdAt).toLocaleString()}</td></tr>)}</tbody></table></div>
          )}
        </article>
        <article className="card">
          <h2>Capacity reservation evidence</h2>
          {reservations.length === 0 ? <div className="empty-state"><strong>No reservation</strong><p>Create the exact-route pilot hold before final approval.</p></div> : (
            <div className="table-wrap" tabIndex={0} aria-label="Capacity reservation evidence table"><table><thead><tr><th>Status</th><th>Pool</th><th>MPM</th><th>Hourly</th><th>Daily</th></tr></thead><tbody>{reservations.map((item) => <tr key={item.id}><td><StatusBadge status={item.status} /></td><td>{item.senderPoolId}</td><td>{item.reservedMessagesPerMinute}</td><td>{item.reservedHourlyUnits}</td><td>{item.reservedDailyUnits}</td></tr>)}</tbody></table></div>
          )}
        </article>
      </section>
    </div>
  );
}
