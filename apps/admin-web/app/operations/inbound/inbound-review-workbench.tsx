'use client';

import { FormEvent, useCallback, useEffect, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';

import { useAuth } from '../../../components/auth-provider';
import { StatusBadge } from '../../../components/status-badge';
import { apiRequest, APIError, type ListEnvelope } from '../../../lib/api';
import { hasPermission } from '../../../lib/session';

type Classification = 'UNREVIEWED' | 'OPT_OUT' | 'QUESTION' | 'COMPLAINT' | 'OTHER';
type ReviewClassification = Exclude<Classification, 'UNREVIEWED'>;

type ReplyMetadata = {
  id: string;
  eventId: string;
  contactId: string;
  campaignId: string;
  sessionId: string;
  classification: Classification;
  escalated: boolean;
  occurredAt: string;
  createdAt: string;
  reviewedAt?: string | null;
  contentRetainUntil?: string;
  contentRedactedAt?: string | null;
  legalHold: boolean;
  version: number;
};

// Review and legal-hold mutations can return additional server-owned details;
// the workbench must project their responses back to the minimal list contract.
type InboundReply = ReplyMetadata & {
  escalationReason?: string;
  messageText?: string;
  messageFingerprint?: string;
  legalHoldReason?: string;
  legalHoldAppliedBy?: string;
  legalHoldReleasedBy?: string;
};

type RetainedContent = {
  id: string;
  messageText: string;
  fingerprint?: string;
  retainUntil?: string;
  legalHold: boolean;
};

type ConsentEvent = {
  id: string;
  suppressionId?: string;
  eventType: string;
  reason?: string;
  sourceReference?: string;
  occurredAt: string;
};

type SuppressionResponse = {
  created: boolean;
  suppression: { id: string; active: boolean; scope: string; version: number };
};

const classifications: Array<{ value: ReviewClassification; name: string }> = [
  { value: 'OPT_OUT', name: 'Opt-out evidence' },
  { value: 'QUESTION', name: 'Question' },
  { value: 'COMPLAINT', name: 'Complaint' },
  { value: 'OTHER', name: 'Other' }
];

function metadata(reply: InboundReply): ReplyMetadata {
  // Explicit projection prevents future mutation-response fields from entering
  // the broad work-queue state, even if an API changes unexpectedly.
  return {
    id: reply.id,
    eventId: reply.eventId,
    contactId: reply.contactId,
    campaignId: reply.campaignId,
    sessionId: reply.sessionId,
    classification: reply.classification,
    escalated: reply.escalated,
    occurredAt: reply.occurredAt,
    createdAt: reply.createdAt,
    reviewedAt: reply.reviewedAt,
    contentRetainUntil: reply.contentRetainUntil,
    contentRedactedAt: reply.contentRedactedAt,
    legalHold: reply.legalHold,
    version: reply.version
  };
}

function maskReference(value?: string): string {
  const clean = String(value ?? '').trim();
  return clean ? '…' + clean.slice(-6) : 'Not supplied';
}

function humanTime(value?: string | null): string {
  if (!value) return 'Not recorded';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? 'Invalid timestamp' : date.toLocaleString();
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError && error.status === 409) {
    return 'The record has changed. Refresh server evidence before attempting another action. No automatic retry has been made.';
  }
  return error instanceof Error ? error.message : fallback;
}

function isStepUp(error: unknown) {
  return error instanceof APIError && error.status === 403 && error.code.includes('STEP_UP');
}

function requestKey(): string {
  if (typeof globalThis.crypto?.randomUUID === 'function') return globalThis.crypto.randomUUID();
  if (typeof globalThis.crypto?.getRandomValues === 'function') {
    const bytes = globalThis.crypto.getRandomValues(new Uint8Array(16));
    return 'suppression-' + Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
  }
  throw new Error('Secure browser randomness is unavailable; suppression requests are disabled.');
}

export function InboundReviewWorkbench() {
  const { session } = useAuth();
  const canRead = hasPermission(session?.permissions, 'inbound.read');
  const generation = JSON.stringify([session?.id, session?.sessionId, [...(session?.permissions ?? [])].sort()]);
  if (!canRead) return <p role="status">Inbound review requires inbound.read permission.</p>;
  return <InboundReviewGeneration key={generation} />;
}

function InboundReviewGeneration() {
  const router = useRouter();
  const { session } = useAuth();
  const canReview = hasPermission(session?.permissions, 'inbound.review');
  const canReveal = hasPermission(session?.permissions, 'inbound.content.read');
  const canReadConsent = hasPermission(session?.permissions, 'consent.read');
  const canSuppress = hasPermission(session?.permissions, 'consent.write');
  const canHold = hasPermission(session?.permissions, 'privacy.admin');

  const mounted = useRef(false);
  const pageSequence = useRef(0);
  const detailSequence = useRef(0);
  const evidenceSequence = useRef(0);
  const pagesRead = useRef(0);
  const seenCursors = useRef(new Set<string>());
  const conflictedVersions = useRef(new Map<string, number>());
  const selectedRef = useRef('');
  const mutating = useRef(false);
  // Keep an unresolved suppression's exact idempotency key across selection and
  // inventory refresh. A lost response must not allow a different request for the
  // same contact to be silently minted inside this operator session.
  const pendingSuppressions = useRef(new Map<string, { key: string; payload: string }>());

  const [replies, setReplies] = useState<ReplyMetadata[]>([]);
  const [loading, setLoading] = useState(true);
  const [loaded, setLoaded] = useState(false);
  const [moreLoading, setMoreLoading] = useState(false);
  const [nextCursor, setNextCursor] = useState('');
  const [hasMore, setHasMore] = useState(false);
  const [refreshedAt, setRefreshedAt] = useState('');
  const [selectedId, setSelectedId] = useState('');
  const [classification, setClassification] = useState<ReviewClassification | ''>('');
  const [escalate, setEscalate] = useState(false);
  const [escalationReason, setEscalationReason] = useState('');
  const [legalHoldReason, setLegalHoldReason] = useState('');
  const [releaseConfirmed, setReleaseConfirmed] = useState(false);
  const [suppressionReason, setSuppressionReason] = useState('');
  const [scopeConfirmed, setScopeConfirmed] = useState(false);
  const [busy, setBusy] = useState('');
  const [conflict, setConflict] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [content, setContent] = useState<RetainedContent>();
  const [contentMessage, setContentMessage] = useState('');
  const [contentLoading, setContentLoading] = useState(false);
  const [events, setEvents] = useState<ConsentEvent[]>([]);
  const [eventsLoading, setEventsLoading] = useState(false);
  const [eventsError, setEventsError] = useState('');

  const selected = replies.find((item) => item.id === selectedId);

  const routeStepUp = useCallback(() => {
    if (mounted.current) router.push('/step-up?returnTo=' + encodeURIComponent('/operations/inbound'));
  }, [router]);

  const loadPage = useCallback(async (cursor = '', reset = false) => {
    const sequence = ++pageSequence.current;
    if (reset) {
      pagesRead.current = 0;
      seenCursors.current.clear();
      setLoading(true);
    } else {
      if (pagesRead.current >= 20) {
        setHasMore(false);
        setError('The inbound reply list is capped at 1,000 loaded records per visit. Refresh to inspect the newest evidence.');
        return;
      }
      setMoreLoading(true);
    }
    setError('');
    try {
      const page = await apiRequest<ListEnvelope<InboundReply>>(
        '/v1/inbound-replies?limit=50' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
      );
      if (!mounted.current || pageSequence.current !== sequence) return;
      if (!Array.isArray(page.items) || typeof page.hasMore !== 'boolean' || (page.hasMore && !page.nextCursor)) {
        throw new Error('Malformed inbound-reply page. The queue has not been changed.');
      }
      if (page.hasMore && (page.nextCursor === cursor || seenCursors.current.has(page.nextCursor || ''))) {
        throw new Error('Repeated inbound-reply cursor. Refresh instead of rereading the same page.');
      }
      pagesRead.current += 1;
      if (page.hasMore && page.nextCursor) seenCursors.current.add(page.nextCursor);
      const safeItems = page.items.map(metadata);
      if (reset) {
        // A conflict may only be cleared by observed newer server evidence.
        for (const item of safeItems) {
          const conflictedVersion = conflictedVersions.current.get(item.id);
          if (conflictedVersion !== undefined && item.version > conflictedVersion) {
            conflictedVersions.current.delete(item.id);
          }
        }
      }
      setReplies((existing) => {
        if (reset) return safeItems;
        const seen = new Set(existing.map((item) => item.id));
        return [...existing, ...safeItems.filter((item) => !seen.has(item.id))];
      });
      setHasMore(page.hasMore && pagesRead.current < 20);
      setNextCursor(page.nextCursor || '');
      setRefreshedAt(new Date().toISOString());
      setLoaded(true);
    } catch (cause) {
      if (!mounted.current || pageSequence.current !== sequence) return;
      if (isStepUp(cause)) routeStepUp();
      else setError(errorMessage(cause, 'Inbound reply inventory could not be loaded.'));
    } finally {
      if (mounted.current && pageSequence.current === sequence) {
        setLoading(false);
        setMoreLoading(false);
      }
    }
  }, [routeStepUp]);

  useEffect(() => {
    mounted.current = true;
    const pendingForThisSession = pendingSuppressions.current;
    const conflictsForThisSession = conflictedVersions.current;
    // The first read synchronises the server-authoritative queue; it is never local authority.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadPage('', true);
    return () => {
      mounted.current = false;
      pageSequence.current += 1;
      detailSequence.current += 1;
      evidenceSequence.current += 1;
      selectedRef.current = '';
      pendingForThisSession.clear();
      conflictsForThisSession.clear();
    };
  }, [loadPage]);

  const loadEvidence = useCallback(async (contactId: string, replyId: string) => {
    const sequence = ++evidenceSequence.current;
    setEvents([]);
    setEventsError('');
    if (!canReadConsent || !contactId) return;
    setEventsLoading(true);
    try {
      const page = await apiRequest<ListEnvelope<ConsentEvent>>(
        '/v1/consent-events?contactId=' + encodeURIComponent(contactId) + '&limit=50'
      );
      if (!mounted.current || sequence !== evidenceSequence.current || selectedRef.current !== replyId) return;
      if (!Array.isArray(page.items)) throw new Error('Malformed contact evidence returned.');
      setEvents(page.items.filter((value) => Boolean(value.suppressionId)));
      if (page.hasMore) setEventsError('Only the 50 newest contact events were loaded. Older history may exist; do not infer that suppression is absent.');
    } catch (cause) {
      if (!mounted.current || sequence !== evidenceSequence.current || selectedRef.current !== replyId) return;
      setEventsError(errorMessage(cause, 'Contact suppression history could not be loaded.'));
    } finally {
      if (mounted.current && sequence === evidenceSequence.current) setEventsLoading(false);
    }
  }, [canReadConsent]);

  function selectReply(reply: ReplyMetadata) {
    if (busy || mutating.current) return;
    selectedRef.current = reply.id;
    detailSequence.current += 1;
    evidenceSequence.current += 1;
    setSelectedId(reply.id);
    setClassification(reply.classification === 'UNREVIEWED' ? '' : reply.classification);
    setEscalate(reply.escalated);
    // Free-text case narratives are intentionally unavailable in the broad
    // metadata list. An escalated case requires a fresh approved reason.
    setEscalationReason('');
    setLegalHoldReason('');
    setReleaseConfirmed(false);
    setSuppressionReason('');
    setScopeConfirmed(false);
    setConflict(conflictedVersions.current.has(reply.id));
    setContent(undefined);
    setContentMessage('');
    setNotice('');
    setError('');
    void loadEvidence(reply.contactId, reply.id);
  }

  function refresh() {
    selectedRef.current = '';
    detailSequence.current += 1;
    evidenceSequence.current += 1;
    setSelectedId('');
    setContent(undefined);
    setEvents([]);
    setContentMessage('');
    setNotice('');
    // An unsuccessful refresh cannot clear a version conflict.
    void loadPage('', true);
  }

  async function reveal() {
    if (!canReveal || !selected || contentLoading) return;
    const selectedID = selected.id;
    const sequence = ++detailSequence.current;
    setContent(undefined);
    setContentMessage('');
    setContentLoading(true);
    try {
      const revealed = await apiRequest<RetainedContent>('/v1/inbound-replies/' + encodeURIComponent(selectedID) + '/content');
      if (!mounted.current || selectedRef.current !== selectedID || sequence !== detailSequence.current) return;
      setContent(revealed);
    } catch (cause) {
      if (!mounted.current || selectedRef.current !== selectedID || sequence !== detailSequence.current) return;
      if (cause instanceof APIError && cause.status === 410) setContentMessage('Reply content has been redacted under retention policy.');
      else if (isStepUp(cause)) routeStepUp();
      else setContentMessage(errorMessage(cause, 'Retained content could not be revealed.'));
    } finally {
      if (mounted.current && sequence === detailSequence.current) setContentLoading(false);
    }
  }

  function hideContent() {
    detailSequence.current += 1;
    setContent(undefined);
    setContentMessage('');
    setContentLoading(false);
  }

  function updateFromMutation(reply: InboundReply) {
    const safe = metadata(reply);
    setReplies((current) => current.map((item) => item.id === safe.id ? safe : item));
  }

  async function submitReview(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canReview || !selected || !classification || busy || conflict || mutating.current || (escalate && escalationReason.trim().length < 8)) return;
    mutating.current = true;
    setBusy('review');
    setError('');
    setNotice('');
    const replyID = selected.id;
    try {
      const updated = await apiRequest<InboundReply>('/v1/inbound-replies/' + encodeURIComponent(replyID) + '/review', {
        method: 'POST',
        body: JSON.stringify({
          expectedVersion: selected.version,
          classification,
          escalated: escalate,
          escalationReason: escalate ? escalationReason.trim() : ''
        })
      });
      if (!mounted.current || selectedRef.current !== replyID) return;
      updateFromMutation(updated);
      setNotice('Classification saved with version ' + updated.version + '. This is not confirmation of a new suppression.');
    } catch (cause) {
      if (!mounted.current || selectedRef.current !== replyID) return;
      if (cause instanceof APIError && cause.status === 409) {
        conflictedVersions.current.set(replyID, selected.version);
        setConflict(true);
      }
      if (isStepUp(cause)) routeStepUp();
      else setError(errorMessage(cause, 'The classification could not be saved.'));
    } finally {
      mutating.current = false;
      if (mounted.current) setBusy('');
    }
  }

  async function updateLegalHold(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canHold || !selected || busy || conflict || mutating.current || legalHoldReason.trim().length < 8 || (selected.legalHold && !releaseConfirmed)) return;
    mutating.current = true;
    setBusy('hold');
    setError('');
    setNotice('');
    const replyID = selected.id;
    const nextHold = !selected.legalHold;
    try {
      const updated = await apiRequest<InboundReply>('/v1/inbound-replies/' + encodeURIComponent(replyID) + '/legal-hold', {
        method: 'POST',
        body: JSON.stringify({ expectedVersion: selected.version, legalHold: nextHold, reason: legalHoldReason.trim() })
      });
      if (!mounted.current || selectedRef.current !== replyID) return;
      updateFromMutation(updated);
      setLegalHoldReason('');
      setReleaseConfirmed(false);
      setNotice(nextHold ? 'Legal hold applied and audited.' : 'Legal hold released and audited.');
    } catch (cause) {
      if (!mounted.current || selectedRef.current !== replyID) return;
      if (cause instanceof APIError && cause.status === 409) {
        conflictedVersions.current.set(replyID, selected.version);
        setConflict(true);
      }
      if (isStepUp(cause)) routeStepUp();
      else setError(errorMessage(cause, 'The legal hold could not be changed.'));
    } finally {
      mutating.current = false;
      if (mounted.current) setBusy('');
    }
  }

  async function createSuppression(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected || !canSuppress || !scopeConfirmed || suppressionReason.trim().length < 8 || busy || mutating.current) return;
    const replyID = selected.id;
    const payload = JSON.stringify({
      contactId: selected.contactId,
      scope: 'GLOBAL',
      reason: suppressionReason.trim(),
      sourceReference: 'inbound:' + selected.eventId
    });
    const prior = pendingSuppressions.current.get(selected.contactId);
    if (prior && prior.payload !== payload) {
      setError('An earlier suppression for this contact has an uncertain outcome. Do not change its scope, source or reason. Reconcile the ledger evidence or explicitly replay the original request.');
      return;
    }
    let key: string;
    try {
      key = prior?.key || requestKey();
    } catch {
      setError('This browser cannot generate a secure idempotency key. No suppression request was sent.');
      return;
    }
    pendingSuppressions.current.set(selected.contactId, { key, payload });
    mutating.current = true;
    setBusy('suppression');
    setError('');
    setNotice('');
    try {
      const outcome = await apiRequest<SuppressionResponse>(
        '/v1/suppressions', { method: 'POST', body: payload }, { idempotencyKey: key }
      );
      if (!mounted.current || selectedRef.current !== replyID) return;
      pendingSuppressions.current.delete(selected.contactId);
      setScopeConfirmed(false);
      setSuppressionReason('');
      setNotice(outcome.created ? 'Global suppression recorded. Final dispatch eligibility must enforce it.' : 'Existing identical suppression request confirmed by the ledger.');
      if (canReadConsent) void loadEvidence(selected.contactId, selected.id);
    } catch (cause) {
      if (!mounted.current || selectedRef.current !== replyID) return;
      if (isStepUp(cause)) routeStepUp();
      else if (cause instanceof APIError && (cause.status === 400 || cause.status === 422)) {
        // A definitive validation rejection is not an ambiguous network commit.
        pendingSuppressions.current.delete(selected.contactId);
        setError('The server rejected this suppression without accepting it. Correct the evidence and submit a new governed request. ' + errorMessage(cause, ''));
      } else {
        // A lost POST response is not proof that the suppression did not commit.
        // Keep the same idempotency key and payload available for an exact replay.
        setError('Suppression outcome is unconfirmed or rejected. Reconcile ledger evidence before retrying; any exact retry uses the original idempotency key. ' + errorMessage(cause, ''));
      }
    } finally {
      mutating.current = false;
      if (mounted.current) setBusy('');
    }
  }

  return (
    <div className="workspace-stack">
      {notice ? <div className="alert alert-information" role="status">{notice}</div> : null}
      {error ? <div className="alert alert-danger" role="alert">{error}</div> : null}

      <section className="card">
        <div className="section-heading">
          <div>
            <h2>Inbound reply register</h2>
            <p className="muted">Latest server-reported replies, 50 per page. Message bodies remain hidden until a separate audited reveal.</p>
          </div>
          <button type="button" onClick={refresh} disabled={Boolean(busy) || loading}>Refresh replies</button>
        </div>
        {refreshedAt ? <p className="muted">Last fetched: {humanTime(refreshedAt)}. This timestamp describes the browser refresh, not the provider event time.</p> : null}
        {loading && !loaded ? <p role="status">Loading inbound reply evidence…</p> : null}
        {loaded && replies.length === 0 ? (
          <div className="empty-state"><strong>No inbound replies on the current page</strong><p>No incoming evidence is available in the current register. This does not certify provider delivery or absence of opt-outs.</p></div>
        ) : null}
        {replies.length > 0 ? (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Event</th><th>Classification</th><th>Campaign / sender</th><th>Contact</th><th>Received</th><th>Review</th></tr></thead>
              <tbody>{replies.map((item) => (
                <tr key={item.id} className={selectedId === item.id ? 'selected-row' : undefined}>
                  <td><strong>{item.eventId}</strong><small>Reply {maskReference(item.id)}</small></td>
                  <td><StatusBadge status={item.classification} />{item.escalated ? <small>Escalated</small> : null}</td>
                  <td>{maskReference(item.campaignId)}<small>Sender {maskReference(item.sessionId)}</small></td>
                  <td>{maskReference(item.contactId)}</td>
                  <td>{humanTime(item.occurredAt)}</td>
                  <td><button type="button" disabled={Boolean(busy)} onClick={() => selectReply(item)} aria-label={'Review reply ' + item.eventId}>Review</button></td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        ) : null}
        {hasMore ? <button type="button" onClick={() => void loadPage(nextCursor)} disabled={moreLoading || loading || Boolean(busy)}>{moreLoading ? 'Loading older replies…' : 'Load older replies'}</button> : null}
        {replies.length > 0 ? <p className="muted">Showing {replies.length} loaded reply records. Filters and counts are limited to fetched pages; no completeness claim is implied.</p> : null}
      </section>

      {selected ? (
        <>
          <section className="grid grid-2">
            <article className="card form-stack">
              <h2>Selected reply evidence</h2>
              <dl className="definition-list">
                <div><dt>Event reference</dt><dd>{selected.eventId}</dd></div>
                <div><dt>Contact</dt><dd>{maskReference(selected.contactId)}</dd></div>
                <div><dt>Campaign</dt><dd>{maskReference(selected.campaignId)}</dd></div>
                <div><dt>Sender session</dt><dd>{maskReference(selected.sessionId)}</dd></div>
                <div><dt>Classification</dt><dd>{selected.classification}</dd></div>
                <div><dt>Version</dt><dd>{selected.version}</dd></div>
                <div><dt>Content retention until</dt><dd>{humanTime(selected.contentRetainUntil)}</dd></div>
                <div><dt>Legal hold</dt><dd>{selected.legalHold ? 'Active' : 'None recorded'}</dd></div>
              </dl>
              {selected.contentRedactedAt ? <p className="alert alert-information">Reply body redacted under retention policy.</p> : null}
              {canReveal ? (
                <>
                  <p className="muted">Content access is separately permissioned and audited. Do not copy recipient text into ungoverned systems.</p>
                  {content ? (
                    <>
                      <div className="card"><strong>Revealed reply content</strong><p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{content.messageText}</p></div>
                      <button type="button" onClick={hideContent}>Hide revealed content</button>
                    </>
                  ) : (
                    <button type="button" onClick={() => void reveal()} disabled={contentLoading || Boolean(busy)}>
                      {contentLoading ? 'Retrieving retained content…' : 'Reveal retained reply content (audited)'}
                    </button>
                  )}
                  {contentMessage ? <p role="status">{contentMessage}</p> : null}
                </>
              ) : <p className="muted">Reply content is restricted. A separate inbound.content.read permission is required to reveal it.</p>}
            </article>

            <form className="card form-stack" onSubmit={(event) => void submitReview(event)}>
              <h2>Classify and escalate</h2>
              <p className="muted">Classification does not itself create a suppression. Signed recognised STOP callbacks create suppression evidence independently, and ambiguous replies require an authorised safety decision.</p>
              <label htmlFor="inbound-classification">Classification</label>
              <select id="inbound-classification" value={classification} onChange={(event) => setClassification(event.target.value as ReviewClassification)} disabled={!canReview || Boolean(busy) || conflict} required>
                <option value="" disabled>Choose classification</option>
                {classifications.map((option) => <option key={option.value} value={option.value}>{option.name}</option>)}
              </select>
              <label className="checkbox-row">
                <input type="checkbox" checked={escalate} onChange={(event) => setEscalate(event.target.checked)} disabled={!canReview || Boolean(busy) || conflict} />
                <span>Escalate for review</span>
              </label>
              <label htmlFor="inbound-escalation-reason">Escalation reason</label>
              <textarea id="inbound-escalation-reason" value={escalationReason} onChange={(event) => setEscalationReason(event.target.value)} minLength={8} maxLength={1000} aria-describedby="inbound-escalation-hint" disabled={!canReview || !escalate || Boolean(busy) || conflict} />
              <small id="inbound-escalation-hint">Required for escalation, at least 8 characters. Previous private case reasons are not disclosed in the list; enter a fresh reason without pasting the recipient message.</small>
              {conflict ? <p role="alert">This reply was changed by another operator. Refresh before another mutation. A rejected write is never repeated automatically.</p> : null}
              <button className="primary" type="submit" disabled={!canReview || !classification || Boolean(busy) || conflict || (escalate && escalationReason.trim().length < 8)}>
                {busy === 'review' ? 'Saving classification…' : 'Save classification'}
              </button>
              {!canReview ? <small>Review changes require inbound.review permission.</small> : null}
            </form>
          </section>

          <section className="grid grid-2">
            <article className="card form-stack">
              <h2>Contact suppression evidence</h2>
              <p className="muted">Contact-scoped immutable events only; these are not a current send-eligibility decision. An expired or revoked suppression and older events may change the actual current state.</p>
              {!canReadConsent ? <p className="muted">Suppression evidence requires consent.read permission. Escalate to an authorised consent reviewer.</p> : null}
              {eventsLoading ? <p role="status">Loading contact-scope audit evidence…</p> : null}
              {eventsError ? <p role="alert">{eventsError}</p> : null}
              {canReadConsent && !eventsLoading && events.length === 0 && !eventsError ? <p>No suppression events on the loaded page. Do not infer that this contact is eligible for dispatch.</p> : null}
              {events.length > 0 ? (
                <div className="table-wrap"><table><thead><tr><th>Event</th><th>Suppression record</th><th>Recorded</th></tr></thead>
                  <tbody>{events.map((entry) => (
                    <tr key={entry.id}><td><strong>{entry.eventType}</strong></td><td>{maskReference(entry.suppressionId)}</td><td>{humanTime(entry.occurredAt)}</td></tr>
                  ))}</tbody>
                </table></div>
              ) : null}
              {canReadConsent ? <button type="button" onClick={() => void loadEvidence(selected.contactId, selected.id)} disabled={eventsLoading || Boolean(busy)}>Refresh contact evidence</button> : null}
            </article>

            <form className="card form-stack" onSubmit={(event) => void createSuppression(event)}>
              <h2>Independent suppression action</h2>
              <p className="muted">A reviewed OPT_OUT classification is not suppression authority. Recognised STOP events are handled at ingress; manual creation requires consent.write and creates a permanent GLOBAL rule for this exact server contact reference.</p>
              {canSuppress ? <p>Exact internal contact reference for confirmation: <code aria-label="Exact internal contact reference">{selected.contactId}</code>. This is a system ID, not the recipient MSISDN.</p> : null}
              <label htmlFor="manual-suppression-reason">Suppression reason</label>
              <textarea id="manual-suppression-reason" value={suppressionReason} onChange={(event) => setSuppressionReason(event.target.value)} minLength={8} maxLength={1000} aria-describedby="suppression-reason-hint" disabled={!canSuppress || Boolean(busy)} />
              <small id="suppression-reason-hint">At least 8 characters. Do not paste raw message content. This is a permanent global do-not-contact action.</small>
              <label className="checkbox-row">
                <input type="checkbox" checked={scopeConfirmed} onChange={(event) => setScopeConfirmed(event.target.checked)} disabled={!canSuppress || Boolean(busy)} />
                <span>I have verified the contact reference and permanent global scope</span>
              </label>
              <button className="primary" type="submit" disabled={!canSuppress || Boolean(busy) || !scopeConfirmed || suppressionReason.trim().length < 8 || !selected.contactId}>
                {busy === 'suppression' ? 'Recording suppression…' : 'Record global suppression'}
              </button>
              {!canSuppress ? <small>Contact suppression must be performed separately by an operator with consent.write permission.</small> : null}
            </form>
          </section>

          {canHold ? (
            <form className="card form-stack" onSubmit={(event) => void updateLegalHold(event)}>
              <h2>Retention legal hold</h2>
              <p className="muted">Restricted to privacy administrators. Legal hold preserves the reply body according to governed retention policy; changes are attributable and version-checked.</p>
              <label htmlFor="inbound-hold-reason">Legal hold reason</label>
              <textarea id="inbound-hold-reason" value={legalHoldReason} onChange={(event) => setLegalHoldReason(event.target.value)} minLength={8} maxLength={1000} required aria-describedby="hold-reason-hint" disabled={Boolean(busy) || conflict} />
              <small id="hold-reason-hint">At least 8 characters. Record lawful authority or approved release justification.</small>
              {selected.legalHold ? (
                <label className="checkbox-row">
                  <input type="checkbox" checked={releaseConfirmed} onChange={(event) => setReleaseConfirmed(event.target.checked)} disabled={Boolean(busy) || conflict} />
                  <span>I confirm that releasing the legal hold may allow retention cleanup to resume.</span>
                </label>
              ) : null}
              <button type="submit" disabled={Boolean(busy) || conflict || legalHoldReason.trim().length < 8 || (selected.legalHold && !releaseConfirmed)}>{selected.legalHold ? 'Release legal hold' : 'Apply legal hold'}</button>
            </form>
          ) : null}
        </>
      ) : (
        <section className="card"><div className="empty-state"><strong>No reply selected</strong><p>Select an inbound event to inspect masked evidence and, if authorised, record a reviewed classification.</p></div></section>
      )}
    </div>
  );
}
