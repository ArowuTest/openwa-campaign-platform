'use client';

import Link from 'next/link';
import { FormEvent, useEffect, useRef, useState } from 'react';

import { StatusBadge } from '../../components/status-badge';
import { apiRequest } from '../../lib/api';
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

const capabilities = [
  'SEND_TEXT',
  'SEND_IMAGE',
  'SEND_VIDEO',
  'SEND_DOCUMENT',
  'DELIVERY_EVENTS',
  'READ_EVENTS',
  'INBOUND_MESSAGES'
] as const;

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
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);

  async function load() {
    const payload = await apiRequest<{ items: Campaign[] }>('/v1/campaigns');
    if (mounted.current) setItems(payload.items ?? []);
  }

  useEffect(() => {
    mounted.current = true;
    let cancelled = false;
    void apiRequest<{ items: Campaign[] }>('/v1/campaigns')
      .then((payload) => {
        if (!cancelled) setItems(payload.items ?? []);
      })
      .catch(() => {
        if (!cancelled) setItems([]);
      });
    return () => { cancelled = true; mounted.current = false; };
  }, []);

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
      const created = await apiRequest<Campaign>('/v1/campaigns', {
        method: 'POST',
        body: JSON.stringify(payload)
      });
      if (!mounted.current) return;
      setCreatedId(created.id);
      setMessage('Campaign created in DRAFT with one governed OpenWA sender pool. It cannot dispatch until every server-side approval and pilot gate is complete.');
      form.reset();
      await load().catch(() => { if (mounted.current) setMessage('Campaign created in DRAFT. The campaign register could not be refreshed; open the created draft directly.'); });
    } catch (cause) {
      if (!mounted.current) return;
      setMessage(cause instanceof Error ? cause.message : 'Campaign could not be created');
    } finally {
      creating.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  return (
    <div className="split-layout">
      <section className="card">
        <div className="section-heading">
          <div>
            <h2>Campaign register</h2>
            <p className="muted">Open a campaign to manage evidence, controlled testing, capacity holds and execution.</p>
          </div>
          <span className="pill">{items.length} campaigns</span>
        </div>
        {items.length === 0 ? <div className="empty-state"><strong>No campaigns</strong><p>Create the first campaign using the governed Day‑1 form.</p></div> : (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Campaign</th><th>Status</th><th>Route</th><th>Audience</th><th>Window</th></tr></thead>
              <tbody>{items.map((item) => (
                <tr key={item.id}>
                  <td><Link className="table-link" href={'/campaigns/' + item.id}><strong>{item.name}</strong></Link><small>{item.id}</small></td>
                  <td><StatusBadge status={item.status} /></td>
                  <td>{item.transport?.engine || '—'}<small>{item.transport?.senderPoolId || 'No pool bound'}</small></td>
                  <td>{item.eligibleAudienceCount.toLocaleString()} / {item.maximumUniqueRecipients.toLocaleString()}</td>
                  <td>{item.requestedStartAt ? new Date(item.requestedStartAt).toLocaleString() : 'Not set'}<small>{item.completionDeadlineAt ? 'Deadline: ' + new Date(item.completionDeadlineAt).toLocaleString() : ''}</small></td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}
      </section>

      <form className="card form-stack" onSubmit={create}>
        <div>
          <h2>Create draft campaign</h2>
          <p className="muted">Day 1 uses exactly one engine-specific OpenWA sender pool. Several READY sessions may serve that pool; mixed engines and automatic fallback are not enabled.</p>
        </div>
        <div className="alert alert-information">Operator identity is taken from your authenticated session. Client organisations never create or launch campaigns directly.</div>

        <label>Campaign name<input name="name" maxLength={250} required /></label>
        <label>Organisation ID<input name="organisationId" required /></label>
        <label>Consent purpose ID<input name="purposeId" required /></label>
        <label>Approved consent-review ID<input name="consentReviewId" required /></label>
        <label>Maximum unique recipients<input name="maximumUniqueRecipients" type="number" min="1" required /></label>

        <fieldset>
          <legend>Exact OpenWA transport</legend>
          <label>Engine
            <select name="engine" defaultValue="WHATSAPP_WEB_JS" required>
              <option value="WHATSAPP_WEB_JS">WhatsApp Web JS / Chromium</option>
              <option value="BAILEYS">Baileys</option>
            </select>
          </label>
          <label>Sender pool ID<input name="senderPoolId" required /></label>
          <label>Gateway pool ID<input name="gatewayPoolId" required /></label>
          <label>Adapter version<input name="adapterVersion" placeholder="0.13.0+platform.1" required /></label>
          <label>Routing policy version<input name="routingPolicyVersion" required /></label>
          <label>Capacity evidence version<input name="capacityEvidenceVersion" required /></label>
          <small>Routing mode is fixed to SENDER_POOL and fallback is fixed to NONE for Day 1.</small>
        </fieldset>

        <fieldset>
          <legend>Required capabilities</legend>
          {capabilities.map((capability) => (
            <label className="check" key={capability}>
              <input
                type="checkbox"
                name={'capability:' + capability}
                defaultChecked={capability === 'SEND_TEXT' || capability === 'DELIVERY_EVENTS'}
              />
              <span>{capability.replaceAll('_', ' ')}</span>
            </label>
          ))}
        </fieldset>

        <div className="grid grid-2">
          <label>Requested start (UTC)<input name="requestedStartAt" type="datetime-local" /></label>
          <label>Completion deadline (UTC)<input name="completionDeadlineAt" type="datetime-local" /></label>
        </div>
        <label>Campaign timezone<input name="timezone" defaultValue="UTC" required /></label>
        <div className="grid grid-2">
          <label>Quiet hours start<input name="quietHoursStart" type="time" /></label>
          <label>Quiet hours end<input name="quietHoursEnd" type="time" /></label>
        </div>

        <button className="primary" disabled={busy || !canWrite}>{busy ? 'Creating…' : 'Create governed draft'}</button>
        {message ? <div className="alert" role="status">{message}</div> : null}
        {createdId && <Link className="text-link" href={'/campaigns/' + encodeURIComponent(createdId)}>Open created draft</Link>}
      </form>
    </div>
  );
}
