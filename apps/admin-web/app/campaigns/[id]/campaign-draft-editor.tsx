'use client';

import { FormEvent, useEffect, useRef, useState } from 'react';
import { useAuth } from '../../../components/auth-provider';
import { APIError } from '../../../lib/api';
import { getCampaignDetail, saveCampaignDraft, type CampaignDetail, type DraftSaveRequest, type DraftTransportRequest } from '../../../lib/campaign-preparation-model';
import { hasPermission } from '../../../lib/session';

const capabilities = ['SEND_TEXT', 'SEND_TEMPLATE', 'SEND_IMAGE', 'SEND_VIDEO', 'SEND_DOCUMENT', 'DELIVERY_EVENTS', 'READ_EVENTS', 'INBOUND_MESSAGES', 'PAIRING_QR', 'PAIRING_CODE'];
type DraftForm = {
  name: string; reason: string; maximum: string; start: string; deadline: string; timezone: string; quietStart: string; quietEnd: string;
  engine: DraftTransportRequest['engine']; gateway: string; pool: string; adapter: string; routing: string; capacity: string; capabilities: string[];
};
function formFrom(c: CampaignDetail): DraftForm {
  return { name: c.name, reason: '', maximum: String(c.maximumUniqueRecipients), start: c.requestedStartAt ?? '', deadline: c.completionDeadlineAt ?? '',
    timezone: c.timezone || 'UTC', quietStart: c.quietHoursStart ?? '', quietEnd: c.quietHoursEnd ?? '', engine: c.transport.engine as DraftTransportRequest['engine'],
    gateway: c.transport.gatewayPoolId ?? '', pool: c.transport.senderPoolId ?? '', adapter: c.transport.adapterVersion,
    routing: c.transport.routingPolicyVersion, capacity: c.transport.capacityEvidenceVersion, capabilities: [...(c.transport.requiredCapabilities ?? [])] };
}
function requestFrom(c: CampaignDetail, f: DraftForm): DraftSaveRequest {
  return { expectedVersion: c.version, reason: f.reason.trim(), name: f.name.trim(), organisationId: c.organisationId, purposeId: c.purposeId, consentReviewId: c.consentReviewId,
    maximumUniqueRecipients: Number(f.maximum), maximumMessagesPerRecipient: 1, requestedStartAt: f.start.trim() || null, completionDeadlineAt: f.deadline.trim() || null,
    timezone: f.timezone.trim(), quietHoursStart: f.quietStart.trim(), quietHoursEnd: f.quietEnd.trim(),
    transport: { channel: 'WHATSAPP', provider: 'OPENWA', engine: f.engine, routingMode: 'SENDER_POOL', gatewayPoolId: f.gateway.trim(), senderPoolId: f.pool.trim(),
      adapterVersion: f.adapter.trim(), routingPolicyVersion: f.routing.trim(), capacityEvidenceVersion: f.capacity.trim(), fallbackMode: 'NONE', requiredCapabilities: [...f.capabilities] } };
}
function submittedFieldsMatch(saved: CampaignDetail, input: DraftSaveRequest): boolean {
  const actual = requestFrom(saved, { ...formFrom(saved), reason: input.reason });
  // Parse only whole seconds with Date; fraction digits retain their full precision.
  const instant = (value: string): string | undefined => {
    const parts = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/.exec(value);
    if (!parts) return undefined;
    const local = Date.parse(parts[1] + 'Z');
    if (!Number.isFinite(local) || new Date(local).toISOString().slice(0, 19) !== parts[1]) return undefined;
    const seconds = Date.parse(parts[1] + parts[3]);
    return Number.isFinite(seconds) ? seconds + ':' + (parts[2] ?? '').replace(/0+$/, '') : undefined;
  };
  const sameInstant = (a: string | null, b: string | null) => {
    if (a === null || b === null) return a === b;
    const exact = instant(a);
    return exact !== undefined && exact === instant(b);
  };
  if (!sameInstant(actual.requestedStartAt, input.requestedStartAt) || !sameInstant(actual.completionDeadlineAt, input.completionDeadlineAt)) return false;
  actual.requestedStartAt = input.requestedStartAt; actual.completionDeadlineAt = input.completionDeadlineAt;
  actual.expectedVersion = input.expectedVersion;
  actual.transport.requiredCapabilities.sort();
  return JSON.stringify(actual) === JSON.stringify({ ...input, transport: { ...input.transport, requiredCapabilities: [...input.transport.requiredCapabilities].sort() } });
}
export function CampaignDraftEditor({ campaign, onSaved }: { campaign: CampaignDetail; onSaved: (saved: CampaignDetail) => void }) {
  const { session } = useAuth();
  const generation = JSON.stringify([campaign.id, session?.id, session?.sessionId, [...(session?.permissions ?? [])].sort()]);
  return <DraftEditorGeneration key={generation} campaign={campaign} onSaved={onSaved} />;
}
function DraftEditorGeneration({ campaign, onSaved }: { campaign: CampaignDetail; onSaved: (saved: CampaignDetail) => void }) {
  const { session } = useAuth();
  const [baseline, setBaseline] = useState(campaign);
  const [form, setForm] = useState(() => formFrom(campaign));
  const [busy, setBusy] = useState(false);
  const [blocked, setBlocked] = useState(false);
  const [denied, setDenied] = useState(false);
  const [server, setServer] = useState<CampaignDetail>();
  const [message, setMessage] = useState('');
  const mounted = useRef(false);
  const mutation = useRef<symbol | undefined>(undefined);
  const read = useRef<AbortController | undefined>(undefined);
  const canWrite = hasPermission(session?.permissions, 'campaign.write') && !denied;
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; mutation.current = undefined; read.current?.abort(); };
  }, []);
  function change<K extends keyof DraftForm>(key: K, value: DraftForm[K]) { setForm(previous => ({ ...previous, [key]: value })); }
  function reset(c = baseline) { setBaseline(c); setForm(formFrom(c)); setServer(undefined); setBlocked(false); setMessage(''); }
  async function reconcile(token: symbol, input: DraftSaveRequest, uncertain: boolean) {
    const controller = new AbortController(); read.current = controller;
    try {
      const current = await getCampaignDetail(baseline.id, controller.signal);
      if (!mounted.current || mutation.current !== token || controller.signal.aborted) return;
      setServer(current);
      setMessage(uncertain
        ? (submittedFieldsMatch(current, input) ? 'Save response was uncertain. Submitted fields are saved on the server.' : 'Save response was uncertain. Submitted fields differ from the server. Reconcile explicitly before another save.')
        : 'The campaign changed. Reconcile explicitly before another save.');
    } catch {
      if (!mounted.current || mutation.current !== token || controller.signal.aborted) return;
      setMessage('Current server state could not be read. Reload explicitly before another save.');
    }
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canWrite || busy || blocked || mutation.current) return;
    const input = requestFrom(baseline, form);
    if (!Number.isSafeInteger(input.maximumUniqueRecipients) || input.maximumUniqueRecipients < 1) { setMessage('Maximum unique recipients must be a safe positive integer.'); return; }
    const token = Symbol('draft save'); mutation.current = token; setBusy(true); setMessage('');
    try {
      const saved = await saveCampaignDraft(baseline.id, input);
      if (!mounted.current || mutation.current !== token) return;
      setBaseline(saved); setForm(formFrom(saved)); setServer(undefined); setBlocked(false); setMessage('Draft saved.'); onSaved(saved);
    } catch (cause) {
      if (!mounted.current || mutation.current !== token) return;
      const uncertain = !(cause instanceof APIError) || (cause.status >= 500 && cause.code !== 'CAMPAIGN_DRAFT_GOVERNANCE_UNAVAILABLE');
      if (uncertain || (cause instanceof APIError && cause.status === 409)) {
        setBlocked(true);
        await reconcile(token, input, uncertain);
      } else {
        if (cause.status === 401 || cause.status === 403) { setDenied(true); setForm(formFrom(baseline)); }
        setMessage(cause.message || 'Draft could not be saved.');
      }
    } finally {
      if (mounted.current && mutation.current === token) { mutation.current = undefined; setBusy(false); }
    }
  }
  async function reload() {
    if (!mounted.current || busy) return;
    if (server) { reset(server); onSaved(server); return; }
    const controller = new AbortController(); read.current = controller; setBusy(true);
    try {
      const current = await getCampaignDetail(baseline.id, controller.signal);
      if (!mounted.current || read.current !== controller || controller.signal.aborted) return;
      reset(current); onSaved(current);
    } catch {
      if (mounted.current && read.current === controller && !controller.signal.aborted) setMessage('Current server state could not be read. Reload explicitly before another save.');
    } finally {
      if (mounted.current && read.current === controller && !controller.signal.aborted) setBusy(false);
    }
  }
  const frozen = baseline.audienceSnapshotId || baseline.audienceSnapshotHash || baseline.eligibleAudienceCount > 0 || baseline.messageVersionId || baseline.messageContentHash || baseline.finalApprovedBy || baseline.commercialApprovalId;
  if (baseline.status !== 'DRAFT' || frozen || baseline.transport.provider !== 'OPENWA') {
    return <section className="card"><h2>Draft preparation</h2><p>Draft editing is locked by the campaign state, frozen evidence or provider workflow.</p></section>;
  }
  if (!canWrite) return <section className="card"><h2>Draft preparation</h2><p>campaign.write permission is required to edit this draft.</p>{message && <p role="alert">{message}</p>}</section>;
  return <form className="card form-stack" onSubmit={save}>
    <h2>Edit campaign draft</h2>
    <p>Campaign {baseline.id} · DRAFT · version {baseline.version} · creator {baseline.createdBy}</p>
    <dl className="definition-list"><div><dt>Organisation (locked)</dt><dd>{baseline.organisationId}</dd></div><div><dt>Purpose (locked)</dt><dd>{baseline.purposeId}</dd></div><div><dt>Consent review (locked)</dt><dd>{baseline.consentReviewId}</dd></div></dl>
    <p className="muted">Saving checks current governance. Approval, readiness and release remain separate steps. UTC instants stay unchanged when the timezone is changed.</p>
    <fieldset disabled={busy}>
      <label>Draft name<input value={form.name} onChange={e => change('name', e.target.value)} maxLength={250} required /></label>
      <label>Maximum unique recipients<input type="number" min={1} max={Number.MAX_SAFE_INTEGER} step={1} value={form.maximum} onChange={e => change('maximum', e.target.value)} required /></label>
      <p>One message per recipient. A draft maximum does not create an entitlement.</p>
      <label>Requested start (UTC)<input value={form.start} onChange={e => change('start', e.target.value)} placeholder="2026-10-09T12:00:00Z" /></label>
      <label>Completion deadline (UTC)<input value={form.deadline} onChange={e => change('deadline', e.target.value)} placeholder="2026-10-09T13:00:00Z" /></label>
      <p className="muted">Either date may be left blank during preparation. Use ISO 8601 date-time values; blank clears the saved value.</p>
      <label>Timezone<input value={form.timezone} onChange={e => change('timezone', e.target.value)} required /></label>
      <label>Quiet-hours start<input type="time" value={form.quietStart} onChange={e => change('quietStart', e.target.value)} /></label>
      <label>Quiet-hours end<input type="time" value={form.quietEnd} onChange={e => change('quietEnd', e.target.value)} /></label>
      <label>Engine<select value={form.engine} onChange={e => change('engine', e.target.value as DraftTransportRequest['engine'])}><option value="WHATSAPP_WEB_JS">WHATSAPP_WEB_JS</option><option value="BAILEYS">BAILEYS</option></select></label>
      <p>WHATSAPP · OPENWA · SENDER_POOL · fallback NONE. Several READY senders can serve the one engine-specific pool.</p>
      <label>Gateway pool ID<input value={form.gateway} onChange={e => change('gateway', e.target.value)} required /></label>
      <label>Sender pool ID<input value={form.pool} onChange={e => change('pool', e.target.value)} required /></label>
      <label>Adapter version<input value={form.adapter} onChange={e => change('adapter', e.target.value)} maxLength={100} required /></label>
      <label>Routing policy version<input value={form.routing} onChange={e => change('routing', e.target.value)} maxLength={100} required /></label>
      <label>Capacity evidence version<input value={form.capacity} onChange={e => change('capacity', e.target.value)} maxLength={100} required /></label>
      <fieldset><legend>Required capabilities</legend>{capabilities.map(capability => <label key={capability}><input type="checkbox" checked={form.capabilities.includes(capability)} onChange={e => change('capabilities', e.target.checked ? [...form.capabilities, capability] : form.capabilities.filter(c => c !== capability))} />{capability}</label>)}</fieldset>
      <label>Save reason<textarea value={form.reason} onChange={e => change('reason', e.target.value)} minLength={8} maxLength={1000} required /></label>
    </fieldset>
    <div className="actions"><button type="submit" className="primary" disabled={busy || blocked}>Save draft</button><button type="button" disabled={busy || blocked} onClick={() => reset()}>Cancel edits</button><button type="button" disabled={busy || blocked} onClick={() => reset()}>Reset draft</button></div>
    {message && <p role={blocked ? 'alert' : 'status'}>{message}</p>}
    {server && <p>Server version {server.version} · state {server.status}. Unsaved form values are retained until explicit reload.</p>}
    {blocked && <button type="button" disabled={busy} onClick={() => { void reload(); }}>Reload server draft</button>}
  </form>;
}
