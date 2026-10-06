'use client';

import { ChangeEvent, FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { StatusBadge } from '../../../components/status-badge';
import { APIError, apiRequest, browserAPIBase, collectBoundedPages, ListEnvelope } from '../../../lib/api';
import {
  AudienceImportColumnMapping,
  audienceImportResumeStorageKey,
  buildUploadSessionRequest,
  choosePreferredTransport,
  csvHeaderProbeBytes,
  csvHeadersFromPrefix,
  fileMatchesResume,
  nextImportStage,
  sanitizeResumeRecord,
  UploadResumeRecord,
  uploadProgress
} from '../../../lib/audience-import-upload-model';

type Organisation = {
  id: string;
  legalName: string;
  tradingName?: string;
  status: string;
};

type ConsentReview = {
  id: string;
  organisationId: string;
  name: string;
  status: string;
  outcome: string;
  wordingVersion: string;
  expiresAt?: string;
  restrictions?: string;
};

type ConsentPurpose = {
  id: string;
  organisationId?: string;
  consentReviewId?: string;
  code: string;
  name: string;
  description?: string;
  channel: string;
  wordingVersion: string;
  active: boolean;
};

type UploadPart = {
  number: number;
  offset: number;
  expectedBytes: number;
  state: 'PENDING' | 'UPLOADED';
  sha256?: string;
  uploadedBytes?: number;
};

type UploadSession = {
  id: string;
  organisationId: string;
  consentReviewId: string;
  purposeId: string;
  sourceName: string;
  sourceSystem?: string;
  originalFilename: string;
  expectedBytes: number;
  uploadedBytes: number;
  partSize: number;
  partCount: number;
  uploadedParts: number;
  state: 'CREATED' | 'UPLOADING' | 'UPLOADED' | 'FINALISING' | 'IMPORT_CREATED' | 'ABORTED' | 'EXPIRED' | 'FAILED';
  version: number;
  failureReason?: string;
  linkedImportId?: string;
  expiresAt: string;
  parts: UploadPart[];
};

type UploadEnvelope = {
  transport: 'DIRECT_S3' | 'RELAY';
  transports?: Array<'DIRECT_S3' | 'RELAY'>;
  preferredTransport?: 'DIRECT_S3' | 'RELAY';
  session: UploadSession;
};

type UploadPartEnvelope = {
  changed: boolean;
  session: UploadSession;
};

type DirectUploadTargetEnvelope = {
  partNumber: number;
  alreadyUploaded: boolean;
  target?: {
    method: 'PUT';
    url: string;
    headers: Record<string, string>;
    expectedBytes: number;
    expiresAt: string;
  };
};

type AudienceImport = {
  id: string;
  organisationId: string;
  consentReviewId: string;
  purposeId: string;
  sourceName: string;
  sourceSystem?: string;
  originalFilename: string;
  detectedMediaType: string;
  byteSize: number;
  status: string;
  malwareStatus: string;
  contentSignatureValid: boolean;
  uploadedRows: number;
  validRows: number;
  invalidRows: number;
  duplicateRows: number;
  suppressedRows: number;
  insertedContacts: number;
  updatedContacts: number;
  approvedBy?: string;
  failureReason?: string;
  version: number;
  updatedAt: string;
};

type ImportListItem = {
  id: string;
  organisationId: string;
  consentReviewId: string;
  purposeId: string;
  sourceName: string;
  sourceSystem?: string;
  originalFilename: string;
  byteSize: number;
  status: string;
  malwareStatus: string;
  uploadedRows: number;
  validRows: number;
  invalidRows: number;
  duplicateRows: number;
  suppressedRows: number;
  insertedContacts: number;
  updatedContacts: number;
  failureReason?: string;
  createdAt: string;
  updatedAt: string;
};

type ConflictSummary = { pending: number; resolved: number; rejected: number; total: number };

type Reconciliation = {
  importId: string;
  status: string;
  uploadedRows: number;
  validRows: number;
  invalidRows: number;
  duplicateRows: number;
  suppressedRows: number;
  insertedContacts: number;
  updatedContacts: number;
  conflicts: ConflictSummary;
  validationAccounted: number;
  validationBalanced: boolean;
  mergeAccounted: number;
  mergeWithinValidRows: boolean;
  readyForClosure: boolean;
  warnings: string[];
  calculatedAt: string;
};

type ProfileConflict = {
  id: string;
  maskedMsisdn: string;
  field: string;
  existingValue?: string;
  incomingValue?: string;
  status: string;
  version: number;
};

const emptyMapping: AudienceImportColumnMapping = {
  worksheet: '',
  msisdn: 'msisdn',
  country: 'country',
  state: 'state',
  lga: 'lga',
  age: 'age',
  gender: 'gender'
};

function idempotencyKey(): string {
  const suffix = typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : Math.random().toString(36).slice(2) + Date.now().toString(36);
  return 'audience-upload:' + suffix;
}

async function sha256Hex(blob: Blob): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', await blob.arrayBuffer());
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('');
}

async function boundedFileFingerprint(file: File): Promise<string> {
  const probeBytes = Math.min(csvHeaderProbeBytes, file.size);
  const first = file.slice(0, probeBytes);
  const last = file.slice(Math.max(0, file.size - probeBytes), file.size);
  const metadata = new TextEncoder().encode(
    [file.name, String(file.size), String(file.lastModified)].join('\u0000') + '\u0000'
  );
  return sha256Hex(new Blob([metadata, first, last]));
}

function isApprovedReview(review: ConsentReview): boolean {
  if (review.status !== 'APPROVED') return false;
  if (!review.expiresAt) return true;
  return new Date(review.expiresAt).getTime() > Date.now();
}

function suggestedMapping(headers: string[]): AudienceImportColumnMapping {
  const lookup = new Map(headers.map((header) => [header.trim().toLowerCase(), header]));
  const choose = (...names: string[]) => {
    for (const name of names) {
      const value = lookup.get(name);
      if (value) return value;
    }
    return '';
  };
  return {
    worksheet: '',
    msisdn: choose('msisdn', 'mobile', 'mobile_number', 'phone', 'phone_number', 'telephone'),
    country: choose('country', 'country_code', 'country_iso2'),
    state: choose('state', 'region'),
    lga: choose('lga', 'local_government', 'local_government_area'),
    age: choose('age'),
    gender: choose('gender', 'sex')
  };
}

function needsImportPolling(status: string | undefined): boolean {
  return ['UPLOADED', 'SCANNING', 'VALIDATING', 'APPROVED', 'IMPORTING'].includes(String(status ?? '').toUpperCase());
}

function requiresStepUp(error: unknown): boolean {
  return error instanceof APIError && (error.code === 'STEP_UP_REQUIRED' || error.code === 'MFA_STEP_UP_REQUIRED');
}

function goToStepUp() {
  window.location.assign('/step-up?returnTo=' + encodeURIComponent('/audiences/imports'));
}

export function AudienceImportWorkspace() {
  const [organisations, setOrganisations] = useState<Organisation[]>([]);
  const [reviews, setReviews] = useState<ConsentReview[]>([]);
  const [purposes, setPurposes] = useState<ConsentPurpose[]>([]);
  const [organisationId, setOrganisationId] = useState('');
  const [consentReviewId, setConsentReviewId] = useState('');
  const [purposeId, setPurposeId] = useState('');
  const [sourceName, setSourceName] = useState('');
  const [sourceSystem, setSourceSystem] = useState('');
  const [defaultCountry, setDefaultCountry] = useState('NG');
  const [templateVersion, setTemplateVersion] = useState('v1');
  const [updatePolicy, setUpdatePolicy] = useState<'INSERT_ONLY' | 'FILL_NULL' | 'NEWEST_SOURCE' | 'TRUSTED_SOURCE' | 'MANUAL_CONFLICT'>('NEWEST_SOURCE');
  const [mapping, setMapping] = useState<AudienceImportColumnMapping>(emptyMapping);
  const [headers, setHeaders] = useState<string[]>([]);
  const [file, setFile] = useState<File | null>(null);
  const [fileFingerprint, setFileFingerprint] = useState('');
  const [sessionEnvelope, setSessionEnvelope] = useState<UploadEnvelope | null>(null);
  const [resumeRecord, setResumeRecord] = useState<UploadResumeRecord | null>(null);
  const [importBatch, setImportBatch] = useState<AudienceImport | null>(null);
  const [recentImports, setRecentImports] = useState<ImportListItem[]>([]);
  const [recentImportsCursor, setRecentImportsCursor] = useState('');
  const [recentImportsHasMore, setRecentImportsHasMore] = useState(false);
  const [recentImportsLoading, setRecentImportsLoading] = useState(false);
  const [reconciliation, setReconciliation] = useState<Reconciliation | null>(null);
  const [conflicts, setConflicts] = useState<ProfileConflict[]>([]);
  const [conflictReason, setConflictReason] = useState('Reviewed against source evidence');
  const [reconciliationReason, setReconciliationReason] = useState('Import totals and conflict decisions reviewed');
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const abortRef = useRef<AbortController | null>(null);

  const session = sessionEnvelope?.session ?? null;
  const progress = session ? uploadProgress(session) : null;
  const selectedReview = reviews.find((item) => item.id === consentReviewId);
  const selectedPurpose = purposes.find((item) => item.id === purposeId);
  const fileExtension = file?.name.toLowerCase().endsWith('.xlsx') ? 'xlsx' : file?.name.toLowerCase().endsWith('.csv') ? 'csv' : '';

  const journeyStep = useMemo(() => {
    if (importBatch) {
      if (importBatch.status === 'PREVIEW_READY') return 4;
      if (['APPROVED', 'IMPORTING'].includes(importBatch.status)) return 5;
      if (['COMPLETED', 'COMPLETED_WITH_EXCEPTIONS'].includes(importBatch.status)) return 6;
      if (['FAILED', 'REJECTED', 'CANCELLED', 'ROLLED_BACK'].includes(importBatch.status)) return 6;
      return 4;
    }
    if (session) {
      if (session.state === 'UPLOADED' || session.state === 'FINALISING' || session.state === 'IMPORT_CREATED') return 3;
      return 2;
    }
    if (file) return 1;
    return 0;
  }, [file, importBatch, session]);

  function sessionEnvelopeFrom(sessionValue: UploadSession): UploadEnvelope {
    const transports = sessionEnvelope?.transports ?? ['RELAY'];
    const preferred = sessionEnvelope?.preferredTransport ?? choosePreferredTransport(transports);
    return { transport: preferred, preferredTransport: preferred, transports, session: sessionValue };
  }

  function persistResume(sessionValue: UploadSession, requestKey: string, selectedFile: File, fingerprint: string) {
    const record = sanitizeResumeRecord({
      sessionId: sessionValue.id,
      requestKey,
      fileName: selectedFile.name,
      fileSize: selectedFile.size,
      fileLastModified: selectedFile.lastModified,
      fileFingerprint: fingerprint,
      organisationId,
      consentReviewId,
      purposeId,
      sourceName,
      sourceSystem
    });
    localStorage.setItem(audienceImportResumeStorageKey, JSON.stringify(record));
    setResumeRecord(record);
  }

  const clearResume = useCallback(() => {
    localStorage.removeItem(audienceImportResumeStorageKey);
    setResumeRecord(null);
  }, []);

  const loadReconciliation = useCallback(async (importId: string) => {
    try {
      const value = await apiRequest<Reconciliation>('/v1/audience-imports/' + importId + '/reconciliation');
      setReconciliation(value);
      if (value.conflicts.pending > 0) {
        const page = await apiRequest<ListEnvelope<ProfileConflict>>(
          '/v1/audience-imports/' + importId + '/conflicts?status=PENDING&limit=100'
        );
        setConflicts(page.items ?? []);
      } else {
        setConflicts([]);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to load reconciliation evidence');
    }
  }, []);

  const loadImport = useCallback(async (importId: string) => {
    try {
      const value = await apiRequest<AudienceImport>('/v1/audience-imports/' + importId);
      setImportBatch(value);
      if (['COMPLETED', 'COMPLETED_WITH_EXCEPTIONS'].includes(value.status)) {
        await loadReconciliation(value.id);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to load import state');
    }
  }, [loadReconciliation]);

  const loadRecentImports = useCallback(async (orgId: string, cursor = '', append = false) => {
    if (!orgId) return;
    setRecentImportsLoading(true);
    try {
      const page = await apiRequest<ListEnvelope<ImportListItem>>(
        '/v1/audience-imports?organisationId=' + encodeURIComponent(orgId) +
        '&limit=20' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
      );
      setRecentImports((current) => append ? [...current, ...page.items] : page.items);
      setRecentImportsCursor(page.nextCursor ?? '');
      setRecentImportsHasMore(page.hasMore);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to load recent audience imports');
    } finally {
      setRecentImportsLoading(false);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    void collectBoundedPages<Organisation>(
      (cursor) => apiRequest<ListEnvelope<Organisation>>('/v1/organisations?limit=500' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')),
      20
    ).then((items) => {
      if (!cancelled) setOrganisations(items.filter((item) => item.status === 'ACTIVE'));
    }).catch((cause) => {
      if (!cancelled) setError(cause instanceof Error ? cause.message : 'Unable to load organisations');
    });

    const restore = async () => {
      await Promise.resolve();
      if (cancelled) return;
      const raw = localStorage.getItem(audienceImportResumeStorageKey);
      if (!raw) return;
      try {
        const record = sanitizeResumeRecord(JSON.parse(raw));
        if (cancelled) return;
        setResumeRecord(record);
        setOrganisationId(record.organisationId);
        setConsentReviewId(record.consentReviewId);
        setPurposeId(record.purposeId);
        setSourceName(record.sourceName);
        setSourceSystem(record.sourceSystem);
        const value = await apiRequest<UploadEnvelope>('/v1/audience-import-upload-sessions/' + record.sessionId);
        if (cancelled) return;
        setSessionEnvelope(value);
        if (value.session.state === 'IMPORT_CREATED' && value.session.linkedImportId) {
          clearResume();
          await loadImport(value.session.linkedImportId);
        }
      } catch {
        if (!cancelled) clearResume();
      }
    };
    void restore();
    return () => { cancelled = true; };
  }, [clearResume, loadImport]);

  useEffect(() => {
    if (!organisationId) return;
    let cancelled = false;
    void collectBoundedPages<ConsentReview>(
      (cursor) => apiRequest<ListEnvelope<ConsentReview>>(
        '/v1/consent-reviews?organisationId=' + encodeURIComponent(organisationId) + '&limit=500' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
      ),
      20
    ).then((items) => {
      if (!cancelled) setReviews(items.filter(isApprovedReview));
    }).catch((cause) => {
      if (!cancelled) setError(cause instanceof Error ? cause.message : 'Unable to load consent reviews');
    });
    return () => { cancelled = true; };
  }, [organisationId]);

  useEffect(() => {
    if (!organisationId) return;
    const timer = window.setTimeout(() => { void loadRecentImports(organisationId); }, 0);
    return () => window.clearTimeout(timer);
  }, [loadRecentImports, organisationId]);

  useEffect(() => {
    if (!organisationId || !consentReviewId) return;
    let cancelled = false;
    void collectBoundedPages<ConsentPurpose>(
      (cursor) => apiRequest<ListEnvelope<ConsentPurpose>>(
        '/v1/consent-purposes?organisationId=' + encodeURIComponent(organisationId) +
        '&consentReviewId=' + encodeURIComponent(consentReviewId) + '&limit=500' +
        (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
      ),
      20
    ).then((items) => {
      if (!cancelled) setPurposes(items.filter((item) => item.active && item.channel === 'WHATSAPP'));
    }).catch((cause) => {
      if (!cancelled) setError(cause instanceof Error ? cause.message : 'Unable to load consent purposes');
    });
    return () => { cancelled = true; };
  }, [organisationId, consentReviewId]);

  const sessionId = session?.id;
  const sessionState = session?.state;
  useEffect(() => {
    if (!sessionId || !sessionState || !['UPLOADED', 'FINALISING'].includes(sessionState)) return;
    const timer = window.setInterval(() => {
      void apiRequest<UploadEnvelope>('/v1/audience-import-upload-sessions/' + sessionId)
        .then((value) => {
          setSessionEnvelope(value);
          if (value.session.state === 'IMPORT_CREATED' && value.session.linkedImportId) {
            clearResume();
            void loadImport(value.session.linkedImportId);
          }
        })
        .catch((cause) => setError(cause instanceof Error ? cause.message : 'Unable to refresh upload state'));
    }, 3000);
    return () => window.clearInterval(timer);
  }, [clearResume, loadImport, sessionId, sessionState]);

  const importId = importBatch?.id;
  const importStatus = importBatch?.status;
  useEffect(() => {
    if (!importId || !needsImportPolling(importStatus)) return;
    const timer = window.setInterval(() => { void loadImport(importId); }, 3000);
    return () => window.clearInterval(timer);
  }, [importId, importStatus, loadImport]);


  async function chooseFile(event: ChangeEvent<HTMLInputElement>) {
    const selected = event.target.files?.[0] ?? null;
    setError('');
    setNotice('');
    setHeaders([]);
    if (!selected) {
      setFile(null);
      setFileFingerprint('');
      return;
    }
    if (!selected.name.toLowerCase().endsWith('.csv') && !selected.name.toLowerCase().endsWith('.xlsx')) {
      setError('Select a CSV or XLSX source file.');
      event.target.value = '';
      return;
    }
    let fingerprint = '';
    try {
      fingerprint = await boundedFileFingerprint(selected);
    } catch {
      setError('Unable to establish a bounded fingerprint for the selected source file.');
      event.target.value = '';
      return;
    }
    if (resumeRecord && !fileMatchesResume({
      name: selected.name,
      size: selected.size,
      lastModified: selected.lastModified,
      fingerprint
    }, resumeRecord)) {
      setError('This does not match the file recorded for the resumable upload. Reselect the exact original file.');
      event.target.value = '';
      return;
    }
    setFileFingerprint(fingerprint);
    setFile(selected);
    if (selected.name.toLowerCase().endsWith('.csv')) {
      try {
        const prefix = await selected.slice(0, csvHeaderProbeBytes).text();
        const discovered = csvHeadersFromPrefix(prefix);
        setHeaders(discovered);
        const suggestion = suggestedMapping(discovered);
        setMapping((current) => ({ ...current, ...suggestion, msisdn: suggestion.msisdn || current.msisdn }));
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : 'Unable to read the bounded CSV header');
      }
    } else {
      setMapping((current) => ({ ...current, worksheet: current.worksheet || 'Sheet1' }));
    }
  }

  function mappingField(label: string, key: keyof AudienceImportColumnMapping, required = false) {
    if (fileExtension === 'csv' && key !== 'worksheet') {
      return (
        <label>{label}
          <select
            required={required}
            value={mapping[key] ?? ''}
            onChange={(event) => setMapping({ ...mapping, [key]: event.target.value })}
          >
            <option value="">{required ? 'Select column' : 'Not provided'}</option>
            {headers.map((header) => <option key={header} value={header}>{header}</option>)}
          </select>
        </label>
      );
    }
    return (
      <label>{label}
        <input
          required={required}
          value={mapping[key] ?? ''}
          onChange={(event) => setMapping({ ...mapping, [key]: event.target.value })}
          placeholder={key === 'worksheet' ? 'Sheet1' : String(key)}
        />
      </label>
    );
  }

  async function startOrResumeUpload(event: FormEvent) {
    event.preventDefault();
    if (!file) {
      setError('Select the source file before starting the upload.');
      return;
    }
    if (!fileFingerprint) {
      setError('The source file fingerprint is unavailable. Reselect the file before continuing.');
      return;
    }
    setBusy('uploading');
    setError('');
    setNotice('');
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      let envelope = sessionEnvelope;
      let requestKey = resumeRecord?.requestKey ?? '';
      if (!envelope) {
        requestKey = idempotencyKey();
        const payload = buildUploadSessionRequest({
          organisationId,
          consentReviewId,
          purposeId,
          sourceName,
          sourceSystem,
          defaultCountryIso2: defaultCountry,
          originalFilename: file.name,
          expectedBytes: file.size,
          templateVersion,
          updatePolicy,
          mapping
        });
        envelope = await apiRequest<UploadEnvelope>(
          '/v1/audience-import-upload-sessions',
          { method: 'POST', body: JSON.stringify(payload), signal: controller.signal },
          { idempotencyKey: requestKey }
        );
        setSessionEnvelope(envelope);
        persistResume(envelope.session, requestKey, file, fileFingerprint);
      } else if (resumeRecord && !fileMatchesResume({
        name: file.name,
        size: file.size,
        lastModified: file.lastModified,
        fingerprint: fileFingerprint
      }, resumeRecord)) {
        throw new Error('The selected file no longer matches the resumable upload fingerprint.');
      }

      const transport = choosePreferredTransport(envelope.transports ?? [envelope.transport]);
      let current = envelope.session;
      for (const part of current.parts) {
        if (part.state === 'UPLOADED') continue;
        if (controller.signal.aborted) throw new DOMException('Upload paused', 'AbortError');
        const blob = file.slice(part.offset, part.offset + part.expectedBytes);
        if (blob.size !== part.expectedBytes) throw new Error('Local file no longer matches the server part manifest.');
        const checksum = await sha256Hex(blob);

        if (transport === 'DIRECT_S3') {
          const target = await apiRequest<DirectUploadTargetEnvelope>(
            '/v1/audience-import-upload-sessions/' + current.id + '/parts/' + part.number + '/target',
            { method: 'POST', body: JSON.stringify({ sha256: checksum }), signal: controller.signal }
          );
          if (!target.alreadyUploaded) {
            if (!target.target) throw new Error('Direct upload target was not returned.');
            const response = await fetch(target.target.url, {
              method: target.target.method,
              body: blob,
              headers: target.target.headers,
              credentials: 'omit',
              signal: controller.signal
            });
            if (!response.ok) throw new Error('Direct object-store upload failed with status ' + response.status);
          }
          const confirmed = await apiRequest<UploadPartEnvelope>(
            '/v1/audience-import-upload-sessions/' + current.id + '/parts/' + part.number + '/confirm',
            { method: 'POST', body: JSON.stringify({ sha256: checksum }), signal: controller.signal }
          );
          current = confirmed.session;
        } else {
          const uploaded = await apiRequest<UploadPartEnvelope>(
            '/v1/audience-import-upload-sessions/' + current.id + '/parts/' + part.number,
            {
              method: 'PUT',
              body: blob,
              headers: { 'Content-Type': 'application/octet-stream', 'X-Content-SHA256': checksum },
              signal: controller.signal
            }
          );
          current = uploaded.session;
        }
        setSessionEnvelope(sessionEnvelopeFrom(current));
      }

      const refreshed = await apiRequest<UploadEnvelope>('/v1/audience-import-upload-sessions/' + current.id);
      setSessionEnvelope(refreshed);
      current = refreshed.session;
      if (current.state === 'CREATED' || current.state === 'UPLOADING') {
        const completed = await apiRequest<UploadEnvelope>(
          '/v1/audience-import-upload-sessions/' + current.id + '/complete',
          { method: 'POST', body: JSON.stringify({ expectedVersion: current.version }) }
        );
        setSessionEnvelope(completed);
        current = completed.session;
      }
      setNotice('The source is durably uploaded. Security scanning and validation continue on the server; you may leave this page and return later.');
    } catch (cause) {
      if (cause instanceof DOMException && cause.name === 'AbortError') {
        setNotice('Upload paused. Completed parts are durable; select Resume upload to continue.');
      } else {
        setError(cause instanceof Error ? cause.message : 'Audience upload failed');
      }
    } finally {
      abortRef.current = null;
      setBusy('');
    }
  }

  function pauseUpload() {
    abortRef.current?.abort();
  }

  async function abortUpload() {
    if (!session) return;
    const reason = window.prompt('Reason for aborting this upload (minimum 8 characters):')?.trim() ?? '';
    if (reason.length < 8) return;
    setBusy('aborting');
    setError('');
    try {
      const value = await apiRequest<UploadEnvelope>(
        '/v1/audience-import-upload-sessions/' + session.id + '/abort',
        { method: 'POST', body: JSON.stringify({ expectedVersion: session.version, reason }) }
      );
      setSessionEnvelope(value);
      clearResume();
      setNotice('Upload aborted. Quarantine cleanup will run asynchronously.');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Unable to abort upload');
    } finally {
      setBusy('');
    }
  }

  async function approveImport() {
    if (!importBatch) return;
    setBusy('approving');
    setError('');
    try {
      const value = await apiRequest<AudienceImport>(
        '/v1/audience-imports/' + importBatch.id + '/approve',
        { method: 'POST', body: JSON.stringify({ expectedVersion: importBatch.version }) }
      );
      setImportBatch(value);
      setNotice('Import approved. Canonical merge is running asynchronously.');
    } catch (cause) {
      if (requiresStepUp(cause)) {
        goToStepUp();
        return;
      }
      setError(cause instanceof Error ? cause.message : 'Import approval failed');
    } finally {
      setBusy('');
    }
  }

  async function resolveConflict(conflict: ProfileConflict, resolution: 'KEEP_EXISTING' | 'USE_INCOMING') {
    if (conflictReason.trim().length < 8) {
      setError('Enter a conflict-resolution reason of at least 8 characters.');
      return;
    }
    setBusy('conflict:' + conflict.id);
    setError('');
    try {
      await apiRequest(
        '/v1/audience-import-conflicts/' + conflict.id + '/resolve',
        {
          method: 'POST',
          body: JSON.stringify({ resolution, reason: conflictReason.trim(), expectedVersion: conflict.version })
        }
      );
      if (importBatch) await loadReconciliation(importBatch.id);
    } catch (cause) {
      if (requiresStepUp(cause)) {
        goToStepUp();
        return;
      }
      setError(cause instanceof Error ? cause.message : 'Conflict resolution failed');
    } finally {
      setBusy('');
    }
  }

  async function closeReconciliation() {
    if (!importBatch || !reconciliation?.readyForClosure) return;
    if (reconciliationReason.trim().length < 8) {
      setError('Enter a reconciliation reason of at least 8 characters.');
      return;
    }
    setBusy('reconciling');
    setError('');
    try {
      await apiRequest(
        '/v1/audience-imports/' + importBatch.id + '/reconciliation',
        { method: 'POST', body: JSON.stringify({ reason: reconciliationReason.trim() }) }
      );
      setNotice('Reconciliation evidence closed and preserved.');
      await loadReconciliation(importBatch.id);
    } catch (cause) {
      if (requiresStepUp(cause)) {
        goToStepUp();
        return;
      }
      setError(cause instanceof Error ? cause.message : 'Unable to close reconciliation');
    } finally {
      setBusy('');
    }
  }

  async function openRecentImport(item: ImportListItem) {
    if (session && ['CREATED', 'UPLOADING', 'UPLOADED', 'FINALISING'].includes(session.state)) {
      setError('Pause, abort or finish the active upload before opening another import.');
      return;
    }
    clearResume();
    setSessionEnvelope(null);
    setFile(null);
    setFileFingerprint('');
    setHeaders([]);
    setReconciliation(null);
    setConflicts([]);
    setError('');
    setNotice('Loading the server-authoritative import record…');
    await loadImport(item.id);
    setNotice('');
  }

  function resetWorkspace() {
    if (session && !['ABORTED', 'EXPIRED', 'FAILED', 'IMPORT_CREATED'].includes(session.state)) {
      setError('Abort or complete the current upload before starting another one.');
      return;
    }
    clearResume();
    setSessionEnvelope(null);
    setImportBatch(null);
    setReconciliation(null);
    setConflicts([]);
    setFile(null);
    setFileFingerprint('');
    setHeaders([]);
    setMapping(emptyMapping);
    setSourceName('');
    setSourceSystem('');
    setNotice('');
    setError('');
  }

  const formLocked = Boolean(session && !['ABORTED', 'EXPIRED', 'FAILED'].includes(session.state));
  const governanceSelectionValid = Boolean(selectedReview && selectedPurpose);
  const canUpload = Boolean(
    file && fileFingerprint && organisationId && consentReviewId && purposeId && sourceName.trim() && mapping.msisdn.trim() &&
    (session ? true : governanceSelectionValid)
  );
  const uploadActive = busy === 'uploading';

  return (
    <div className="workspace-stack">
      <section className="card import-journey" aria-label="Audience import journey">
        {['Source & governance', 'Map columns', 'Transfer', 'Validate', 'Approve & merge', 'Reconcile'].map((label, index) => (
          <div key={label} className={'journey-step ' + (journeyStep === index ? 'journey-step-active' : journeyStep > index ? 'journey-step-complete' : '')}>
            <span>{index + 1}</span><strong>{label}</strong>
          </div>
        ))}
      </section>

      {error ? <div className="alert alert-danger" role="alert">{error}</div> : null}
      {notice ? <div className="alert alert-success" role="status">{notice}</div> : null}

      <div className="split-layout">
        <form className="card form-stack" onSubmit={startOrResumeUpload}>
          <div className="section-heading">
            <div>
              <span className="eyebrow">Governed source intake</span>
              <h2>Source, consent and mapping</h2>
            </div>
            {session ? <StatusBadge status={session.state} /> : null}
          </div>

          <div className="alert alert-information">
            Large files are transferred in bounded resumable parts. The browser does not parse recipient rows or persist MSISDN data. Once bytes are durable, scan, validation and reconciliation continue server-side even if you close this page.
          </div>

          <div className="grid grid-2">
            <label>Organisation
              <select disabled={formLocked} required value={organisationId} onChange={(event) => {
                setOrganisationId(event.target.value);
                setRecentImports([]);
                setRecentImportsCursor('');
                setRecentImportsHasMore(false);
                setReviews([]);
                setConsentReviewId('');
                setPurposes([]);
                setPurposeId('');
              }}>
                <option value="">Select organisation</option>
                {organisations.map((item) => <option key={item.id} value={item.id}>{item.tradingName || item.legalName}</option>)}
              </select>
            </label>
            <label>Approved consent review
              <select disabled={formLocked || !organisationId} required value={consentReviewId} onChange={(event) => {
                setConsentReviewId(event.target.value);
                setPurposes([]);
                setPurposeId('');
              }}>
                <option value="">Select approved review</option>
                {reviews.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
              </select>
            </label>
            <label>Consent purpose
              <select disabled={formLocked || !consentReviewId} required value={purposeId} onChange={(event) => setPurposeId(event.target.value)}>
                <option value="">Select active purpose</option>
                {purposes.map((item) => <option key={item.id} value={item.id}>{item.code} â€” {item.name}</option>)}
              </select>
            </label>
            <label>Default country
              <select disabled={formLocked} value={defaultCountry} onChange={(event) => setDefaultCountry(event.target.value)}>
                <option value="NG">Nigeria</option><option value="GH">Ghana</option><option value="GB">United Kingdom</option>
              </select>
            </label>
            <label>Source name<input disabled={formLocked} required value={sourceName} onChange={(event) => setSourceName(event.target.value)} placeholder="e.g. August registrations" /></label>
            <label>Source system<input disabled={formLocked} value={sourceSystem} onChange={(event) => setSourceSystem(event.target.value)} placeholder="CRM, registration portal, partner export" /></label>
            <label>Template version<input disabled={formLocked} required value={templateVersion} onChange={(event) => setTemplateVersion(event.target.value)} /></label>
            <label>Update policy
              <select disabled={formLocked} value={updatePolicy} onChange={(event) => setUpdatePolicy(event.target.value as typeof updatePolicy)}>
                <option value="NEWEST_SOURCE">Newest trusted observation</option>
                <option value="FILL_NULL">Fill missing values only</option>
                <option value="INSERT_ONLY">Insert new contacts only</option>
                <option value="TRUSTED_SOURCE">Trusted-source precedence</option>
                <option value="MANUAL_CONFLICT">Manual conflict review</option>
              </select>
            </label>
          </div>

          {selectedReview ? (
            <div className="alert alert-success">
              <strong>{selectedReview.name}</strong> is approved{selectedReview.expiresAt ? ' until ' + new Date(selectedReview.expiresAt).toLocaleDateString() : ''}.
              {selectedReview.restrictions ? <small>Restrictions: {selectedReview.restrictions}</small> : null}
            </div>
          ) : null}
          {selectedPurpose ? <small>Purpose: {selectedPurpose.description || selectedPurpose.name} Â· wording {selectedPurpose.wordingVersion}</small> : null}

          <label>Audience source file
            <input
              disabled={formLocked && !resumeRecord}
              type="file"
              accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
              onChange={chooseFile}
            />
          </label>
          {resumeRecord && !file ? (
            <div className="alert alert-warning">
              Resumable upload found for <strong>{resumeRecord.fileName}</strong>. Reselect the same local file to continue; completed server parts will not be re-uploaded.
            </div>
          ) : null}

          {file ? (
            <div className="upload-file-summary">
              <strong>{file.name}</strong>
              <span>{file.size.toLocaleString()} bytes</span>
              <span>{fileExtension === 'csv' ? headers.length + ' CSV columns discovered from a bounded header probe' : 'XLSX mapping uses explicit worksheet and column names'}</span>
            </div>
          ) : null}

          {file ? (
            <>
              <h3>Column mapping</h3>
              {fileExtension === 'xlsx' ? <div className="grid grid-2">{mappingField('Worksheet', 'worksheet', true)}</div> : null}
              <div className="grid grid-2">
                {mappingField('MSISDN column', 'msisdn', true)}
                {mappingField('Country column', 'country')}
                {mappingField('State column', 'state')}
                {mappingField('LGA column', 'lga')}
                {mappingField('Age column', 'age')}
                {mappingField('Gender column', 'gender')}
              </div>
            </>
          ) : null}

          <div className="button-row">
            <button className="primary" disabled={!canUpload || uploadActive || ['UPLOADED', 'FINALISING', 'IMPORT_CREATED'].includes(session?.state ?? '')}>
              {uploadActive ? 'Transferringâ€¦' : session && (session.state === 'CREATED' || session.state === 'UPLOADING') ? 'Resume upload' : 'Start resumable upload'}
            </button>
            {uploadActive ? <button type="button" className="secondary" onClick={pauseUpload}>Pause transfer</button> : null}
            {session && ['CREATED', 'UPLOADING', 'UPLOADED'].includes(session.state) ? (
              <button type="button" className="danger-ghost" disabled={busy === 'aborting'} onClick={() => void abortUpload()}>Abort upload</button>
            ) : null}
            {(session || importBatch) ? <button type="button" className="secondary" onClick={resetWorkspace}>New import</button> : null}
          </div>
        </form>

        <aside className="card sticky-panel">
          <h2>Durable job status</h2>
          {!session && !importBatch ? (
            <div className="empty-state"><strong>No active import</strong><p>Select governed context and a source file to begin.</p></div>
          ) : null}
          {session ? (
            <>
              <dl className="definition-list">
                <div><dt>Upload session</dt><dd className="mono-small">{session.id}</dd></div>
                <div><dt>Stage</dt><dd>{nextImportStage(session.state)}</dd></div>
                <div><dt>Transport</dt><dd>{sessionEnvelope?.preferredTransport || sessionEnvelope?.transport || 'RELAY'}</dd></div>
                <div><dt>Expires</dt><dd>{new Date(session.expiresAt).toLocaleString()}</dd></div>
              </dl>
              {progress ? (
                <div className="upload-progress" aria-label={'Upload progress ' + progress.percent + ' percent'}>
                  <div className="progress-track"><span style={{ width: progress.percent + '%' }} /></div>
                  <div className="progress-meta"><strong>{progress.percent}% durable</strong><span>{progress.bytesLabel}</span><span>{progress.partsLabel}</span></div>
                </div>
              ) : null}
              {session.failureReason ? <div className="alert alert-danger">{session.failureReason}</div> : null}
            </>
          ) : null}

          {importBatch ? (
            <>
              <h3>Import processing</h3>
              <div className="section-heading"><StatusBadge status={importBatch.status} /><StatusBadge status={importBatch.malwareStatus} /></div>
              <div className="mini-metric-grid">
                <div className="metric-tile"><span>Uploaded</span><strong>{importBatch.uploadedRows.toLocaleString()}</strong></div>
                <div className="metric-tile success"><span>Valid</span><strong>{importBatch.validRows.toLocaleString()}</strong></div>
                <div className="metric-tile danger"><span>Invalid</span><strong>{importBatch.invalidRows.toLocaleString()}</strong></div>
                <div className="metric-tile warning"><span>Duplicates</span><strong>{importBatch.duplicateRows.toLocaleString()}</strong></div>
              </div>
              {importBatch.failureReason ? <div className="alert alert-danger">{importBatch.failureReason}</div> : null}
              <a className="text-link" href={browserAPIBase + '/v1/audience-imports/' + importBatch.id + '/issues.csv'}>Download bounded issue report</a>
            </>
          ) : null}
        </aside>
      </div>

      <section className="card">
        <div className="workspace-hero">
          <div>
            <span className="eyebrow">Recoverable server jobs</span>
            <h2>Recent imports</h2>
            <p className="muted">Running and completed imports remain discoverable from server state after browser closure or device change.</p>
          </div>
          <div className="button-row">
            <button
              type="button"
              className="secondary"
              disabled={!organisationId || recentImportsLoading}
              onClick={() => void loadRecentImports(organisationId)}
            >
              {recentImportsLoading ? 'Refreshing…' : 'Refresh'}
            </button>
          </div>
        </div>
        {!organisationId ? (
          <div className="empty-state"><strong>Select an organisation</strong><p>Recent import jobs are intentionally organisation-scoped.</p></div>
        ) : recentImportsLoading && recentImports.length === 0 ? (
          <div className="empty-state"><strong>Loading recent imports…</strong><p>Reading server-authoritative job state.</p></div>
        ) : recentImports.length === 0 ? (
          <div className="empty-state"><strong>No imports found</strong><p>This organisation has no recent audience import records.</p></div>
        ) : (
          <>
            <div className="table-wrap">
              <table>
                <thead><tr><th>Source</th><th>File</th><th>Status</th><th>Valid / uploaded</th><th>Updated</th><th>Action</th></tr></thead>
                <tbody>
                  {recentImports.map((item) => (
                    <tr key={item.id}>
                      <td><strong>{item.sourceName}</strong>{item.sourceSystem ? <small>{item.sourceSystem}</small> : null}</td>
                      <td>{item.originalFilename}<small>{item.byteSize.toLocaleString()} bytes</small></td>
                      <td><StatusBadge status={item.status} />{item.failureReason ? <small>{item.failureReason}</small> : null}</td>
                      <td>{item.validRows.toLocaleString()} / {item.uploadedRows.toLocaleString()}<small>{item.invalidRows.toLocaleString()} invalid · {item.duplicateRows.toLocaleString()} duplicates</small></td>
                      <td>{new Date(item.updatedAt).toLocaleString()}</td>
                      <td><button type="button" className="secondary compact-button" onClick={() => void openRecentImport(item)}>Open</button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {recentImportsHasMore ? (
              <div className="button-row">
                <button
                  type="button"
                  className="secondary"
                  disabled={recentImportsLoading || !recentImportsCursor}
                  onClick={() => void loadRecentImports(organisationId, recentImportsCursor, true)}
                >
                  {recentImportsLoading ? 'Loading…' : 'Load more'}
                </button>
              </div>
            ) : null}
          </>
        )}
      </section>

      {importBatch?.status === 'PREVIEW_READY' ? (
        <section className="card">
          <div className="workspace-hero">
            <div>
              <span className="eyebrow">Maker-checker boundary</span>
              <h2>Validation review</h2>
              <p className="muted">Review authoritative totals before approving canonical contact mutation. The uploader cannot self-approve.</p>
            </div>
            <StatusBadge status={importBatch.status} />
          </div>
          <div className="grid grid-4">
            <div className="metric-tile"><span>Uploaded</span><strong>{importBatch.uploadedRows.toLocaleString()}</strong></div>
            <div className="metric-tile success"><span>Valid</span><strong>{importBatch.validRows.toLocaleString()}</strong></div>
            <div className="metric-tile danger"><span>Invalid</span><strong>{importBatch.invalidRows.toLocaleString()}</strong></div>
            <div className="metric-tile warning"><span>Duplicates</span><strong>{importBatch.duplicateRows.toLocaleString()}</strong></div>
          </div>
          <div className="button-row">
            <button className="primary" disabled={busy === 'approving'} onClick={() => void approveImport()}>{busy === 'approving' ? 'Approvingâ€¦' : 'Approve import and start merge'}</button>
          </div>
        </section>
      ) : null}

      {reconciliation ? (
        <section className="card">
          <div className="workspace-hero">
            <div>
              <span className="eyebrow">Post-merge accounting</span>
              <h2>Reconciliation</h2>
              <p className="muted">Closure is available only when row totals balance and no profile conflicts remain pending.</p>
            </div>
            <StatusBadge status={reconciliation.readyForClosure ? 'READY' : 'PENDING_REVIEW'} />
          </div>
          <div className="grid grid-4">
            <div className="metric-tile"><span>Validation accounted</span><strong>{reconciliation.validationAccounted.toLocaleString()}</strong></div>
            <div className="metric-tile success"><span>Inserted</span><strong>{reconciliation.insertedContacts.toLocaleString()}</strong></div>
            <div className="metric-tile"><span>Updated</span><strong>{reconciliation.updatedContacts.toLocaleString()}</strong></div>
            <div className="metric-tile warning"><span>Pending conflicts</span><strong>{reconciliation.conflicts.pending.toLocaleString()}</strong></div>
          </div>
          {reconciliation.warnings.length ? <div className="alert alert-warning">{reconciliation.warnings.join(' Â· ')}</div> : <div className="alert alert-success">Accounting checks are balanced.</div>}

          {conflicts.length ? (
            <>
              <h3>Pending profile conflicts</h3>
              <label className="field-label">Resolution reason<input value={conflictReason} onChange={(event) => setConflictReason(event.target.value)} /></label>
              <div className="table-wrap">
                <table>
                  <thead><tr><th>Recipient</th><th>Field</th><th>Existing</th><th>Incoming</th><th>Decision</th></tr></thead>
                  <tbody>
                    {conflicts.map((conflict) => (
                      <tr key={conflict.id}>
                        <td>{conflict.maskedMsisdn}</td>
                        <td>{conflict.field}</td>
                        <td>{conflict.existingValue || 'â€”'}</td>
                        <td>{conflict.incomingValue || 'â€”'}</td>
                        <td>
                          <div className="button-row">
                            <button className="secondary compact-button" disabled={busy === 'conflict:' + conflict.id} onClick={() => void resolveConflict(conflict, 'KEEP_EXISTING')}>Keep existing</button>
                            <button className="primary compact-button" disabled={busy === 'conflict:' + conflict.id} onClick={() => void resolveConflict(conflict, 'USE_INCOMING')}>Use incoming</button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {reconciliation.conflicts.pending > conflicts.length ? <small>Showing the first {conflicts.length} pending conflicts. Resolve these and refresh to continue through the bounded queue.</small> : null}
            </>
          ) : null}

          <div className="reconciliation-close">
            <label>Closure reason<input value={reconciliationReason} onChange={(event) => setReconciliationReason(event.target.value)} /></label>
            <button className="primary" disabled={!reconciliation.readyForClosure || busy === 'reconciling'} onClick={() => void closeReconciliation()}>
              {busy === 'reconciling' ? 'Closingâ€¦' : 'Close reconciliation evidence'}
            </button>
          </div>
        </section>
      ) : null}
    </div>
  );
}
