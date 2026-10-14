'use client';

import Link from 'next/link';
import { FormEvent, useEffect, useRef, useState } from 'react';

import { useAuth } from '../../../components/auth-provider';
import { APIError, apiRequest } from '../../../lib/api';
import { type CampaignDetail, canonicalUUID } from '../../../lib/campaign-preparation-model';
import { hasPermission } from '../../../lib/session';

function parseUTC(value: string): Date | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) return;
  const date = new Date(value + ':00.000Z');
  if (Number.isNaN(date.getTime()) || date.toISOString().slice(0, 16) !== value) return;
  return date;
}

// Sample the wall clock only from user events, never during a React render.
function wallClockNow(): number { return Date.now(); }

type CloneOutcome = { id: string; name: string; status: string };

export function CampaignClonePanel({ campaign, canWrite }: {
  campaign: CampaignDetail;
  canWrite: boolean;
}) {
  const { session } = useAuth();
  const allowed = canWrite && hasPermission(session?.permissions, 'campaign.write');
  const mounted = useRef(false);
  const submitted = useRef(false);
  const [name, setName] = useState((campaign.name + ' (Copy)').slice(0, 200));
  const [startUTC, setStartUTC] = useState('');
  const [deadlineUTC, setDeadlineUTC] = useState('');
  const [validationNow, setValidationNow] = useState(0);
  const [maxRecipients, setMaxRecipients] = useState(String(campaign.maximumUniqueRecipients));
  const [acknowledged, setAcknowledged] = useState(false);
  const [busy, setBusy] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const [outcome, setOutcome] = useState<CloneOutcome>();
  const [error, setError] = useState('');

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  const start = parseUTC(startUTC);
  const deadline = parseUTC(deadlineUTC);
  const count = Number(maxRecipients);
  const validSchedule = Boolean(
    start && deadline &&
    validationNow > 0 && start.getTime() > validationNow &&
    deadline.getTime() > start.getTime()
  );
  const validCount = Number.isSafeInteger(count) && count > 0;
  const ready = allowed && !busy && !attempted && !outcome &&
    name.trim().length >= 2 && validSchedule && validCount && acknowledged;

  async function clone(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!ready || submitted.current || !start || !deadline) return;
    // Re-check against the actual clock at submission; an old open form must
    // never submit an already expired dispatch window.
    if (start.getTime() <= wallClockNow()) {
      setError('The selected start time has passed. Enter a new future UTC window.');
      setValidationNow(wallClockNow());
      return;
    }
    submitted.current = true;
    setAttempted(true);
    setBusy(true);
    setError('');
    try {
      const result = await apiRequest<CampaignDetail>(
        '/v1/campaigns/' + encodeURIComponent(canonicalUUID(campaign.id)) + '/clone',
        {
          method: 'POST',
          body: JSON.stringify({
            name: name.trim(),
            requestedStartAt: start.toISOString(),
            completionDeadlineAt: deadline.toISOString(),
            timezone: campaign.timezone,
            quietHoursStart: campaign.quietHoursStart || '',
            quietHoursEnd: campaign.quietHoursEnd || '',
            maximumUniqueRecipients: count
          })
        }
      );
      if (!mounted.current) return;
      if (!result?.id || canonicalUUID(result.id) === canonicalUUID(campaign.id) ||
          result.organisationId !== campaign.organisationId ||
          result.createdBy !== session?.id || result.status !== 'DRAFT' ||
          result.audienceSnapshotId || result.audienceSnapshotHash ||
          result.messageVersionId || result.messageContentHash ||
          result.commercialApprovalId || result.finalApprovedBy ||
          result.eligibleAudienceCount !== 0 ||
          Date.parse(result.requestedStartAt || '') !== start.getTime() ||
          Date.parse(result.completionDeadlineAt || '') !== deadline.getTime()) {
        throw new Error('The clone response did not prove a fresh, unapproved DRAFT. Reconcile the campaign register before any retry.');
      }
      setOutcome({ id: result.id, name: result.name, status: result.status });
    } catch (cause) {
      if (!mounted.current) return;
      const detail = cause instanceof Error ? cause.message : 'The clone response could not be confirmed.';
      const forbidden = cause instanceof APIError && (cause.status === 401 || cause.status === 403);
      setError(
        (forbidden ? 'Clone denied by the platform. ' : 'Clone outcome is unconfirmed. ') +
        detail + ' Do not resubmit until the campaign register has been reconciled.'
      );
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  return (
    <section className="card form-stack" aria-labelledby="clone-campaign-title">
      <h2 id="clone-campaign-title">Clone into a new draft</h2>
      <p className="muted">
        Copy reusable campaign preparation settings into a separate DRAFT. This does not
        copy an authorised audience snapshot, message approval, commercial approval,
        accepted controlled test, release permission or active capacity reservation.
        All evidence must be rechecked before a cloned campaign can dispatch.
      </p>
      {!allowed ? (
        <p role="status">Cloning requires campaign.write permission. The source remains available read-only.</p>
      ) : (
        <>
          <dl className="definition-list">
            <div><dt>Source</dt><dd>{campaign.name}</dd></div>
            <div><dt>Organisation</dt><dd>{campaign.organisationId}</dd></div>
            <div><dt>Original campaign zone</dt><dd>{campaign.timezone || 'Not configured'}</dd></div>
            <div><dt>Quiet hours</dt><dd>{campaign.quietHoursStart || 'No limit'} / {campaign.quietHoursEnd || 'No limit'}</dd></div>
          </dl>
          {outcome ? (
            <div className="alert alert-information" role="status">
              <strong>New DRAFT confirmed: {outcome.name}</strong>
              <p>Open the new draft and revalidate consent, message, audience, route, finance and final approval.</p>
              <Link href={'/campaigns/' + encodeURIComponent(canonicalUUID(outcome.id))}>Open cloned draft</Link>
            </div>
          ) : (
            <form className="form-stack" onSubmit={(event) => void clone(event)}>
              <label htmlFor="clone-campaign-name">New campaign name</label>
              <input id="clone-campaign-name" value={name}
                onChange={(event) => setName(event.target.value)} minLength={2} maxLength={200}
                required disabled={busy || attempted} />
              <label htmlFor="clone-start-utc">New requested start (UTC)</label>
              <input id="clone-start-utc" type="datetime-local" value={startUTC}
                onChange={(event) => { setStartUTC(event.target.value); setValidationNow(wallClockNow()); }}
                aria-describedby="clone-utc-hint" required disabled={busy || attempted} />
              <label htmlFor="clone-deadline-utc">New completion deadline (UTC)</label>
              <input id="clone-deadline-utc" type="datetime-local" value={deadlineUTC}
                onChange={(event) => { setDeadlineUTC(event.target.value); setValidationNow(wallClockNow()); }}
                aria-describedby="clone-utc-hint" required disabled={busy || attempted} />
              <small id="clone-utc-hint">
                Enter both times explicitly in UTC, not local browser time. The start must
                be in the future, and the deadline must follow it. The original
                {campaign.timezone ? ' ' + campaign.timezone : ''} time zone and quiet-hours policy are
                retained for server revalidation.
              </small>
              <label htmlFor="clone-max-recipients">Maximum unique recipients</label>
              <input id="clone-max-recipients" type="number" min={1} step={1}
                value={maxRecipients} onChange={(event) => setMaxRecipients(event.target.value)}
                required disabled={busy || attempted} />
              {validSchedule ? (
                <p className="muted" role="status">
                  Requested UTC window: {start?.toISOString()} to {deadline?.toISOString()}.
                </p>
              ) : <p className="muted">A valid new future UTC window is required to prevent inheriting the source campaign's possibly expired dates.</p>}
              <label className="checkbox-row">
                <input type="checkbox" checked={acknowledged}
                  onChange={(event) => setAcknowledged(event.target.checked)}
                  disabled={busy || attempted} />
                <span>I understand the new draft requires fresh operational evidence and independent approvals before sending.</span>
              </label>
              <button className="primary" type="submit" disabled={!ready}>
                {busy ? 'Creating draft…' : 'Create separate draft'}
              </button>
              {error ? <p role="alert">{error}</p> : null}
            </form>
          )}
        </>
      )}
    </section>
  );
}
