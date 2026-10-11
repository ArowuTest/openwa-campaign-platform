'use client';

import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';

import { useAuth } from '../../../components/auth-provider';
import { StatusBadge } from '../../../components/status-badge';
import { APIError, apiRequest, type ListEnvelope } from '../../../lib/api';
import { type CampaignDetail } from '../../../lib/campaign-preparation-model';
import { hasPermission } from '../../../lib/session';

type CommercialStatus = 'DRAFT' | 'PENDING_APPROVAL' | 'APPROVED' | 'REJECTED' | 'REVOKED';

type CommercialRecord = {
  id: string;
  campaignId: string;
  organisationId: string;
  quotationReference: string;
  invoiceReference: string;
  currency: string;
  approvedRecipients: number;
  unitPriceMinor: number;
  managementFeeMinor: number;
  totalAmountMinor: number;
  paymentReference?: string;
  paymentReceivedAt?: string;
  status: CommercialStatus;
  version: number;
  createdBy: string;
  submittedBy?: string;
  approvedBy?: string;
  reason: string;
  createdAt: string;
  updatedAt: string;
};

type CommercialEvent = {
  id: string;
  commercialRecordId: string;
  eventType: string;
  actorId: string;
  reason: string;
  recordVersion: number;
  evidence: Record<string, unknown>;
  occurredAt: string;
};

const visibleStatuses = new Set([
  'MESSAGE_APPROVED', 'COMMERCIAL_APPROVED', 'FINAL_APPROVAL_PENDING',
  'SCHEDULED', 'DISPATCHING', 'PAUSED', 'CANCELLED', 'COMPLETED', 'COMPLETED_WITH_EXCEPTIONS'
]);

function isStepUp(error: unknown): boolean {
  return error instanceof APIError && error.status === 403 && error.code.includes('STEP_UP');
}

function safeInteger(value: FormDataEntryValue | null, label: string): number {
  const raw = String(value ?? '').trim();
  if (!/^[0-9]+$/.test(raw)) throw new Error(label + ' must be a whole non-negative number.');
  const number = Number(raw);
  if (!Number.isSafeInteger(number) || number < 0) {
    throw new Error(label + ' exceeds the browser-safe integer range.');
  }
  return number;
}

function commercialPayload(data: FormData, expectedVersion?: number) {
  const approvedRecipients = safeInteger(data.get('approvedRecipients'), 'Approved recipients');
  if (approvedRecipients < 1) throw new Error('Approved recipients must be at least 1.');
  const unitPriceMinor = safeInteger(data.get('unitPriceMinor'), 'Unit price');
  const managementFeeMinor = safeInteger(data.get('managementFeeMinor'), 'Management fee');
  const total = BigInt(approvedRecipients) * BigInt(unitPriceMinor) + BigInt(managementFeeMinor);
  if (total > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('Calculated commercial total exceeds the browser-safe integer range.');

  const paymentRaw = String(data.get('paymentReceivedAt') ?? '').trim();
  let paymentReceivedAt: string | undefined;
  if (paymentRaw) {
    const parsed = new Date(paymentRaw);
    if (Number.isNaN(parsed.valueOf())) throw new Error('Payment received time is invalid.');
    paymentReceivedAt = parsed.toISOString();
  }

  return {
    ...(expectedVersion ? { expectedVersion } : {}),
    quotationReference: String(data.get('quotationReference') ?? '').trim(),
    invoiceReference: String(data.get('invoiceReference') ?? '').trim(),
    currency: String(data.get('currency') ?? '').trim().toUpperCase(),
    approvedRecipients,
    unitPriceMinor,
    managementFeeMinor,
    paymentReference: String(data.get('paymentReference') ?? '').trim(),
    ...(paymentReceivedAt ? { paymentReceivedAt } : {}),
    reason: String(data.get('reason') ?? '').trim()
  };
}

function localDateTime(value?: string): string {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.valueOf())) return '';
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.valueOf() - offset).toISOString().slice(0, 16);
}

export function CommercialGovernance({
  campaign,
  pilotEvidenceReady,
  onChanged
}: {
  campaign: CampaignDetail;
  pilotEvidenceReady: boolean;
  onChanged: () => Promise<void>;
}) {
  const router = useRouter();
  const { session } = useAuth();
  const canRead = hasPermission(session?.permissions, 'finance.read');
  const canReadCommercial = canRead || hasPermission(session?.permissions, 'campaign.approve');
  const canWrite = hasPermission(session?.permissions, 'finance.write');
  const canApprove = hasPermission(session?.permissions, 'finance.approve');
  const canApproveCampaign = hasPermission(session?.permissions, 'campaign.approve');
  const visible = visibleStatuses.has(campaign.status);

  const [record, setRecord] = useState<CommercialRecord>();
  const [events, setEvents] = useState<CommercialEvent[]>([]);
  const [eventsNextCursor, setEventsNextCursor] = useState('');
  const [eventsHasMore, setEventsHasMore] = useState(false);
  const eventsRequestActive = useRef(false);
  const [eventsLoading, setEventsLoading] = useState(false);
  const [eventsError, setEventsError] = useState('');
  const [notFound, setNotFound] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');

  const loadEvents = useCallback(async (recordId: string, cursor = '', append = false) => {
    if (!canRead || eventsRequestActive.current) return;
    eventsRequestActive.current = true;
    setEventsLoading(true);
    setEventsError('');
    try {
      const query = cursor ? new URLSearchParams({ limit: '100', cursor }) : new URLSearchParams();
      const suffix = query.toString() ? '?' + query.toString() : '';
      const history = await apiRequest<ListEnvelope<CommercialEvent>>(
        '/v1/commercial-records/' + encodeURIComponent(recordId) + '/events' + suffix
      );
      setEvents(current => append ? [...current, ...(history.items ?? [])] : (history.items ?? []));
      setEventsNextCursor(history.nextCursor ?? '');
      setEventsHasMore(Boolean(history.hasMore));
    } catch (error) {
      if (append) {
        setEventsError(error instanceof Error ? error.message : 'Commercial event history could not be loaded.');
      }
      throw error;
    } finally {
      eventsRequestActive.current = false;
      setEventsLoading(false);
    }
  }, [canRead]);

  const load = useCallback(async () => {
    if (!visible || !canReadCommercial) return;
    setLoading(true);
    setLoadError('');
    setEventsError('');
    try {
      const value = await apiRequest<CommercialRecord>(
        '/v1/campaigns/' + encodeURIComponent(campaign.id) + '/commercial-record'
      );
      setRecord(value);
      setNotFound(false);
      if (canRead) {
        await loadEvents(value.id);
      } else {
        setEvents([]);
        setEventsNextCursor('');
        setEventsHasMore(false);
      }
      return true;
    } catch (error) {
      if (error instanceof APIError && error.status === 404 && error.code === 'COMMERCIAL_NOT_FOUND') {
        setRecord(undefined);
        setEvents([]);
        setEventsNextCursor('');
        setEventsHasMore(false);
        setNotFound(true);
        return true;
      }
      setLoadError(error instanceof Error ? error.message : 'Commercial evidence could not be loaded.');
      return false;
    } finally {
      setLoading(false);
    }
  }, [campaign.id, canRead, canReadCommercial, loadEvents, visible]);

  useEffect(() => {
    if (!visible || !canReadCommercial) return;
    // The load callback synchronizes remote campaign state into local state.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [canReadCommercial, load, visible]);

  const independentChecker = useMemo(() => {
    if (!record || !session?.id) return false;
    return session.id !== record.createdBy && session.id !== record.submittedBy;
  }, [record, session?.id]);

  if (!visible || !canReadCommercial) return null;

  async function mutate(label: string, action: () => Promise<unknown>, refreshCampaign = false) {
    if (busy) return;
    setBusy(label);
    setNotice('');
    try {
      await action();
      const refreshed = await load();
      if (!refreshed) {
        setNotice(label + ' completed, but the authoritative readback failed. Retry refresh before taking another action.');
        return;
      }
      if (refreshCampaign) await onChanged();
      setNotice(label + ' completed. Commercial evidence has been refreshed.');
    } catch (error) {
      if (isStepUp(error)) {
        router.push('/step-up?returnTo=' + encodeURIComponent('/campaigns/' + campaign.id));
        return;
      }
      if (error instanceof APIError && error.status === 409) {
        const refreshed = await load();
        setNotice(refreshed
          ? label + ' was rejected because the record changed. The latest commercial evidence has been loaded.'
          : label + ' was rejected because the record changed, but the latest commercial evidence could not be loaded. Retry refresh before taking another action.');
        return;
      }
      setNotice(error instanceof Error ? error.message : label + ' failed.');
    } finally {
      setBusy('');
    }
  }

  async function saveDraft(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canWrite || busy) return;
    let payload;
    try {
      payload = commercialPayload(new FormData(event.currentTarget), record?.version);
    } catch (error) {
      setNotice(error instanceof Error ? error.message : 'Commercial evidence is invalid.');
      return;
    }
    if (payload.reason.length < 5) {
      setNotice('Enter a meaningful commercial evidence reason.');
      return;
    }
    const path = record
      ? '/v1/commercial-records/' + encodeURIComponent(record.id) + '/draft'
      : '/v1/campaigns/' + encodeURIComponent(campaign.id) + '/commercial-record';
    await mutate(record ? 'Correct commercial draft' : 'Create commercial draft', () => apiRequest(path, {
      method: record ? 'PUT' : 'POST',
      body: JSON.stringify(payload)
    }));
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!record || !canWrite || busy) return;
    const data = new FormData(event.currentTarget);
    const reason = String(data.get('submitReason') ?? '').trim();
    await mutate('Submit commercial evidence', () => apiRequest('/v1/commercial-records/' + encodeURIComponent(record.id) + '/submit', {
      method: 'POST',
      body: JSON.stringify({ expectedVersion: record.version, reason })
    }));
  }

  async function decide(approve: boolean, form: HTMLFormElement) {
    if (!record || !canApprove || !independentChecker || busy) return;
    const data = new FormData(form);
    const reason = String(data.get('decisionReason') ?? '').trim();
    await mutate(approve ? 'Approve commercial evidence' : 'Reject commercial evidence', () => apiRequest(
      '/v1/commercial-records/' + encodeURIComponent(record.id) + '/decision',
      {
        method: 'POST',
        body: JSON.stringify({ expectedVersion: record.version, approve, reason })
      }
    ));
  }

  async function revoke(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!record || !canApprove || busy) return;
    const reason = String(new FormData(event.currentTarget).get('revokeReason') ?? '').trim();
    await mutate('Revoke commercial approval', () => apiRequest('/v1/commercial-records/' + encodeURIComponent(record.id) + '/revoke', {
      method: 'POST',
      body: JSON.stringify({ expectedVersion: record.version, reason })
    }), campaign.commercialApprovalId === record.id);
  }

  async function bindCampaignApproval() {
    if (!record || record.status !== 'APPROVED' || campaign.status !== 'MESSAGE_APPROVED' || !canApproveCampaign || !pilotEvidenceReady || busy) return;
    await mutate('Record campaign commercial approval', () => apiRequest('/v1/campaigns/' + encodeURIComponent(campaign.id) + '/transition', {
      method: 'POST',
      body: JSON.stringify({
        action: 'APPROVE_COMMERCIAL',
        expectedVersion: campaign.version,
        reason: 'Approved commercial entitlement verified after controlled test and capacity hold'
      })
    }), true);
  }

  async function loadOlderEvents() {
    if (!record || !eventsHasMore || !eventsNextCursor || eventsLoading) return;
    try {
      await loadEvents(record.id, eventsNextCursor, true);
    } catch {
      // The visible eventsError explains the failed page and keeps the cursor retryable.
    }
  }

  const editable = !record || record.status === 'DRAFT' || record.status === 'REJECTED' || record.status === 'REVOKED';
  const canCreate = campaign.status === 'MESSAGE_APPROVED' && !record && notFound;

  return (
    <section className="card form-stack" aria-label="Commercial governance">
      <div className="section-heading">
        <div>
          <h2>Commercial governance</h2>
          <p className="muted">Record offline quotation, invoice and payment evidence. Commercial approval is independent of the campaign state until the explicit campaign transition succeeds.</p>
        </div>
        {record ? <StatusBadge status={record.status} /> : <span className="pill">No record</span>}
      </div>

      {notice ? <div className="alert alert-information" role="status" aria-live="polite">{notice}</div> : null}
      {loadError ? (
        <div className="alert alert-danger" role="alert">
          <span>{loadError}</span>
          <button type="button" className="secondary" onClick={() => void load()}>Retry readback</button>
        </div>
      ) : null}
      {loading ? <div className="alert alert-information" role="status" aria-live="polite">Loading commercial evidence…</div> : null}

      {(editable && canWrite && (record || canCreate)) ? (
        <form key={record ? record.id + ':' + record.version : 'new-commercial'} className="form-stack" onSubmit={saveDraft}>
          <div className="grid grid-2">
            <label>Quotation reference<input name="quotationReference" required defaultValue={record?.quotationReference ?? ''} /></label>
            <label>Invoice reference<input name="invoiceReference" required defaultValue={record?.invoiceReference ?? ''} /></label>
            <label>Currency<input name="currency" required minLength={3} maxLength={3} defaultValue={record?.currency ?? 'NGN'} /></label>
            <label>Approved recipients<input name="approvedRecipients" type="number" min="1" max={Number.MAX_SAFE_INTEGER} required defaultValue={record?.approvedRecipients ?? campaign.maximumUniqueRecipients} /></label>
            <label>Unit price (minor units)<input name="unitPriceMinor" type="number" min="0" max={Number.MAX_SAFE_INTEGER} required defaultValue={record?.unitPriceMinor ?? 0} /></label>
            <label>Management fee (minor units)<input name="managementFeeMinor" type="number" min="0" max={Number.MAX_SAFE_INTEGER} required defaultValue={record?.managementFeeMinor ?? 0} /></label>
            <label>Payment reference<input name="paymentReference" defaultValue={record?.paymentReference ?? ''} /></label>
            <label>Payment received at<input name="paymentReceivedAt" type="datetime-local" defaultValue={localDateTime(record?.paymentReceivedAt)} /></label>
          </div>
          <label>Evidence reason<textarea name="reason" minLength={5} rows={2} required defaultValue={record?.status === 'REJECTED' ? 'Correct rejected commercial evidence' : 'Record offline commercial evidence'} /></label>
          <small>Total is derived from approved recipients × unit price + management fee and revalidated by the server.</small>
          <button className="primary" disabled={Boolean(busy)}>{record ? 'Save corrected draft' : 'Create commercial draft'}</button>
        </form>
      ) : null}

      {!loading && !loadError && !record ? (
        <div className="empty-state">
          <strong>No commercial record</strong>
          <p>Commercial evidence becomes actionable after message approval and remains governed by finance permissions.</p>
        </div>
      ) : null}

      {record ? (
        <>
          <dl className="definition-list">
            <div><dt>Quotation</dt><dd>{record.quotationReference}</dd></div>
            <div><dt>Invoice</dt><dd>{record.invoiceReference}</dd></div>
            <div><dt>Entitlement</dt><dd>{record.approvedRecipients.toLocaleString()} recipients</dd></div>
            <div><dt>Currency</dt><dd>{record.currency}</dd></div>
            <div><dt>Total</dt><dd>{record.totalAmountMinor.toLocaleString()} minor units</dd></div>
            <div><dt>Payment</dt><dd>{record.paymentReference || 'Not recorded'}</dd></div>
            <div><dt>Payment time</dt><dd>{record.paymentReceivedAt ? new Date(record.paymentReceivedAt).toLocaleString() : 'Not recorded'}</dd></div>
            <div><dt>Record version</dt><dd>{record.version}</dd></div>
          </dl>

          {record.status === 'DRAFT' && canWrite ? (
            <form className="form-stack" onSubmit={submit}>
              <label>Submission reason<textarea name="submitReason" minLength={5} required rows={2} /></label>
              <button className="secondary" disabled={Boolean(busy)}>Submit for independent finance approval</button>
            </form>
          ) : null}

          {record.status === 'PENDING_APPROVAL' && canApprove ? (
            <form className="form-stack" onSubmit={event => event.preventDefault()}>
              <h3>Independent finance decision</h3>
              {!independentChecker ? <div className="alert alert-information">Maker/checker separation: the creator or submitter cannot decide this record.</div> : null}
              <label>Decision reason<textarea name="decisionReason" minLength={5} required rows={2} /></label>
              <div className="button-row">
                <button className="primary" type="button" disabled={Boolean(busy) || !independentChecker} onClick={event => {
                  const form = event.currentTarget.form!;
                  if (form.reportValidity()) void decide(true, form);
                }}>Approve commercial evidence</button>
                <button className="secondary" type="button" disabled={Boolean(busy) || !independentChecker} onClick={event => {
                  const form = event.currentTarget.form!;
                  if (form.reportValidity()) void decide(false, form);
                }}>Reject commercial evidence</button>
              </div>
            </form>
          ) : null}

          {record.status === 'APPROVED' && campaign.status === 'MESSAGE_APPROVED' ? (
            <div className="form-stack">
              <h3>Campaign commercial transition</h3>
              <p className="muted">The approved record does not change the campaign automatically. The server revalidates campaign, organisation and recipient entitlement during APPROVE_COMMERCIAL.</p>
              {!pilotEvidenceReady ? <small>Complete an accepted exact-route controlled test and authoritative capacity hold before advancing the campaign.</small> : null}
              <button className="primary" type="button" disabled={Boolean(busy) || !canApproveCampaign || !pilotEvidenceReady} onClick={() => void bindCampaignApproval()}>Record campaign commercial approval</button>
            </div>
          ) : null}

          {record.status === 'APPROVED' && canApprove ? (
            <form className="form-stack" onSubmit={revoke}>
              <label>Revocation reason<textarea name="revokeReason" minLength={5} required rows={2} /></label>
              <button className="danger-ghost" disabled={Boolean(busy)}>Revoke commercial approval</button>
            </form>
          ) : null}

          <div className="table-wrap" tabIndex={0} aria-label="Commercial event history">
            {!canRead ? <div className="alert alert-information" role="status">Immutable event history is restricted to finance readers.</div> : null}
            <table>
              <caption className="sr-only">Immutable commercial event history</caption>
              <thead><tr><th scope="col">Version</th><th scope="col">Event</th><th scope="col">Actor</th><th scope="col">Reason</th><th scope="col">Evidence</th><th scope="col">When</th></tr></thead>
              <tbody>{events.map(event => (
                <tr key={event.id}>
                  <td>v{event.recordVersion}</td><td>{event.eventType}</td><td>{event.actorId}</td>
                  <td>{event.reason}</td>
                  <td><details><summary>View snapshot</summary><pre>{JSON.stringify(event.evidence, null, 2)}</pre></details></td>
                  <td>{new Date(event.occurredAt).toLocaleString()}</td>
                </tr>
              ))}</tbody>
            </table>
            {eventsError ? (
              <div className="alert alert-danger" role="alert">
                <span>{eventsError}</span>
                <button type="button" className="secondary" onClick={() => void loadOlderEvents()}>Retry older evidence</button>
              </div>
            ) : null}
            {eventsHasMore && record ? (
              <button type="button" className="secondary" disabled={Boolean(busy) || eventsLoading} onClick={() => void loadOlderEvents()}>
                {eventsLoading ? 'Loading older evidence…' : 'Load older evidence'}
              </button>
            ) : null}
          </div>
        </>
      ) : null}
    </section>
  );
}
