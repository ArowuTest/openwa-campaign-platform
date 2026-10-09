'use client';

import { FormEvent, useEffect, useMemo, useState } from 'react';

import { StatusBadge } from '../../components/status-badge';
import { useAuth } from '../../components/auth-provider';
import { apiRequest, collectBoundedPages, type ListEnvelope, APIError } from '../../lib/api';
import { hasPermission } from '../../lib/session';

type ReviewScope = 'ORGANISATION' | 'SOURCE' | 'CAMPAIGN';

type ExternalEvidenceReference = {
  reference: string;
  sha256Checksum: string;
};

type ConsentReview = {
  id: string;
  organisationId: string;
  scope: ReviewScope | string;
  campaignId?: string;
  sourceSystem?: string;
  name: string;
  purposeDescription: string;
  purposeCode?: string;
  channel: string;
  consentSource: string;
  collectionMethod?: string;
  collectionPeriodFrom?: string;
  collectionPeriodTo?: string;
  controllerRole?: string;
  wordingVersion: string;
  exactConsentWording?: string;
  privacyNoticeVersion?: string;
  evidenceAssetIds?: string[];
  externalEvidenceReferences?: ExternalEvidenceReference[];
  privacyNoticeReviewed: boolean;
  optOutProcessReviewed: boolean;
  sampleRecordsReviewed: boolean;
  sampleReviewNotes?: string;
  permittedCountries: string[];
  permittedMessageCategory: string;
  restrictions?: string;
  status: string;
  outcome: string;
  createdBy?: string;
  submittedBy?: string;
  reviewedBy?: string;
  reviewedAt?: string;
  expiresAt?: string;
  nextReviewAt?: string;
  createdAt: string;
  updatedAt: string;
  version: number;
};

const scopes: Array<{ value: ReviewScope; label: string }> = [
  { value: 'ORGANISATION', label: 'Organisation-wide' },
  { value: 'SOURCE', label: 'Specific collection source' },
  { value: 'CAMPAIGN', label: 'Specific campaign' }
];

const countries = [
  ['NG', 'Nigeria'],
  ['GH', 'Ghana'],
  ['GB', 'United Kingdom']
] as const;

function lines(value: FormDataEntryValue | null): string[] {
  return String(value ?? '')
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function dateOnlyAsISO(value: FormDataEntryValue | null): string | undefined {
  const date = String(value ?? '').trim();
  return date ? new Date(date + 'T00:00:00.000Z').toISOString() : undefined;
}

function dateTimeAsISO(value: FormDataEntryValue | null): string | undefined {
  const date = String(value ?? '').trim();
  return date ? new Date(date).toISOString() : undefined;
}

function evidenceReferences(value: FormDataEntryValue | null): ExternalEvidenceReference[] {
  return lines(value).map((line) => {
    const [reference, sha256Checksum] = line.split(/\s*\t\s*|\s*\|\s*/);
    return { reference: String(reference ?? '').trim(), sha256Checksum: String(sha256Checksum ?? '').trim().toLowerCase() };
  });
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError && error.status === 409) {
    return 'This consent review changed in another session. Reload it before continuing.';
  }
  return error instanceof Error ? error.message : fallback;
}

function displayDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : '—';
}

function buildCreatePayload(data: FormData, scope: ReviewScope) {
  const evidenceAssetIds = lines(data.get('evidenceAssetIds'));
  const externalEvidence = evidenceReferences(data.get('externalEvidenceReferences'));
  const payload: Record<string, unknown> = {
    organisationId: String(data.get('organisationId') ?? '').trim(),
    scope,
    name: String(data.get('name') ?? '').trim(),
    purposeDescription: String(data.get('purposeDescription') ?? '').trim(),
    purposeCode: String(data.get('purposeCode') ?? '').trim(),
    channel: 'WHATSAPP',
    consentSource: String(data.get('consentSource') ?? '').trim(),
    collectionMethod: String(data.get('collectionMethod') ?? '').trim(),
    collectionPeriodFrom: dateOnlyAsISO(data.get('collectionPeriodFrom')),
    collectionPeriodTo: dateOnlyAsISO(data.get('collectionPeriodTo')),
    controllerRole: String(data.get('controllerRole') ?? '').trim(),
    wordingVersion: String(data.get('wordingVersion') ?? '').trim(),
    exactConsentWording: String(data.get('exactConsentWording') ?? '').trim(),
    privacyNoticeVersion: String(data.get('privacyNoticeVersion') ?? '').trim(),
    evidenceAssetIds,
    externalEvidenceReferences: externalEvidence,
    privacyNoticeReviewed: data.get('privacyNoticeReviewed') === 'on',
    optOutProcessReviewed: data.get('optOutProcessReviewed') === 'on',
    sampleRecordsReviewed: data.get('sampleRecordsReviewed') === 'on',
    sampleReviewNotes: String(data.get('sampleReviewNotes') ?? '').trim(),
    permittedCountries: data.getAll('permittedCountries').map(String),
    permittedMessageCategory: String(data.get('permittedMessageCategory') ?? '').trim(),
    restrictions: String(data.get('restrictions') ?? '').trim()
  };
  if (scope === 'SOURCE') payload.sourceSystem = String(data.get('sourceSystem') ?? '').trim();
  if (scope === 'CAMPAIGN') payload.campaignId = String(data.get('campaignId') ?? '').trim();
  return payload;
}

export function ConsentReviewManager() {
  const { session } = useAuth();
  const canWrite = hasPermission(session?.permissions, 'consent.write');
  const canReview = hasPermission(session?.permissions, 'consent.review');
  const [items, setItems] = useState<ConsentReview[]>([]);
  const [selected, setSelected] = useState<ConsentReview | null>(null);
  const [scope, setScope] = useState<ReviewScope>('ORGANISATION');
  const [message, setMessage] = useState('');
  const [state, setState] = useState<'loading' | 'idle' | 'saving' | 'error'>('loading');
  const [submitReason, setSubmitReason] = useState('');
  const [decision, setDecision] = useState<'APPROVED' | 'REJECTED'>('APPROVED');
  const [approvedWithRestrictions, setApprovedWithRestrictions] = useState(false);
  const [expiresAt, setExpiresAt] = useState('');
  const [nextReviewAt, setNextReviewAt] = useState('');
  const [decisionReason, setDecisionReason] = useState('');
  const [revokeReason, setRevokeReason] = useState('');

  async function load() {
    setState('loading');
    try {
      const values = await collectBoundedPages<ConsentReview>(
        (cursor) => apiRequest<ListEnvelope<ConsentReview>>(
          '/v1/consent-reviews?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
        ),
        20
      );
      setItems(values);
      setState('idle');
    } catch (error) {
      setMessage(errorMessage(error, 'Unable to load consent reviews'));
      setState('error');
    }
  }

  useEffect(() => {
    let cancelled = false;
    void collectBoundedPages<ConsentReview>(
      (cursor) => apiRequest<ListEnvelope<ConsentReview>>(
        '/v1/consent-reviews?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
      ),
      20
    ).then((values) => {
      if (cancelled) return;
      setItems(values);
      setState('idle');
    }).catch((error) => {
      if (cancelled) return;
      setMessage(errorMessage(error, 'Unable to load consent reviews'));
      setState('error');
    });
    return () => { cancelled = true; };
  }, []);

  async function selectReview(item: ConsentReview) {
    setMessage('');
    try {
      const value = await apiRequest<ConsentReview>('/v1/consent-reviews/' + encodeURIComponent(item.id));
      setSelected(value);
      setDecision('APPROVED');
      setApprovedWithRestrictions(false);
      setExpiresAt('');
      setNextReviewAt('');
      setSubmitReason('');
      setDecisionReason('');
      setRevokeReason('');
    } catch (error) {
      setMessage(errorMessage(error, 'Unable to load consent review detail'));
    }
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canWrite || state === 'saving') return;
    const form = event.currentTarget;
    const data = new FormData(form);
    const payload = buildCreatePayload(data, scope);
    if (scope === 'SOURCE' && !String(payload.sourceSystem ?? '').trim()) {
      setMessage('Enter the source system for a source-scoped review.');
      return;
    }
    if (scope === 'CAMPAIGN' && !String(payload.campaignId ?? '').trim()) {
      setMessage('Enter the campaign ID for a campaign-scoped review.');
      return;
    }
    setState('saving');
    setMessage('');
    try {
      const created = await apiRequest<ConsentReview>('/v1/consent-reviews', {
        method: 'POST',
        body: JSON.stringify(payload)
      });
      form.reset();
      setScope('ORGANISATION');
      setSelected(created);
      setMessage('Draft consent review created. The maker must submit it before an independent checker can decide it.');
      await load();
    } catch (error) {
      setMessage(errorMessage(error, 'Consent review could not be created'));
      setState('error');
    } finally {
      setState('idle');
    }
  }

  async function submitReview() {
    if (!selected || !canWrite || selected.status !== 'DRAFT') return;
    const reason = submitReason.trim();
    if (reason.length < 5) {
      setMessage('Enter a meaningful submission reason (at least 5 characters).');
      return;
    }
    const evidenceAssetIds = selected.evidenceAssetIds ?? [];
    const externalEvidenceReferences = selected.externalEvidenceReferences ?? [];
    if (evidenceAssetIds.length === 0 && externalEvidenceReferences.length === 0) {
      setMessage('Add at least one trusted evidence asset ID or checksummed external evidence reference before submitting.');
      return;
    }
    if ((selected.permittedCountries ?? []).length === 0) {
      setMessage('Select at least one permitted country before submitting.');
      return;
    }
    if (!selected.privacyNoticeReviewed || !selected.optOutProcessReviewed || !selected.sampleRecordsReviewed) {
      setMessage('Complete all mandatory review checks before submitting.');
      return;
    }
    if (selected.sampleRecordsReviewed && !selected.sampleReviewNotes?.trim()) {
      setMessage('Add minimised sample review notes before submitting.');
      return;
    }
    setState('saving');
    setMessage('');
    try {
      const value = await apiRequest<ConsentReview>('/v1/consent-reviews/' + encodeURIComponent(selected.id) + '/submit', {
        method: 'POST',
        body: JSON.stringify({ expectedVersion: selected.version, reason })
      });
      setSelected(value);
      setSubmitReason('');
      setMessage('Review submitted. An independent checker must make the decision.');
      await load();
    } catch (error) {
      setMessage(errorMessage(error, 'Consent review could not be submitted'));
    } finally {
      setState('idle');
    }
  }

  async function decideReview() {
    if (!selected || !canReview || selected.status !== 'PENDING_REVIEW') return;
    const reason = decisionReason.trim();
    if (reason.length < 5) {
      setMessage('Enter a meaningful decision reason (at least 5 characters).');
      return;
    }
    const expiry = decision === 'APPROVED' ? dateTimeAsISO(expiresAt) : undefined;
    if (decision === 'APPROVED' && !expiry) {
      setMessage('An approval requires a future expiry date.');
      return;
    }
    if (decision === 'APPROVED' && approvedWithRestrictions && !selected.restrictions?.trim()) {
      setMessage('Approved with restrictions requires restrictions recorded on the review.');
      return;
    }
    setState('saving');
    setMessage('');
    try {
      const value = await apiRequest<ConsentReview>('/v1/consent-reviews/' + encodeURIComponent(selected.id) + '/decision', {
        method: 'POST',
        body: JSON.stringify({
          decision,
          approvedWithRestrictions: decision === 'APPROVED' && approvedWithRestrictions,
          expiresAt: expiry,
          nextReviewAt: dateTimeAsISO(nextReviewAt),
          reason,
          expectedVersion: selected.version
        })
      });
      setSelected(value);
      setDecisionReason('');
      setExpiresAt('');
      setNextReviewAt('');
      setMessage('Decision recorded with the server-side maker/checker and version checks.');
      await load();
    } catch (error) {
      setMessage(errorMessage(error, 'Consent review decision could not be recorded'));
    } finally {
      setState('idle');
    }
  }

  async function revokeReview() {
    if (!selected || !canReview || selected.status !== 'APPROVED') return;
    const reason = revokeReason.trim();
    if (reason.length < 8) {
      setMessage('Enter a revocation reason (at least 8 characters).');
      return;
    }
    setState('saving');
    setMessage('');
    try {
      const value = await apiRequest<ConsentReview>('/v1/consent-reviews/' + encodeURIComponent(selected.id) + '/revoke', {
        method: 'POST',
        body: JSON.stringify({ expectedVersion: selected.version, reason })
      });
      setSelected(value);
      setRevokeReason('');
      setMessage('Approved consent review revoked.');
      await load();
    } catch (error) {
      setMessage(errorMessage(error, 'Consent review could not be revoked'));
    } finally {
      setState('idle');
    }
  }

  const selectedEvidence = useMemo(() => {
    if (!selected) return [];
    return [
      ...(selected.evidenceAssetIds ?? []).map((id) => 'Trusted asset: ' + id),
      ...(selected.externalEvidenceReferences ?? []).map((value) => value.reference + ' · SHA-256 ' + value.sha256Checksum)
    ];
  }, [selected]);

  return (
    <div className="workspace-stack">
      {message ? <div className={state === 'error' ? 'alert alert-danger' : 'alert alert-information'} role="status">{message}</div> : null}

      <section className="card">
        <div className="section-heading">
          <div>
            <h2>Consent review register</h2>
            <p className="muted">Internal offline-vetted consent evidence. Creating a draft never approves consent or authorises a campaign.</p>
          </div>
          <span className="pill">{items.length} reviews</span>
        </div>
        {state === 'loading' ? <p className="muted">Loading consent reviews…</p> : null}
        {state !== 'loading' && items.length === 0 ? (
          <div className="empty-state"><strong>No consent reviews</strong><p>Record the first offline consent assessment below.</p></div>
        ) : null}
        {items.length > 0 ? (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Review</th><th>Scope</th><th>Purpose</th><th>Status</th><th>Expiry</th><th>Version</th></tr></thead>
              <tbody>
                {items.map((item) => (
                  <tr key={item.id} className={selected?.id === item.id ? 'selected-row' : undefined}>
                    <td><button type="button" className="table-link" onClick={() => void selectReview(item)}><strong>{item.name}</strong></button><small>{item.organisationId}</small></td>
                    <td>{item.scope || 'ORGANISATION'}<small>{item.sourceSystem || item.campaignId || ''}</small></td>
                    <td>{item.purposeDescription}<small>{item.permittedMessageCategory || ''}</small></td>
                    <td><StatusBadge status={item.status} /></td>
                    <td>{displayDate(item.expiresAt)}</td>
                    <td>{item.version}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>

      <section className="grid grid-2">
        <form className="card form-stack" onSubmit={create}>
          <h2>Record consent assessment</h2>
          <p className="muted">Capture the purpose, collection provenance, exact wording and evidence reviewed during client vetting.</p>
          <label>Organisation ID<input name="organisationId" required /></label>
          <label>Review name<input name="name" required maxLength={250} placeholder="e.g. Festival registration consent – August 2026" /></label>
          <label>Review scope<select name="scope" value={scope} onChange={(event) => setScope(event.target.value as ReviewScope)}>{scopes.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label>
          {scope === 'SOURCE' ? <label>Source system<input name="sourceSystem" required placeholder="CRM export, website form, partner database" /></label> : null}
          {scope === 'CAMPAIGN' ? <label>Campaign ID<input name="campaignId" required /></label> : null}
          <label>Permitted purpose description<textarea name="purposeDescription" rows={3} required /></label>
          <label>Purpose code<input name="purposeCode" required placeholder="PROMOTIONAL_MESSAGING" /></label>
          <label>Consent collection source<input name="consentSource" required placeholder="Website registration form or source system" /></label>
          <label>Collection method<input name="collectionMethod" required placeholder="Explicit checkbox, double opt-in, assisted capture" /></label>
          <div className="grid grid-2">
            <label>Collection period from<input name="collectionPeriodFrom" type="date" /></label>
            <label>Collection period to<input name="collectionPeriodTo" type="date" /></label>
          </div>
          <label>Controller role<input name="controllerRole" required placeholder="Data controller, joint controller or processor" /></label>
          <label>Consent wording version<input name="wordingVersion" required /></label>
          <label>Exact consent wording<textarea name="exactConsentWording" rows={3} required /></label>
          <label>Privacy notice version<input name="privacyNoticeVersion" required /></label>
          <label>Trusted evidence asset IDs<small>One clean trusted-asset ID per line.</small><textarea name="evidenceAssetIds" rows={3} placeholder="UUID per line" /></label>
          <label>External evidence references<small>One reference and lowercase SHA-256 checksum per line, separated by a tab or |.</small><textarea name="externalEvidenceReferences" rows={3} placeholder="case-2026-08-14 | 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" /></label>
          <fieldset>
            <legend>Permitted countries</legend>
            {countries.map(([code, label]) => (
              <label className="check" key={code}><input type="checkbox" name="permittedCountries" value={code} /> {label}</label>
            ))}
          </fieldset>
          <label>Permitted message category<input name="permittedMessageCategory" required placeholder="Event promotions" /></label>
          <fieldset>
            <legend>Mandatory review checks</legend>
            <label className="check"><input type="checkbox" name="privacyNoticeReviewed" /> Privacy notice reviewed</label>
            <label className="check"><input type="checkbox" name="optOutProcessReviewed" /> Opt-out process reviewed</label>
            <label className="check"><input type="checkbox" name="sampleRecordsReviewed" /> Sample consent records reviewed</label>
          </fieldset>
          <label>Sample review notes<textarea name="sampleReviewNotes" rows={3} maxLength={2000} placeholder="Record what was checked without entering contact PII." /></label>
          <label>Restrictions<textarea name="restrictions" rows={3} placeholder="Required for an approved-with-restrictions decision." /></label>
          <button className="primary" disabled={state === 'saving' || !canWrite}>{state === 'saving' ? 'Saving…' : 'Create draft review'}</button>
        </form>

        <article className="card form-stack">
          <div className="section-heading">
            <div><h2>Maker / checker workspace</h2><p className="muted">{selected ? selected.name + ' · version ' + selected.version : 'Select a review from the register to inspect or progress it.'}</p></div>
            {selected ? <StatusBadge status={selected.status} /> : null}
          </div>
          {!selected ? <div className="empty-state"><strong>No review selected</strong><p>The maker submits a complete draft; an independent checker records the decision.</p></div> : (
            <>
              <dl className="detail-list">
                <div><dt>Organisation</dt><dd>{selected.organisationId}</dd></div>
                <div><dt>Scope</dt><dd>{selected.scope}{selected.sourceSystem ? ' · ' + selected.sourceSystem : ''}{selected.campaignId ? ' · ' + selected.campaignId : ''}</dd></div>
                <div><dt>Purpose</dt><dd>{selected.purposeDescription} ({selected.purposeCode || 'no code'})</dd></div>
                <div><dt>Collection</dt><dd>{selected.collectionMethod || '—'} via {selected.consentSource || '—'}</dd></div>
                <div><dt>Controller / wording</dt><dd>{selected.controllerRole || '—'} · {selected.wordingVersion}</dd></div>
                <div><dt>Evidence</dt><dd>{selectedEvidence.length ? <ul>{selectedEvidence.map((value) => <li key={value}>{value}</li>)}</ul> : 'No evidence recorded'}</dd></div>
                <div><dt>Checks</dt><dd>{[selected.privacyNoticeReviewed && 'privacy notice', selected.optOutProcessReviewed && 'opt-out route', selected.sampleRecordsReviewed && 'sample records'].filter(Boolean).join(', ') || 'None complete'}</dd></div>
                <div><dt>Countries / category</dt><dd>{selected.permittedCountries?.join(', ') || 'None'} · {selected.permittedMessageCategory || '—'}</dd></div>
                <div><dt>Restrictions</dt><dd>{selected.restrictions || 'None recorded'}</dd></div>
                <div><dt>Expiry</dt><dd>{displayDate(selected.expiresAt)}</dd></div>
              </dl>

              {selected.status === 'DRAFT' ? (
                <fieldset>
                  <legend>Maker submission</legend>
                  <label>Submission reason<textarea rows={2} value={submitReason} onChange={(event) => setSubmitReason(event.target.value)} disabled={!canWrite} /></label>
                  <button type="button" className="primary" disabled={!canWrite || state === 'saving' || submitReason.trim().length < 5} onClick={() => void submitReview()}>Submit for checker review</button>
                </fieldset>
              ) : null}

              {selected.status === 'PENDING_REVIEW' ? (
                <fieldset>
                  <legend>Independent checker decision</legend>
                  <p className="muted">The server rejects a decision by the creator or submitting maker, and requires the current review version.</p>
                  <label>Decision<select value={decision} onChange={(event) => setDecision(event.target.value as 'APPROVED' | 'REJECTED')} disabled={!canReview}><option value="APPROVED">Approve</option><option value="REJECTED">Reject</option></select></label>
                  {decision === 'APPROVED' ? <label>Approval expiry (operator local time)<input type="datetime-local" value={expiresAt} onChange={(event) => setExpiresAt(event.target.value)} disabled={!canReview} required /></label> : null}
                  {decision === 'APPROVED' ? <label className="check"><input type="checkbox" checked={approvedWithRestrictions} onChange={(event) => setApprovedWithRestrictions(event.target.checked)} disabled={!canReview} /> Approve with recorded restrictions</label> : null}
                  {decision === 'APPROVED' ? <label>Next review at (optional)<input type="datetime-local" value={nextReviewAt} onChange={(event) => setNextReviewAt(event.target.value)} disabled={!canReview} /></label> : null}
                  <label>Decision reason<textarea rows={3} value={decisionReason} onChange={(event) => setDecisionReason(event.target.value)} disabled={!canReview} /></label>
                  <button type="button" className="primary" disabled={!canReview || state === 'saving' || decisionReason.trim().length < 5 || (decision === 'APPROVED' && !expiresAt)} onClick={() => void decideReview()}>{decision === 'APPROVED' ? 'Record approval' : 'Record rejection'}</button>
                </fieldset>
              ) : null}

              {selected.status === 'APPROVED' ? (
                <fieldset>
                  <legend>Revoke approved review</legend>
                  <label>Revocation reason<textarea rows={2} value={revokeReason} onChange={(event) => setRevokeReason(event.target.value)} disabled={!canReview} /></label>
                  <button type="button" className="secondary" disabled={!canReview || state === 'saving' || revokeReason.trim().length < 8} onClick={() => void revokeReview()}>Revoke approval</button>
                </fieldset>
              ) : null}
            </>
          )}
        </article>
      </section>
    </div>
  );
}
