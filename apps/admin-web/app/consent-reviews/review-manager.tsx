'use client';

import { FormEvent, useEffect, useState } from 'react';

import { StatusBadge } from '../../components/status-badge';
import { useAuth } from '../../components/auth-provider';
import { apiRequest, collectBoundedPages, type ListEnvelope, APIError } from '../../lib/api';
import { hasPermission } from '../../lib/session';
import { ConsentEvidencePanel } from './consent-evidence-panel';

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

function externalEvidenceValidationMessage(value: FormDataEntryValue | null): string | undefined {
  for (const line of lines(value)) {
    const [reference, checksum, ...extra] = line.split(/\s*\t\s*|\s*\|\s*/).map((part) => part.trim());
    if (!reference || extra.length > 0 || !checksum || !/^[0-9a-f]{64}$/i.test(checksum)) {
      return 'External evidence reference must include a 64-character SHA-256 checksum for that specific file, separated by |. Leave this optional field blank when using trusted evidence asset IDs.';
    }
  }
  return undefined;
}

const consentInputFields = new Set([
  'organisationId', 'name', 'scope', 'sourceSystem', 'campaignId', 'channel',
  'collectionPeriodFrom', 'collectionPeriodTo', 'evidenceAssetIds',
  'externalEvidenceReferences', 'sampleReviewNotes', 'revisionReason'
]);

// Only display bounded field messages explicitly returned by the consent API.
function consentFieldErrors(error: unknown): Record<string, string> {
  if (!(error instanceof APIError) || (error.status !== 422 && error.status !== 409)) return {};
  const candidate = error.details?.fields;
  if (!candidate || typeof candidate !== 'object' || Array.isArray(candidate)) return {};
  const fields: Record<string, string> = {};
  for (const [name, message] of Object.entries(candidate)) {
    if (consentInputFields.has(name) && typeof message === 'string' && message.length > 0 && message.length <= 300) {
      fields[name] = message;
    }
  }
  return fields;
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    const reference = error.requestId && /^[a-zA-Z0-9_-]{1,128}$/.test(error.requestId)
      ? ' Reference: ' + error.requestId + '.' : '';
    if (error.status === 409 && error.code === 'ORGANISATION_NOT_ACTIVE') {
      return 'The organisation must be active before a consent review can be created.' + reference;
    }
    if (error.status === 409 && error.code === 'CONSENT_EVIDENCE_NOT_CLEAN') {
      return 'Trusted consent evidence has not passed malware scanning.' + reference;
    }
    if (error.status === 409) return 'This record is not ready or has changed. Review its status before retrying.' + reference;
    if (error.status === 422) {
      return Object.keys(consentFieldErrors(error)).length
        ? 'Review not saved. Correct the highlighted field and try again.' + reference
        : 'Review not saved. Check the fields or contact support with the reference.' + reference;
    }
    if (error.status >= 500) {
      return 'The consent-review service could not complete this request. Your form entries are preserved. Reconcile the outcome before retrying.' + reference;
    }
    return error.message + reference;
  }
  return error instanceof Error ? error.message : fallback;
}

function displayDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : '—';
}

function dateInputValue(value?: string): string {
  return value ? value.slice(0, 10) : '';
}

function evidenceReferenceText(values?: ExternalEvidenceReference[]): string {
  return (values ?? []).map((value) => value.reference + ' | ' + value.sha256Checksum).join('\n');
}

function normaliseScope(value?: string): ReviewScope {
  return value === 'SOURCE' || value === 'CAMPAIGN' ? value : 'ORGANISATION';
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

export function ConsentReviewManager({ initialReviewId = '' }: { initialReviewId?: string } = {}) {
  const { session } = useAuth();
  const canRead = hasPermission(session?.permissions, 'consent.read');
  const generation = JSON.stringify([initialReviewId, session?.id, session?.sessionId, [...(session?.permissions ?? [])].sort()]);
  if (!canRead) return <p role="status">Consent review requires consent.read permission.</p>;
  return <ConsentReviewGeneration key={generation} initialReviewId={initialReviewId} />;
}

function ConsentReviewGeneration({ initialReviewId }: { initialReviewId: string }) {
  const { session } = useAuth();
  const canWrite = hasPermission(session?.permissions, 'consent.write');
  const canReview = hasPermission(session?.permissions, 'consent.review');
  const [items, setItems] = useState<ConsentReview[]>([]);
  const [selected, setSelected] = useState<ConsentReview | null>(null);
  const [revisionSource, setRevisionSource] = useState<ConsentReview | null>(null);
  const [scope, setScope] = useState<ReviewScope>('ORGANISATION');
  const [message, setMessage] = useState('');
  const [formErrors, setFormErrors] = useState<Record<string, string>>({});
  const [formFailed, setFormFailed] = useState(false);
  const fieldFeedback = (name: string) => formErrors[name]
    ? <small className="field-error" role="alert">{formErrors[name]}</small> : null;
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

  useEffect(() => {
    if (!initialReviewId) return;
    let cancelled = false;
    void apiRequest<ConsentReview>('/v1/consent-reviews/' + encodeURIComponent(initialReviewId))
      .then((value) => {
        if (cancelled) return;
        // Do not overwrite a different record the operator has opened manually.
        setSelected((current) => current ?? value);
        setRevisionSource(null);
      })
      .catch((error) => {
        if (!cancelled) setMessage(errorMessage(error, 'The linked consent review could not be opened.'));
      });
    return () => { cancelled = true; };
  }, [initialReviewId]);

  async function selectReview(item: ConsentReview) {
    setMessage('');
    setFormErrors({});
    setFormFailed(false);
    try {
      const value = await apiRequest<ConsentReview>('/v1/consent-reviews/' + encodeURIComponent(item.id));
      setSelected(value);
      setRevisionSource(null);
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
    setFormErrors({});
    setFormFailed(false);
    const form = event.currentTarget;
    const data = new FormData(form);
    const payload = buildCreatePayload(data, scope);
    const evidenceError = externalEvidenceValidationMessage(data.get('externalEvidenceReferences'));
    if (evidenceError) {
      setMessage(evidenceError);
      setFormErrors({ externalEvidenceReferences: evidenceError });
      setFormFailed(true);
      setState('error');
      return;
    }
    if (scope === 'SOURCE' && !String(payload.sourceSystem ?? '').trim()) {
      setMessage('Enter the source system for a source-scoped review.');
      setFormErrors({ sourceSystem: 'Enter the source system for a source-scoped review.' });
      setFormFailed(true);
      return;
    }
    if (scope === 'CAMPAIGN' && !String(payload.campaignId ?? '').trim()) {
      setMessage('Enter the campaign ID for a campaign-scoped review.');
      setFormErrors({ campaignId: 'Enter the campaign ID for a campaign-scoped review.' });
      setFormFailed(true);
      return;
    }
    const revisionReason = String(data.get('revisionReason') ?? '').trim();
    if (revisionSource && revisionReason.length < 8) {
      setMessage('Enter a meaningful revision reason (at least 8 characters).');
      setFormErrors({ revisionReason: 'Enter a meaningful revision reason (at least 8 characters).' });
      setFormFailed(true);
      return;
    }
    setState('saving');
    setMessage('');
    try {
      const created = revisionSource
        ? await apiRequest<ConsentReview>('/v1/consent-reviews/' + encodeURIComponent(revisionSource.id) + '/revisions', {
            method: 'POST',
            body: JSON.stringify({ expectedVersion: revisionSource.version, reason: revisionReason, review: payload })
          })
        : await apiRequest<ConsentReview>('/v1/consent-reviews', {
            method: 'POST',
            body: JSON.stringify(payload)
          });
      form.reset();
      setFormErrors({});
      setFormFailed(false);
      setScope('ORGANISATION');
      setRevisionSource(null);
      setSelected(created);
      setMessage(revisionSource
        ? 'Corrected draft created. The previous draft is preserved as superseded evidence.'
        : 'Draft consent review created. The maker must submit it before an independent checker can decide it.');
      await load();
    } catch (error) {
      setMessage(errorMessage(error, revisionSource ? 'Consent review revision could not be created' : 'Consent review could not be created'));
      setFormErrors(consentFieldErrors(error));
      setFormFailed(true);
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

  return (
    <div className="workspace-stack">
      {message ? <div className={formFailed || state === 'error' ? 'alert alert-danger' : 'alert alert-information'} role={formFailed || state === 'error' ? 'alert' : 'status'}>{message}</div> : null}

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
        <form
          key={revisionSource ? 'revision:' + revisionSource.id + ':' + revisionSource.version : 'new-review'}
          className="card form-stack"
          onSubmit={create}
        >
          <h2>{revisionSource ? 'Revise consent assessment' : 'Record consent assessment'}</h2>
          <p className="muted">{revisionSource
            ? 'Create a corrected draft while preserving the selected draft as superseded audit evidence.'
            : 'Capture the purpose, collection provenance, exact wording and evidence reviewed during client vetting.'}</p>
          <label>Organisation ID<input name="organisationId" aria-invalid={Boolean(formErrors.organisationId)} required readOnly={Boolean(revisionSource)} defaultValue={revisionSource?.organisationId ?? ''} />{fieldFeedback("organisationId")}</label>
          <label>Review name<input name="name" aria-invalid={Boolean(formErrors.name)} required maxLength={250} placeholder="e.g. Festival registration consent – August 2026" defaultValue={revisionSource?.name ?? ''} />{fieldFeedback("name")}</label>
          <label>Review scope<select name="scope" aria-invalid={Boolean(formErrors.scope)} value={scope} onChange={(event) => setScope(event.target.value as ReviewScope)}>{scopes.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select>{fieldFeedback("scope")}</label>
          {scope === 'SOURCE' ? <label>Source system<input name="sourceSystem" aria-invalid={Boolean(formErrors.sourceSystem)} required placeholder="CRM export, website form, partner database" defaultValue={revisionSource?.sourceSystem ?? ''} />{fieldFeedback("sourceSystem")}</label> : null}
          {scope === 'CAMPAIGN' ? <label>Campaign ID<input name="campaignId" aria-invalid={Boolean(formErrors.campaignId)} required defaultValue={revisionSource?.campaignId ?? ''} />{fieldFeedback("campaignId")}</label> : null}
          <label>Permitted purpose description<textarea name="purposeDescription" aria-invalid={Boolean(formErrors.purposeDescription)} rows={3} required defaultValue={revisionSource?.purposeDescription ?? ''} />{fieldFeedback("purposeDescription")}</label>
          <label>Purpose code<input name="purposeCode" aria-invalid={Boolean(formErrors.purposeCode)} required placeholder="PROMOTIONAL_MESSAGING" defaultValue={revisionSource?.purposeCode ?? ''} />{fieldFeedback("purposeCode")}</label>
          <label>Consent collection source<input name="consentSource" aria-invalid={Boolean(formErrors.consentSource)} required placeholder="Website registration form or source system" defaultValue={revisionSource?.consentSource ?? ''} />{fieldFeedback("consentSource")}</label>
          <label>Collection method<input name="collectionMethod" aria-invalid={Boolean(formErrors.collectionMethod)} required placeholder="Explicit checkbox, double opt-in, assisted capture" defaultValue={revisionSource?.collectionMethod ?? ''} />{fieldFeedback("collectionMethod")}</label>
          <div className="grid grid-2">
            <label>Collection period from<input name="collectionPeriodFrom" aria-invalid={Boolean(formErrors.collectionPeriodFrom)} type="date" defaultValue={dateInputValue(revisionSource?.collectionPeriodFrom)} />{fieldFeedback("collectionPeriodFrom")}</label>
            <label>Collection period to<input name="collectionPeriodTo" aria-invalid={Boolean(formErrors.collectionPeriodTo)} type="date" defaultValue={dateInputValue(revisionSource?.collectionPeriodTo)} />{fieldFeedback("collectionPeriodTo")}</label>
          </div>
          <label>Controller role<input name="controllerRole" aria-invalid={Boolean(formErrors.controllerRole)} required placeholder="Data controller, joint controller or processor" defaultValue={revisionSource?.controllerRole ?? ''} />{fieldFeedback("controllerRole")}</label>
          <label>Consent wording version<input name="wordingVersion" aria-invalid={Boolean(formErrors.wordingVersion)} required defaultValue={revisionSource?.wordingVersion ?? ''} />{fieldFeedback("wordingVersion")}</label>
          <label>Exact consent wording<textarea name="exactConsentWording" aria-invalid={Boolean(formErrors.exactConsentWording)} rows={3} required defaultValue={revisionSource?.exactConsentWording ?? ''} />{fieldFeedback("exactConsentWording")}</label>
          <label>Privacy notice version<input name="privacyNoticeVersion" aria-invalid={Boolean(formErrors.privacyNoticeVersion)} required defaultValue={revisionSource?.privacyNoticeVersion ?? ''} />{fieldFeedback("privacyNoticeVersion")}</label>
          <label>Trusted evidence asset IDs<small>One clean trusted-asset ID per line.</small><textarea name="evidenceAssetIds" aria-invalid={Boolean(formErrors.evidenceAssetIds)} rows={3} placeholder="UUID per line" defaultValue={(revisionSource?.evidenceAssetIds ?? []).join('\n')} />{fieldFeedback("evidenceAssetIds")}</label>
          <label>External evidence references<small>Optional. This is one line per evidence file, not per organisation. Add the 64-character SHA-256 checksum for that exact file, separated by |. Leave blank when using trusted evidence asset IDs.</small><textarea name="externalEvidenceReferences" aria-invalid={Boolean(formErrors.externalEvidenceReferences)} rows={3} placeholder="consent-policy.pdf | 64-character SHA-256 checksum" defaultValue={evidenceReferenceText(revisionSource?.externalEvidenceReferences)} />{fieldFeedback("externalEvidenceReferences")}</label>
          <fieldset>
            <legend>Permitted countries</legend>
            {countries.map(([code, label]) => (
              <label className="check" key={code}><input type="checkbox" name="permittedCountries" value={code} defaultChecked={revisionSource?.permittedCountries?.includes(code) ?? false} /> {label}</label>
            ))}
          </fieldset>
          <label>Permitted message category<input name="permittedMessageCategory" aria-invalid={Boolean(formErrors.permittedMessageCategory)} required placeholder="Event promotions" defaultValue={revisionSource?.permittedMessageCategory ?? ''} />{fieldFeedback("permittedMessageCategory")}</label>
          <fieldset>
            <legend>Mandatory review checks</legend>
            <label className="check"><input type="checkbox" name="privacyNoticeReviewed" defaultChecked={revisionSource?.privacyNoticeReviewed ?? false} /> Privacy notice reviewed</label>
            <label className="check"><input type="checkbox" name="optOutProcessReviewed" defaultChecked={revisionSource?.optOutProcessReviewed ?? false} /> Opt-out process reviewed</label>
            <label className="check"><input type="checkbox" name="sampleRecordsReviewed" defaultChecked={revisionSource?.sampleRecordsReviewed ?? false} /> Sample consent records reviewed</label>
          </fieldset>
          <label>Sample review notes<textarea name="sampleReviewNotes" aria-invalid={Boolean(formErrors.sampleReviewNotes)} rows={3} maxLength={2000} placeholder="Record what was checked without entering contact PII." defaultValue={revisionSource?.sampleReviewNotes ?? ''} />{fieldFeedback("sampleReviewNotes")}</label>
          <label>Restrictions<textarea name="restrictions" aria-invalid={Boolean(formErrors.restrictions)} rows={3} placeholder="Required for an approved-with-restrictions decision." defaultValue={revisionSource?.restrictions ?? ''} />{fieldFeedback("restrictions")}</label>
          {revisionSource ? <label>Revision reason<textarea name="revisionReason" aria-invalid={Boolean(formErrors.revisionReason)} rows={2} minLength={8} required placeholder="Explain what evidence or wording is being corrected." />{fieldFeedback("revisionReason")}</label> : null}
          <div className="button-row">
            <button className="primary" disabled={state === 'saving' || !canWrite}>{state === 'saving' ? 'Saving…' : revisionSource ? 'Create corrected draft' : 'Create draft review'}</button>
            {revisionSource ? <button type="button" className="secondary" disabled={state === 'saving'} onClick={() => { setRevisionSource(null); setScope('ORGANISATION'); setMessage('Draft revision cancelled.'); }}>Cancel revision</button> : null}
          </div>
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
                <div><dt>Checks</dt><dd>{[selected.privacyNoticeReviewed && 'privacy notice', selected.optOutProcessReviewed && 'opt-out route', selected.sampleRecordsReviewed && 'sample records'].filter(Boolean).join(', ') || 'None complete'}</dd></div>
                <div><dt>Countries / category</dt><dd>{selected.permittedCountries?.join(', ') || 'None'} · {selected.permittedMessageCategory || '—'}</dd></div>
                <div><dt>Restrictions</dt><dd>{selected.restrictions || 'None recorded'}</dd></div>
                <div><dt>Expiry</dt><dd>{displayDate(selected.expiresAt)}</dd></div>
              </dl>
              <ConsentEvidencePanel
                reviewId={selected.id}
                version={selected.version}
                assetIds={selected.evidenceAssetIds}
                externalReferences={selected.externalEvidenceReferences}
              />

              {selected.status === 'DRAFT' ? (
                <fieldset>
                  <legend>Maker draft actions</legend>
                  <p className="muted">Corrections create a new draft revision and preserve this draft as superseded evidence; the portal never overwrites consent evidence in place.</p>
                  <button
                    type="button"
                    className="secondary"
                    disabled={!canWrite || state === 'saving'}
                    onClick={() => {
                      setRevisionSource(selected);
                      setScope(normaliseScope(selected.scope));
                      setMessage('Draft loaded into the revision form. Saving will preserve this version as superseded evidence.');
                    }}
                  >
                    Revise draft evidence
                  </button>
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
