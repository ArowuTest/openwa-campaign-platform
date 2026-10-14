'use client';

import Link from 'next/link';
import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';

import { StatusBadge } from '../../../components/status-badge';
import { CampaignDraftEditor } from './campaign-draft-editor';
import { CampaignClonePanel } from './campaign-clone-panel';
import { CampaignReadinessPanel } from './campaign-readiness-panel';
import { CommercialGovernance } from './commercial-governance';
import { MessageComposer } from './message-composer';
import { useAuth } from '../../../components/auth-provider';
import { APIError, apiRequest, collectBoundedPages, type ListEnvelope } from '../../../lib/api';
import { canonicalUUID, getCampaignDetail, type CampaignDetail } from '../../../lib/campaign-preparation-model';
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

type ControlledTestOptions = {
  campaignId: string;
  campaignVersion: number;
  messageVersionId?: string;
  provider: string;
  engine: string;
  senderPoolId?: string;
  gatewayPoolId?: string;
  supported: boolean;
  reason?: string;
  recipients: Array<{ id: string; label: string; maskedMsisdn: string; status: string }>;
  sessions: Array<{ id: string; profileDisplayName: string; maskedMsisdn: string; status: string; version: number }>;
  recipientInventoryComplete: boolean;
  sessionInventoryComplete: boolean;
};

type CapacityHoldProposal = {
  campaignId: string;
  campaignVersion: number;
  senderPoolId: string;
  gatewayPoolId: string;
  provider: string;
  engine: string;
  reservationStart: string;
  reservationEnd: string;
  basisRecipientCount: number;
  measurementAsOf: string;
  healthySessions: number;
  healthyNodes: number;
  minimumHealthyNodes: number;
  availableMessagesPerMinute: number;
  availableHourlyUnits: number;
  availableDailyUnits: number;
  safetyMarginPercent: number;
  requiredMessagesPerMinute: number;
  reservedMessagesPerMinute: number;
  reservedHourlyUnits: number;
  reservedDailyUnits: number;
  pacingPolicyId: string;
  pacingPolicyVersion: number;
  pacingMessagesPerMinute: number;
  pacingHourlyAllowance: number;
  pacingDailyAllowance: number;
  routingPolicyVersion: string;
  capacityEvidenceVersion: string;
  classification: 'READY_TO_HOLD';
};

type Metrics = Record<string, number>;

function optionsMatchCampaign(options: ControlledTestOptions, current: Campaign): boolean {
  return options.campaignId === current.id &&
    options.campaignVersion === current.version &&
    (options.messageVersionId ?? '') === (current.messageVersionId ?? '') &&
    options.provider === current.transport.provider &&
    options.engine === current.transport.engine &&
    (options.senderPoolId ?? '') === (current.transport.senderPoolId ?? '') &&
    (options.gatewayPoolId ?? '') === (current.transport.gatewayPoolId ?? '');
}

function acceptedTestForCampaign(items: TestSend[], current: Campaign): TestSend | undefined {
  return items.find((test) =>
    test.status === 'ACCEPTED' &&
    test.messageVersionId === current.messageVersionId &&
    test.messageContentHash === current.messageContentHash &&
    test.gatewayPoolId === current.transport.gatewayPoolId &&
    test.gatewayPoolVersion === current.transport.gatewayPoolVersion &&
    test.senderPoolId === current.transport.senderPoolId &&
    test.provider === current.transport.provider &&
    test.engine === current.transport.engine
  );
}

function capacityProposalMatchesCampaign(proposal: CapacityHoldProposal, current: Campaign): boolean {
  return proposal.campaignId === current.id &&
    proposal.campaignVersion === current.version &&
    proposal.senderPoolId === current.transport.senderPoolId &&
    proposal.gatewayPoolId === current.transport.gatewayPoolId &&
    proposal.provider === current.transport.provider &&
    proposal.engine === current.transport.engine &&
    proposal.routingPolicyVersion === current.transport.routingPolicyVersion &&
    proposal.capacityEvidenceVersion === current.transport.capacityEvidenceVersion &&
    proposal.classification === 'READY_TO_HOLD';
}

function isStepUp(error: unknown) {
  return error instanceof APIError && error.status === 403 && error.code.includes('STEP_UP');
}

export function CampaignWorkspace({ campaignId }: { campaignId: string }) {
  campaignId = canonicalUUID(campaignId);
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
  const [readinessGeneration, setReadinessGeneration] = useState(0);
  const [workspace, setWorkspace] = useState<Workspace>();
  const [metrics, setMetrics] = useState<Metrics>();
  const [tests, setTests] = useState<TestSend[]>([]);
  const [testOptions, setTestOptions] = useState<ControlledTestOptions>();
  const [capacityProposal, setCapacityProposal] = useState<CapacityHoldProposal>();
  const [plan, setPlan] = useState<RoutingPlan>();
  const [reservations, setReservations] = useState<Reservation[]>([]);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');

  const [coreError, setCoreError] = useState<{ campaignId: string; message: string }>();
  const [supplementalState, setSupplementalState] = useState('Loading supplemental evidence…');
  const activeRequest = useRef<AbortController | undefined>(undefined);
  const mounted = useRef(false);
  const activeMutation = useRef<symbol | undefined>(undefined);
  const capacityHoldRequest = useRef<{ campaignVersion: number; reason: string; id: string } | undefined>(undefined);
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
      collectBoundedPages<TestSend>(
        cursor => apiRequest<ListEnvelope<TestSend>>(
          base + '/test-messages?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : ''),
          { signal }
        ),
        10
      ),
      apiRequest<ListEnvelope<RoutingPlan>>(base + '/routing-plans?limit=20', { signal })
    ]);
    if (!isCurrent()) return;
    const unavailable = ['workspace', 'metrics', 'controlled tests', 'routing plans'].filter((_, index) => settled[index].status === 'rejected');
    const workspaceValue = settled[0].status === 'fulfilled' ? settled[0].value : undefined;
    const metricValue = settled[1].status === 'fulfilled' ? settled[1].value : undefined;
    const testValues = settled[2].status === 'fulfilled' ? settled[2].value : [];
    const latestPlan = settled[3].status === 'fulfilled' ? settled[3].value.items?.[0] : undefined;
    let controlledTestOptions: ControlledTestOptions | undefined;
    if (canWrite && current.messageVersionId) {
      try {
        const value = await apiRequest<ControlledTestOptions>(base + '/test-message-options', { signal });
        if (optionsMatchCampaign(value, current)) {
          controlledTestOptions = value;
        } else {
          unavailable.push('controlled-test options changed with the campaign');
        }
      } catch {
        unavailable.push('controlled-test options');
      }
    }
    let reservationValues: Reservation[] = [];
    if (latestPlan) {
      try {
        reservationValues = (await apiRequest<ListEnvelope<Reservation>>('/v1/routing-plans/' + encodeURIComponent(latestPlan.id) + '/reservations', { signal })).items ?? [];
      } catch {
        unavailable.push('reservations');
      }
    }
    const acceptedCapacityTest = acceptedTestForCampaign(testValues, current);
    const liveReservation = reservationValues.some(item => item.status === 'HELD' || item.status === 'ACTIVE');
    let capacityProposalValue: CapacityHoldProposal | undefined;
    if (acceptedCapacityTest && !liveReservation &&
        ['MESSAGE_APPROVED', 'COMMERCIAL_APPROVED', 'FINAL_APPROVAL_PENDING'].includes(current.status)) {
      try {
        const value = await apiRequest<CapacityHoldProposal>(base + '/capacity-hold-proposal', { signal });
        if (capacityProposalMatchesCampaign(value, current)) {
          capacityProposalValue = value;
        } else {
          unavailable.push('capacity-hold proposal changed with the campaign');
        }
      } catch {
        unavailable.push('capacity-hold proposal');
      }
    }
    if (!isCurrent()) return;
    setWorkspace(workspaceValue);
    setMetrics(metricValue);
    setTests(testValues);
    setTestOptions(controlledTestOptions);
    setCapacityProposal(capacityProposalValue);
    setPlan(latestPlan);
    setReservations(reservationValues);
    setSupplementalState(unavailable.length ? 'Supplemental evidence unavailable: ' + unavailable.join(', ') + '.' : '');
  }, [campaignId, canWrite]);

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
    setTestOptions(undefined);
    setCapacityProposal(undefined);
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

  const acceptedExactTest = useMemo(
    () => campaign ? acceptedTestForCampaign(tests, campaign) : undefined,
    [campaign, tests]
  );

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

  async function confirmCapacityHold(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!campaign || !acceptedExactTest || !capacityProposal || heldReservation || activeReservation ||
        !capacityProposalMatchesCampaign(capacityProposal, campaign)) {
      setMessage('Reload the accepted controlled-test and server-derived capacity proposal before confirming a hold.');
      return;
    }
    const data = new FormData(event.currentTarget);
    const reason = String(data.get('reason') ?? '').trim();
    if (reason.length < 8 || reason.length > 1000) {
      setMessage('Enter a meaningful capacity-hold approval reason (8–1000 characters).');
      return;
    }
    let request = capacityHoldRequest.current;
    if (!request || request.campaignVersion !== campaign.version || request.reason !== reason) {
      request = { campaignVersion: campaign.version, reason, id: crypto.randomUUID() };
      capacityHoldRequest.current = request;
    }
    const clientRequestId = request.id;
    await mutate('Confirm server-derived capacity hold', async () => {
      await apiRequest('/v1/campaigns/' + campaign.id + '/capacity-hold', {
        method: 'POST',
        body: JSON.stringify({
          expectedCampaignVersion: campaign.version,
          reason,
          clientRequestId
        })
      });
      capacityHoldRequest.current = undefined;
    });
  }

  async function sendControlledTest(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!campaign?.messageVersionId || !testOptions?.supported || !optionsMatchCampaign(testOptions, campaign)) {
      setMessage('Reload governed controlled-test options before scheduling a test.');
      return;
    }
    const data = new FormData(event.currentTarget);
    const testRecipientId = String(data.get('testRecipientId') ?? '').trim();
    const senderSessionId = String(data.get('senderSessionId') ?? '').trim();
    if (!testOptions.recipients.some((item) => item.id === testRecipientId) ||
        !testOptions.sessions.some((item) => item.id === senderSessionId)) {
      setMessage('Select an approved recipient and a READY sender session from the current governed options.');
      return;
    }
    await mutate('Schedule controlled test', () => apiRequest('/v1/campaigns/' + campaign.id + '/test-messages', {
      method: 'POST',
      body: JSON.stringify({
        messageVersionId: campaign.messageVersionId,
        testRecipientId,
        gatewayPoolId: campaign.transport.gatewayPoolId,
        senderPoolId: campaign.transport.senderPoolId,
        provider: campaign.transport.provider,
        engine: campaign.transport.engine,
        senderSessionId,
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

      <CampaignClonePanel key={`${campaign.id}:${campaign.version}`} campaign={campaign} canWrite={canWrite} />
      <CampaignDraftEditor campaign={campaign} onSaved={saved => { if (mounted.current) { setCampaign(saved); setReadinessGeneration(value => value + 1); } }} />
      <CampaignReadinessPanel key={readinessGeneration} campaign={campaign} />
      {['AUDIENCE_VALIDATED','MESSAGE_REVIEW_PENDING','MESSAGE_APPROVED','COMMERCIAL_APPROVED','FINAL_APPROVAL_PENDING','SCHEDULED','DISPATCHING','PAUSED','COMPLETED','COMPLETED_WITH_EXCEPTIONS'].includes(campaign.status)
        ? <MessageComposer campaign={campaign} onChanged={load} />
        : null}

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
        <form className="card form-stack" onSubmit={sendControlledTest}>
          <h2>Controlled test</h2>
          <p className="muted">Choose only from server-filtered approved recipients and READY sessions on this campaign’s exact sender/gateway/engine route. The server revalidates everything again when the test is scheduled and executed.</p>
          <label>Approved test recipient
            <select aria-label="Approved test recipient" name="testRecipientId" required disabled={!testOptions?.supported || testOptions.recipients.length === 0 || Boolean(busy)} defaultValue="">
              <option value="" disabled>Select an approved masked recipient</option>
              {(testOptions?.recipients ?? []).map((item) => <option key={item.id} value={item.id}>{item.label} · {item.maskedMsisdn}</option>)}
            </select>
          </label>
          <label>Compatible READY sender session
            <select aria-label="Compatible READY sender session" name="senderSessionId" required disabled={!testOptions?.supported || testOptions.sessions.length === 0 || Boolean(busy)} defaultValue="">
              <option value="" disabled>Select a READY session on the exact route</option>
              {(testOptions?.sessions ?? []).map((item) => <option key={item.id} value={item.id}>{item.profileDisplayName || 'Sender'} · {item.maskedMsisdn}</option>)}
            </select>
          </label>
          <label>Reason<textarea name="reason" minLength={8} rows={3} required /></label>
          <button className="primary" type="submit" disabled={!canWrite || !campaign.messageVersionId || !testOptions?.supported || testOptions.recipients.length === 0 || testOptions.sessions.length === 0 || Boolean(busy)}>Send controlled test</button>
          {!campaign.messageVersionId ? <small>An approved campaign message version is required first.</small> : null}
          {testOptions && !testOptions.supported ? <small>{testOptions.reason || 'Controlled testing is unavailable for this campaign route.'}</small> : null}
          {testOptions?.supported && testOptions.recipients.length === 0 ? <small>No ACTIVE approved test recipient is currently available. Govern one in the sender console before testing.</small> : null}
          {testOptions?.supported && testOptions.sessions.length === 0 ? <small>No READY sender session currently matches this campaign’s sender pool, gateway pool and engine.</small> : null}
          {testOptions?.supported && (!testOptions.recipientInventoryComplete || !testOptions.sessionInventoryComplete) ? <small>The selector inventory reached its bounded scan limit. Only the returned governed options are selectable; ask a technical administrator if an expected option is absent.</small> : null}
        </form>

        <article className="card form-stack">
          <h2>Day‑1 capacity hold</h2>
          <p className="muted">Capacity values are derived and revalidated by the server from the accepted exact-route test, current measured health, pacing policy, saved campaign window and existing reservations. Operators cannot enter or enlarge capacity values.</p>
          {heldReservation || activeReservation ? (
            <div className="alert alert-information">
              <strong>{activeReservation ? 'ACTIVE capacity reservation' : 'HELD capacity reservation'}</strong>
              <p>The authoritative reservation already exists for this campaign. Its evidence is listed below.</p>
            </div>
          ) : !acceptedExactTest ? (
            <div className="empty-state">
              <strong>Accepted exact-route test required</strong>
              <p>Complete a controlled test on the current message and frozen route before capacity can be reserved.</p>
            </div>
          ) : !capacityProposal ? (
            <div className="empty-state">
              <strong>Server-derived hold is not currently available</strong>
              <p>Reload readiness after current route, pacing and capacity evidence is available.</p>
            </div>
          ) : (
            <>
              <dl className="definition-list">
                <div><dt>Exact route</dt><dd>{capacityProposal.provider} / {capacityProposal.engine}<small>{capacityProposal.senderPoolId} · {capacityProposal.gatewayPoolId}</small></dd></div>
                <div><dt>Reservation window</dt><dd>{new Date(capacityProposal.reservationStart).toLocaleString()} → {new Date(capacityProposal.reservationEnd).toLocaleString()}</dd></div>
                <div><dt>Audience basis</dt><dd>{capacityProposal.basisRecipientCount.toLocaleString()} recipients</dd></div>
                <div><dt>Measured at</dt><dd>{new Date(capacityProposal.measurementAsOf).toLocaleString()}</dd></div>
                <div><dt>Healthy authority</dt><dd>{capacityProposal.healthySessions} sessions · {capacityProposal.healthyNodes} nodes (minimum {capacityProposal.minimumHealthyNodes})</dd></div>
                <div><dt>Available capacity</dt><dd>{capacityProposal.availableMessagesPerMinute.toLocaleString()} MPM · {capacityProposal.availableHourlyUnits.toLocaleString()} hourly · {capacityProposal.availableDailyUnits.toLocaleString()} daily</dd></div>
                <div><dt>Required rate</dt><dd>{capacityProposal.requiredMessagesPerMinute.toFixed(2)} MPM · {capacityProposal.safetyMarginPercent}% safety margin</dd></div>
                <div><dt>Derived hold</dt><dd><strong>{capacityProposal.reservedMessagesPerMinute.toLocaleString()} MPM · {capacityProposal.reservedHourlyUnits.toLocaleString()} hourly · {capacityProposal.reservedDailyUnits.toLocaleString()} daily</strong></dd></div>
                <div><dt>Pacing policy</dt><dd>{capacityProposal.pacingPolicyId} / v{capacityProposal.pacingPolicyVersion}<small>{capacityProposal.pacingMessagesPerMinute} MPM · {capacityProposal.pacingHourlyAllowance.toLocaleString()} hourly · {capacityProposal.pacingDailyAllowance.toLocaleString()} daily</small></dd></div>
                <div><dt>Evidence versions</dt><dd>{capacityProposal.routingPolicyVersion}<small>{capacityProposal.capacityEvidenceVersion}</small></dd></div>
              </dl>
              <form className="form-stack" onSubmit={confirmCapacityHold}>
                <label>Capacity approval reason<textarea name="reason" minLength={8} maxLength={1000} rows={3} required placeholder="Explain why this measured capacity hold is approved." /></label>
                <button className="primary" type="submit" disabled={!canApprove || Boolean(busy)}>Confirm governed hold</button>
                <small>The server re-derives current measured and pacing evidence immediately before the hold, then atomically rechecks overlapping reservations and pool limits while persisting the HELD reservation.</small>
              </form>
            </>
          )}
        </article>
      </section>

      <CommercialGovernance
        campaign={campaign}
        pilotEvidenceReady={Boolean(acceptedExactTest && (heldReservation || activeReservation))}
        onChanged={load}
      />

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
