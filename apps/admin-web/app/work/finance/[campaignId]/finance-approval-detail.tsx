'use client';

import Link from 'next/link';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';

import { useAuth } from '../../../../components/auth-provider';
import { StatusBadge } from '../../../../components/status-badge';
import { apiRequest, APIError, type ListEnvelope } from '../../../../lib/api';
import { hasPermission } from '../../../../lib/session';

type CommercialStatus = 'DRAFT' | 'PENDING_APPROVAL' | 'APPROVED' | 'REJECTED' | 'REVOKED';
type CommercialRecord = {
  id: string;
  campaignId: string;
  organisationId: string;
  status: CommercialStatus;
  version: number;
  quotationReference: string;
  invoiceReference: string;
  currency: string;
  approvedRecipients: number;
  totalAmountMinor: number;
  paymentReference?: string;
  paymentReceivedAt?: string;
  createdBy: string;
  submittedBy?: string;
  approvedBy?: string;
  createdAt?: string;
  updatedAt?: string;
};
type CommercialEvent = {
  id: string;
  eventType: string;
  recordVersion: number;
  actorId: string;
  reason: string;
  occurredAt: string;
};

function timestamp(value?: string) {
  if (!value) return 'Not recorded';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? 'Invalid date' : date.toLocaleString();
}

function displayError(error: unknown) {
  if (error instanceof APIError && error.status === 409) {
    return 'The commercial record changed since the version you reviewed. Refresh the authoritative evidence before trying another decision.';
  }
  return error instanceof Error ? error.message : 'The commercial operation could not be completed.';
}

export function FinanceApprovalDetail({ campaignId }: { campaignId: string }) {
  const { session } = useAuth();
  const permitted = hasPermission(session?.permissions, 'finance.read');
  const generation = JSON.stringify([campaignId, session?.id, session?.sessionId, [...(session?.permissions ?? [])].sort()]);
  if (!permitted) return <p role="status">Commercial review requires finance.read permission.</p>;
  return <FinanceApprovalGeneration key={generation} campaignId={campaignId} />;
}

function FinanceApprovalGeneration({ campaignId }: { campaignId: string }) {
  const { session } = useAuth();
  const router = useRouter();
  const canApprove = hasPermission(session?.permissions, 'finance.approve');
  const mounted = useRef(false);
  const request = useRef(0);
  const acting = useRef(false);
  const [record, setRecord] = useState<CommercialRecord>();
  const [events, setEvents] = useState<CommercialEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [eventError, setEventError] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [conflictVersion, setConflictVersion] = useState<number>();
  const [uncertainVersion, setUncertainVersion] = useState<number>();

  const load = useCallback(async (): Promise<CommercialRecord | undefined> => {
    const sequence = ++request.current;
    setLoading(true);
    setError('');
    try {
      const value = await apiRequest<CommercialRecord>(
        '/v1/campaigns/' + encodeURIComponent(campaignId) + '/commercial-record'
      );
      if (!mounted.current || sequence !== request.current) return undefined;
      if (!value?.id || value.campaignId !== campaignId || !Number.isSafeInteger(value.version)) {
        throw new Error('Invalid authoritative commercial record.');
      }
      setRecord(value);
      setConflictVersion((previous) => previous !== undefined && value.version > previous ? undefined : previous);
      setUncertainVersion((previous) => previous !== undefined && value.version > previous ? undefined : previous);
      setEvents([]);
      setEventError('');
      try {
        const history = await apiRequest<ListEnvelope<CommercialEvent>>(
          '/v1/commercial-records/' + encodeURIComponent(value.id) + '/events?limit=50'
        );
        if (mounted.current && sequence === request.current) {
          if (!Array.isArray(history.items)) throw new Error('Invalid commercial audit history.');
          // Do not retain or render the raw evidence snapshots from this endpoint.
          setEvents(history.items.map((item) => ({
            id: item.id,
            eventType: item.eventType,
            recordVersion: item.recordVersion,
            actorId: item.actorId,
            reason: item.reason,
            occurredAt: item.occurredAt
          })));
          if (history.hasMore) setEventError('Only the latest 50 events are displayed; additional immutable evidence may exist.');
        }
      } catch (historyError) {
        if (mounted.current && sequence === request.current) {
          setEventError(displayError(historyError));
        }
      }
      return value;
    } catch (loadError) {
      if (mounted.current && sequence === request.current) setError(displayError(loadError));
      return undefined;
    } finally {
      if (mounted.current && sequence === request.current) setLoading(false);
    }
  }, [campaignId]);

  useEffect(() => {
    mounted.current = true;
    // Synchronise with the existing server-owned commercial record.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
    return () => {
      mounted.current = false;
      request.current += 1;
    };
  }, [load]);

  const independent = Boolean(
    session?.id && record && session.id !== record.createdBy && session.id !== record.submittedBy
  );
  const eligible = Boolean(record && record.status === 'PENDING_APPROVAL' && canApprove && independent);
  const blocked = conflictVersion !== undefined || uncertainVersion !== undefined;

  async function decide(approve: boolean) {
    if (!eligible || !record || blocked || busy || acting.current || reason.trim().length < 5) return;
    acting.current = true;
    const current = record;
    setBusy(true);
    setNotice('');
    setError('');
    try {
      await apiRequest('/v1/commercial-records/' + encodeURIComponent(current.id) + '/decision', {
        method: 'POST',
        body: JSON.stringify({
          expectedVersion: current.version,
          approve,
          reason: reason.trim()
        })
      });
      if (!mounted.current) return;
      // The mutation response is not sufficient approval evidence. Read back.
      const authoritative = await load();
      if (!mounted.current) return;
      const expectedStatus = approve ? 'APPROVED' : 'REJECTED';
      if (!authoritative || authoritative.id !== current.id ||
          authoritative.version <= current.version ||
          authoritative.status !== expectedStatus) {
        setUncertainVersion(current.version);
        setNotice('Decision was submitted but readback is unconfirmed. Do not retry before authorised reconciliation.');
        return;
      }
      setReason('');
      setNotice((approve ? 'Approved' : 'Rejected') + ' and confirmed by server readback.');
    } catch (cause) {
      if (!mounted.current) return;
      if (cause instanceof APIError && cause.status === 403 && cause.code.includes('STEP_UP')) {
        router.push('/step-up?returnTo=' + encodeURIComponent('/work/finance/' + campaignId));
        return;
      }
      if (cause instanceof APIError && cause.status === 409) {
        setConflictVersion(current.version);
      } else {
        // A lost POST response is never evidence that the decision did not commit.
        setUncertainVersion(current.version);
      }
      setError(displayError(cause));
    } finally {
      acting.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  return (
    <div className="workspace-stack">
      <section className="card">
        <div className="section-heading">
          <div>
            <h2>Finance evidence and independent decision</h2>
            <p className="muted">A finance reader can inspect this record without campaign.read. The server enforces finance approval authority and maker/checker separation.</p>
          </div>
          <Link href="/work">Back to My Work</Link>
        </div>
        <button type="button" onClick={() => void load()} disabled={loading || busy}>Refresh commercial evidence</button>
        {loading ? <p role="status">Loading authoritative commercial record…</p> : null}
        {error ? <p role="alert">{error}</p> : null}
        {notice ? <p role="status">{notice}</p> : null}
        {record ? (
          <dl className="definition-list">
            <div><dt>Commercial record</dt><dd>{record.id}</dd></div>
            <div><dt>Organisation</dt><dd>{record.organisationId}</dd></div>
            <div><dt>Campaign</dt><dd>{record.campaignId}</dd></div>
            <div><dt>Quotation</dt><dd>{record.quotationReference}</dd></div>
            <div><dt>Invoice</dt><dd>{record.invoiceReference}</dd></div>
            <div><dt>Approved audience</dt><dd>{record.approvedRecipients.toLocaleString()} recipients</dd></div>
            <div><dt>Total</dt><dd>{record.totalAmountMinor.toLocaleString()} {record.currency} minor units</dd></div>
            <div><dt>Payment evidence</dt><dd>{record.paymentReference || 'Not recorded'}</dd></div>
            <div><dt>Payment received</dt><dd>{timestamp(record.paymentReceivedAt)}</dd></div>
            <div><dt>Status</dt><dd><StatusBadge status={record.status} /></dd></div>
            <div><dt>Record version</dt><dd>{record.version}</dd></div>
          </dl>
        ) : null}
        {record && !independent ? <p role="status">Your submission must be decided by another finance checker.</p> : null}
        {record && !canApprove ? <p className="muted">This is a read-only finance view; approval requires finance.approve.</p> : null}
      </section>
      {record && record.status === 'PENDING_APPROVAL' && canApprove ? (
        <form className="card form-stack" onSubmit={(event) => { event.preventDefault(); void decide(true); }}>
          <h2>Independent finance decision</h2>
          <p className="muted">Check approved recipients, invoice, payment evidence and current version. A decision does not automatically release a campaign.</p>
          <label htmlFor="finance-decision-reason">Decision reason</label>
          <textarea id="finance-decision-reason" rows={3} minLength={5} maxLength={1000} required
            value={reason} onChange={(event) => setReason(event.target.value)}
            aria-describedby="finance-reason-hint" disabled={!eligible || blocked || busy} />
          <small id="finance-reason-hint">At least 5 characters. Do not record personal contact details in the reason.</small>
          {blocked ? <p role="alert">Decision controls are locked until newer server evidence is verified. No automatic retry will occur.</p> : null}
          <div className="button-row">
            <button className="primary" type="submit" disabled={!eligible || blocked || busy || reason.trim().length < 5}>Approve evidence</button>
            <button className="secondary" type="button" disabled={!eligible || blocked || busy || reason.trim().length < 5}
              onClick={() => void decide(false)}>
              Reject evidence
            </button>
          </div>
        </form>
      ) : null}
      <section className="card">
        <h2>Immutable commercial history</h2>
        <p className="muted">Events are server-owned. This view intentionally omits raw evidence snapshots; consult the authorised source for formal evidence exports.</p>
        {eventError ? <p role="alert">{eventError}</p> : null}
        {events.length ? <div className="table-wrap"><table>
          <thead><tr><th>Version</th><th>Event</th><th>Actor</th><th>Reason</th><th>When</th></tr></thead>
          <tbody>{events.map((item) => <tr key={item.id}>
            <td>{item.recordVersion}</td>
            <td>{item.eventType}</td>
            <td>{item.actorId}</td>
            <td>{item.reason}</td>
            <td>{timestamp(item.occurredAt)}</td>
          </tr>)}</tbody>
        </table></div> : <p>No commercial events loaded.</p>}
      </section>
    </div>
  );
}
