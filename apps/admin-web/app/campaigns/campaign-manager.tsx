'use client';

import Link from 'next/link';
import { FormEvent, useEffect, useRef, useState } from 'react';

import { StatusBadge } from '../../components/status-badge';
import { apiRequest, collectBoundedPages, type ListEnvelope } from '../../lib/api';
import { useAuth } from '../../components/auth-provider';
import { hasPermission } from '../../lib/session';

type Campaign = {
  id: string;
  name: string;
  organisationId: string;
  status: string;
  maximumUniqueRecipients: number;
  eligibleAudienceCount: number;
  requestedStartAt?: string;
  completionDeadlineAt?: string;
  createdAt: string;
  transport?: { provider?: string; engine?: string; senderPoolId?: string };
};

type Organisation = { id: string; legalName: string; tradingName?: string; status: string };
type ConsentReview = { id: string; organisationId: string; name: string; status: string; outcome?: string; wordingVersion?: string; expiresAt?: string };
type ConsentPurpose = { id: string; organisationId?: string; consentReviewId?: string; code: string; name: string; active: boolean; channel: string; wordingVersion?: string };
type SenderPool = { id: string; name: string; organisationId?: string; status: string; version?: number };
type GatewayPool = { id: string; name: string; provider?: string; engine: string; adapterVersion: string; status: string; capabilities?: string[]; version?: number };

const capabilities = [
  'SEND_TEXT',
  'SEND_IMAGE',
  'SEND_VIDEO',
  'SEND_DOCUMENT',
  'DELIVERY_EVENTS',
  'READ_EVENTS',
  'INBOUND_MESSAGES'
] as const;

const approvalStatuses = new Set(['APPROVED', 'ACTIVE']);

async function collectCampaigns(maxPages = 20): Promise<Campaign[]> {
  const values: Campaign[] = [];
  const seen = new Set<string>();
  let cursor = '';
  for (let pageNumber = 0; pageNumber < maxPages; pageNumber += 1) {
    const page = await apiRequest<{ items: Campaign[]; nextCursor?: string }>(
      '/v1/campaigns?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
    );
    if (!page || !Array.isArray(page.items)) throw new Error('Malformed campaign inventory response');
    values.push(...page.items);
    const next = typeof page.nextCursor === 'string' ? page.nextCursor.trim() : '';
    if (!next) return values;
    if (seen.has(next) || next === cursor) throw new Error('Repeated campaign inventory cursor');
    seen.add(next);
    cursor = next;
  }
  throw new Error('Campaign inventory page limit exceeded');
}

function approvedReview(review: ConsentReview): boolean {
  if (review.status !== 'APPROVED' && review.outcome !== 'APPROVED') return false;
  return !review.expiresAt || new Date(review.expiresAt).getTime() > Date.now();
}

export function CampaignManager() {
  const { session } = useAuth();
  const generation = JSON.stringify([session?.id, session?.sessionId, [...(session?.permissions ?? [])].sort()]);
  return <CampaignManagerGeneration key={generation} />;
}

function CampaignManagerGeneration() {
  const { session } = useAuth();
  const canWrite = hasPermission(session?.permissions, 'campaign.write');
  const mounted = useRef(false);
  const creating = useRef(false);
  const [createdId, setCreatedId] = useState('');
  const [items, setItems] = useState<Campaign[]>([]);
  const [organisations, setOrganisations] = useState<Organisation[]>([]);
  const [reviews, setReviews] = useState<ConsentReview[]>([]);
  const [purposes, setPurposes] = useState<ConsentPurpose[]>([]);
  const [senderPools, setSenderPools] = useState<SenderPool[]>([]);
  const [gatewayPools, setGatewayPools] = useState<GatewayPool[]>([]);
  const [organisationId, setOrganisationId] = useState('');
  const [reviewId, setReviewId] = useState('');
  const [purposeId, setPurposeId] = useState('');
  const [engine, setEngine] = useState('WHATSAPP_WEB_JS');
  const [senderPoolId, setSenderPoolId] = useState('');
  const [gatewayPoolId, setGatewayPoolId] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const [inventoryLoading, setInventoryLoading] = useState(true);

  async function loadCampaigns() {
    const values = await collectCampaigns();
    if (mounted.current) setItems(values);
  }

  useEffect(() => {
    mounted.current = true;
    let cancelled = false;
    void Promise.all([
      collectBoundedPages<Organisation>((cursor) => apiRequest<ListEnvelope<Organisation>>('/v1/organisations?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20),
      collectBoundedPages<SenderPool>((cursor) => apiRequest<ListEnvelope<SenderPool>>('/v1/sender-pools?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20),
      collectBoundedPages<GatewayPool>((cursor) => apiRequest<ListEnvelope<GatewayPool>>('/v1/admin/gateway-pools?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20),
      collectCampaigns()
    ]).then(([orgs, pools, gateways, campaigns]) => {
      if (cancelled) return;
      setOrganisations(orgs.filter((item) => item.status === 'ACTIVE'));
      setSenderPools(pools);
      setGatewayPools(gateways.filter((item) => approvalStatuses.has(item.status)));
      setItems(campaigns);
      setSenderPoolId('');
      setGatewayPoolId('');
    }).catch((error) => {
      if (!cancelled) setMessage(error instanceof Error ? error.message : 'Campaign inventory could not be loaded.');
    }).finally(() => {
      if (!cancelled) setInventoryLoading(false);
    });
    return () => { cancelled = true; mounted.current = false; };
  }, []);

  useEffect(() => {
    if (!organisationId) return;
    let cancelled = false;
    void collectBoundedPages<ConsentReview>(
      (cursor) => apiRequest<ListEnvelope<ConsentReview>>('/v1/consent-reviews?organisationId=' + encodeURIComponent(organisationId) + '&limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')),
      20
    ).then((values) => {
      if (!cancelled) setReviews(values.filter(approvedReview));
    }).catch((error) => {
      if (!cancelled) setMessage(error instanceof Error ? error.message : 'Consent reviews could not be loaded.');
    });
    return () => { cancelled = true; };
  }, [organisationId]);

  useEffect(() => {
    if (!organisationId || !reviewId) return;
    let cancelled = false;
    void collectBoundedPages<ConsentPurpose>(
      (cursor) => apiRequest<ListEnvelope<ConsentPurpose>>('/v1/consent-purposes?organisationId=' + encodeURIComponent(organisationId) + '&consentReviewId=' + encodeURIComponent(reviewId) + '&limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')),
      20
    ).then((values) => {
      if (!cancelled) setPurposes(values.filter((item) => item.active && item.channel === 'WHATSAPP'));
    }).catch((error) => {
      if (!cancelled) setMessage(error instanceof Error ? error.message : 'Consent purposes could not be loaded.');
    });
    return () => { cancelled = true; };
  }, [organisationId, reviewId]);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canWrite || creating.current) return;
    creating.current = true;
    setBusy(true);
    setCreatedId('');
    setMessage('');
    const form = event.currentTarget;
    const data = new FormData(form);
    const start = String(data.get('requestedStartAt') ?? '');
    const deadline = String(data.get('completionDeadlineAt') ?? '');
    const requiredCapabilities = capabilities.filter((capability) => data.get('capability:' + capability) === 'on');
    const payload = {
      organisationId: String(data.get('organisationId') ?? '').trim(),
      name: String(data.get('name') ?? '').trim(),
      purposeId: String(data.get('purposeId') ?? '').trim(),
      consentReviewId: String(data.get('consentReviewId') ?? '').trim(),
      requestedStartAt: start ? new Date(start + 'Z').toISOString() : undefined,
      completionDeadlineAt: deadline ? new Date(deadline + 'Z').toISOString() : undefined,
      timezone: String(data.get('timezone') ?? 'UTC').trim() || 'UTC',
      quietHoursStart: String(data.get('quietHoursStart') ?? '').trim() || undefined,
      quietHoursEnd: String(data.get('quietHoursEnd') ?? '').trim() || undefined,
      maximumUniqueRecipients: Number(data.get('maximumUniqueRecipients')),
      maximumMessagesPerRecipient: 1,
      transport: {
        channel: 'WHATSAPP',
        provider: 'OPENWA',
        engine: String(data.get('engine') ?? ''),
        routingMode: 'SENDER_POOL',
        gatewayPoolId: String(data.get('gatewayPoolId') ?? '').trim(),
        senderPoolId: String(data.get('senderPoolId') ?? '').trim(),
        adapterVersion: String(data.get('adapterVersion') ?? '').trim(),
        requiredCapabilities,
        fallbackMode: 'NONE',
        routingPolicyVersion: String(data.get('routingPolicyVersion') ?? '').trim(),
        capacityEvidenceVersion: String(data.get('capacityEvidenceVersion') ?? '').trim()
      }
    };
    try {
      const created = await apiRequest<Campaign>('/v1/campaigns', { method: 'POST', body: JSON.stringify(payload) });
      if (!mounted.current) return;
      setCreatedId(created.id);
      setMessage('Campaign created in DRAFT with one governed OpenWA sender pool. It cannot dispatch until every server-side approval and pilot gate is complete.');
      form.reset();
      setOrganisationId('');
      setSenderPoolId('');
      setGatewayPoolId('');
      await loadCampaigns().catch(() => { if (mounted.current) setMessage('Campaign created in DRAFT. The campaign register could not be refreshed; open the created draft directly.'); });
    } catch (cause) {
      if (mounted.current) setMessage(cause instanceof Error ? cause.message : 'Campaign could not be created');
    } finally {
      creating.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  const selectedGateway = gatewayPools.find((item) => item.id === gatewayPoolId);
  const engineGateways = gatewayPools.filter((item) => item.engine === engine);
  const organisationPools = senderPools.filter((item) => !item.organisationId || item.organisationId === organisationId);

  return (
    <div className="workspace-stack">
      <section className="card">
        <div className="section-heading"><div><h2>Campaign register</h2><p className="muted">Open a campaign to manage evidence, controlled testing, capacity holds and execution.</p></div><span className="pill">{items.length} campaigns</span></div>
        {inventoryLoading ? <p className="muted">Loading governed campaign inventory…</p> : null}
        {!inventoryLoading && items.length === 0 ? <div className="empty-state"><strong>No campaigns</strong><p>Create the first campaign using the governed Day‑1 form.</p></div> : null}
        {items.length > 0 ? (
          <div className="table-wrap"><table><thead><tr><th>Campaign</th><th>Status</th><th>Route</th><th>Audience</th><th>Window</th></tr></thead><tbody>{items.map((item) => (
            <tr key={item.id}><td><Link className="table-link" href={'/campaigns/' + item.id}><strong>{item.name}</strong></Link><small>{item.id}</small></td><td><StatusBadge status={item.status} /></td><td>{item.transport?.engine || '—'}<small>{item.transport?.senderPoolId || 'No pool bound'}</small></td><td>{item.eligibleAudienceCount.toLocaleString()} / {item.maximumUniqueRecipients.toLocaleString()}</td><td>{item.requestedStartAt ? new Date(item.requestedStartAt).toLocaleString() : 'Not set'}<small>{item.completionDeadlineAt ? 'Deadline: ' + new Date(item.completionDeadlineAt).toLocaleString() : ''}</small></td></tr>
          ))}</tbody></table></div>
        ) : null}
      </section>

      <form className="card form-stack" onSubmit={create}>
        <div><h2>Create draft campaign</h2><p className="muted">Day 1 uses exactly one engine-specific OpenWA sender pool. Several READY sessions may serve that pool; mixed engines and automatic fallback are not enabled.</p></div>
        <div className="alert alert-information">Operator identity is taken from your authenticated session. Client organisations never create or launch campaigns directly.</div>

        <label>Campaign name<input name="name" maxLength={250} required /></label>
        <label>Organisation<select aria-label="Organisation ID" name="organisationId" required value={organisationId} onChange={(event) => { setOrganisationId(event.target.value); setReviews([]); setPurposes([]); setReviewId(''); setPurposeId(''); }}><option value="" disabled>Select an active organisation</option>{organisations.map((item) => <option key={item.id} value={item.id}>{item.tradingName || item.legalName} · {item.id}</option>)}</select></label>
        <label>Consent review<select aria-label="Approved consent-review ID" name="consentReviewId" required value={reviewId} disabled={!organisationId || reviews.length === 0} onChange={(event) => { setReviewId(event.target.value); setPurposes([]); setPurposeId(''); }}><option value="" disabled>Select an approved review</option>{reviews.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.id}</option>)}</select></label>
        <label>Consent purpose<select aria-label="Consent purpose ID" name="purposeId" required value={purposeId} disabled={!reviewId || purposes.length === 0} onChange={(event) => setPurposeId(event.target.value)}><option value="" disabled>Select an active WhatsApp purpose</option>{purposes.map((item) => <option key={item.id} value={item.id}>{item.name} ({item.code}) · {item.id}</option>)}</select></label>
        <label>Maximum unique recipients<input name="maximumUniqueRecipients" type="number" min="1" required /></label>

        <fieldset><legend>Exact OpenWA transport</legend>
          <label>Engine<select name="engine" value={engine} onChange={(event) => { setEngine(event.target.value); setGatewayPoolId(''); }} required><option value="WHATSAPP_WEB_JS">WhatsApp Web JS / Chromium</option><option value="BAILEYS">Baileys</option></select></label>
          <label>Sender pool<select aria-label="Sender pool ID" name="senderPoolId" required disabled={!organisationId || organisationPools.length === 0} value={senderPoolId} onChange={(event) => setSenderPoolId(event.target.value)}><option value="" disabled>Select a sender pool</option>{organisationPools.filter((item) => approvalStatuses.has(item.status)).map((item) => <option key={item.id} value={item.id}>{item.name} · {item.id}</option>)}</select></label>
          <label>Gateway pool<select aria-label="Gateway pool ID" name="gatewayPoolId" required disabled={engineGateways.length === 0} value={gatewayPoolId} onChange={(event) => setGatewayPoolId(event.target.value)}><option value="" disabled>Select an active gateway pool</option>{engineGateways.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.engine} · {item.id}</option>)}</select></label>
          <label>Adapter version<input name="adapterVersion" value={selectedGateway?.adapterVersion ?? ''} readOnly placeholder="Select a gateway pool" required /></label>
          <label>Routing policy version<input name="routingPolicyVersion" required /></label>
          <label>Capacity evidence version<input name="capacityEvidenceVersion" required /></label>
          <small>Routing mode is fixed to SENDER_POOL and fallback is fixed to NONE for Day 1. Gateway and sender IDs are selected from current governed inventory.</small>
        </fieldset>

        <fieldset><legend>Required capabilities</legend>{capabilities.map((capability) => <label className="check" key={capability}><input type="checkbox" name={'capability:' + capability} defaultChecked={capability === 'SEND_TEXT' || capability === 'DELIVERY_EVENTS'} /><span>{capability.replaceAll('_', ' ')}</span></label>)}</fieldset>
        <div className="grid grid-2"><label>Requested start (UTC)<input name="requestedStartAt" type="datetime-local" /></label><label>Completion deadline (UTC)<input name="completionDeadlineAt" type="datetime-local" /></label></div>
        <label>Campaign timezone<input name="timezone" defaultValue="UTC" required /></label>
        <div className="grid grid-2"><label>Quiet hours start<input name="quietHoursStart" type="time" /></label><label>Quiet hours end<input name="quietHoursEnd" type="time" /></label></div>

        <button className="primary" disabled={busy || !canWrite || inventoryLoading}>{busy ? 'Creating…' : 'Create governed draft'}</button>
        {message ? <div className="alert" role="status">{message}</div> : null}
        {createdId && <Link className="text-link" href={'/campaigns/' + encodeURIComponent(createdId)}>Open created draft</Link>}
      </form>
    </div>
  );
}
