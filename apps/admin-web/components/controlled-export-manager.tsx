'use client';

import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { useAuth } from './auth-provider';
import { StatusBadge } from './status-badge';
import { APIError, apiRequest, collectBoundedPages, type ListEnvelope } from '../lib/api';
import { browserAPIBase } from '../lib/api';
import { hasPermission } from '../lib/session';

type ExportKind = 'CAMPAIGN_REPORT' | 'AUDIT_LOG' | 'PRIVACY_PACKAGE';
type ExportFormat = 'CSV' | 'JSON' | 'PDF' | 'XLSX';

type ControlledExport = {
  id: string;
  kind: ExportKind;
  objectId?: string;
  format: ExportFormat;
  status: string;
  requestedBy: string;
  approvedBy?: string;
  reason: string;
  rejectionReason?: string;
  templateVersion?: string;
  version: number;
  createdAt: string;
  updatedAt: string;
  expiresAt?: string;
  sizeBytes?: number;
  failureCode?: string;
  downloadCount?: number;
};

type DownloadAuthorization = {
  token: string;
  grant: { expiresAt: string };
};

type ControlledExportManagerProps = {
  campaignId?: string;
  campaignName?: string;
  kind?: ExportKind;
  objectId?: string;
  title?: string;
};

function formatDate(value?: string) {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString();
}

function describeKind(kind: ExportKind) {
  if (kind === 'AUDIT_LOG') return 'Audit evidence';
  if (kind === 'PRIVACY_PACKAGE') return 'Privacy package';
  return 'Campaign report';
}

export function ControlledExportManager({
  campaignId,
  campaignName,
  kind = 'CAMPAIGN_REPORT',
  objectId,
  title = 'Controlled exports'
}: ControlledExportManagerProps) {
  const { session } = useAuth();
  const [items, setItems] = useState<ControlledExport[]>([]);
  const [reason, setReason] = useState('');
  const [decisionReason, setDecisionReason] = useState('');
  const [format, setFormat] = useState<ExportFormat>('PDF');
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');
  const [downloadLinks, setDownloadLinks] = useState<Record<string, { href: string; expiresAt: string }>>({});
  const requestSequence = useRef(0);

  const canRequest = kind === 'PRIVACY_PACKAGE'
    ? hasPermission(session?.permissions, 'privacy.read')
    : hasPermission(session?.permissions, 'export.request');
  const canApprove = hasPermission(session?.permissions, 'export.approve');
  const query = useMemo(() => '/v1/exports?kind=' + encodeURIComponent(kind) + '&limit=100' + (objectId ? '&objectId=' + encodeURIComponent(objectId) : ''), [kind, objectId]);

  function requiresStepUp(error: unknown) {
    return error instanceof APIError && (error.code === 'STEP_UP_REQUIRED' || error.code === 'MFA_STEP_UP_REQUIRED');
  }

  function goToStepUp() {
    window.location.assign('/step-up?returnTo=' + encodeURIComponent('/privacy'));
  }

  const load = useCallback(async () => {
    const request = ++requestSequence.current;
    try {
      const values = await collectBoundedPages(
        (cursor) => apiRequest<ListEnvelope<ControlledExport>>(query + (cursor ? '&after=' + encodeURIComponent(cursor) : '')),
        20
      );
      if (request === requestSequence.current) setItems(values);
    } catch (cause) {
      if (requiresStepUp(cause)) {
        goToStepUp();
        return;
      }
      setMessage(cause instanceof Error ? cause.message : 'Controlled exports could not be loaded.');
    }
  }, [query]);

  useEffect(() => {
    // The initial load synchronizes the controlled-export inventory into local state.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [load]);

  async function requestExport(event: FormEvent) {
    event.preventDefault();
    if (!canRequest || !reason.trim()) return;
    setBusy('request');
    setMessage('');
    try {
      const requestPath = kind === 'PRIVACY_PACKAGE'
        ? '/v1/privacy/cases/' + encodeURIComponent(objectId ?? '') + '/exports'
        : '/v1/exports';
      await apiRequest<ControlledExport>(requestPath, {
        method: 'POST',
        body: JSON.stringify({
          ...(kind === 'PRIVACY_PACKAGE' ? {} : {
            kind,
            objectId: kind === 'CAMPAIGN_REPORT' ? objectId ?? campaignId ?? '' : ''
          }),
          format,
          reason: reason.trim(),
          criteria: kind === 'AUDIT_LOG' ? {} : undefined
        })
      });
      setReason('');
      await load();
    } catch (cause) {
      if (requiresStepUp(cause)) {
        goToStepUp();
        return;
      }
      setMessage(cause instanceof Error ? cause.message : 'The controlled export request failed.');
    } finally {
      setBusy('');
    }
  }

  async function decideExport(item: ControlledExport, approve: boolean) {
    if (!canApprove) return;
    setBusy(item.id);
    setMessage('');
    try {
      await apiRequest<ControlledExport>('/v1/exports/' + encodeURIComponent(item.id) + '/decision', {
        method: 'POST',
        body: JSON.stringify({
          expectedVersion: item.version,
          approve,
          reason: (decisionReason || (approve ? 'Approved for governed review.' : 'Rejected by checker.')).trim()
        })
      });
      setDecisionReason('');
      await load();
    } catch (cause) {
      if (requiresStepUp(cause)) {
        goToStepUp();
        return;
      }
      setMessage(cause instanceof Error ? cause.message : 'The export decision failed.');
    } finally {
      setBusy('');
    }
  }

  async function authorizeDownload(item: ControlledExport) {
    setBusy(item.id);
    setMessage('');
    try {
      const authorization = await apiRequest<DownloadAuthorization>('/v1/exports/' + encodeURIComponent(item.id) + '/download-authorisations', {
        method: 'POST',
        body: JSON.stringify({})
      });
      setDownloadLinks((current) => ({
        ...current,
        [item.id]: {
          href: browserAPIBase + '/v1/exports/' + encodeURIComponent(item.id) + '/download?token=' + encodeURIComponent(authorization.token),
          expiresAt: authorization.grant.expiresAt
        }
      }));
    } catch (cause) {
      if (requiresStepUp(cause)) {
        goToStepUp();
        return;
      }
      setMessage(cause instanceof Error ? cause.message : 'A download authorisation could not be created.');
    } finally {
      setBusy('');
    }
  }

  async function revokeExport(item: ControlledExport) {
    if (!canApprove) return;
    setBusy(item.id);
    setMessage('');
    try {
      await apiRequest<ControlledExport>('/v1/exports/' + encodeURIComponent(item.id) + '/revoke', {
        method: 'POST',
        body: JSON.stringify({
          expectedVersion: item.version,
          reason: 'Revoked by authorised checker.'
        })
      });
      await load();
    } catch (cause) {
      if (requiresStepUp(cause)) {
        goToStepUp();
        return;
      }
      setMessage(cause instanceof Error ? cause.message : 'The export could not be revoked.');
    } finally {
      setBusy('');
    }
  }

  return (
    <section className="card controlled-export-manager" aria-labelledby="controlled-export-title">
      <div className="section-heading">
        <div>
          <h2 id="controlled-export-title">{title}</h2>
          <p className="muted">{describeKind(kind)}{campaignName ? ' · ' + campaignName : ''}. Requests are purpose-bound, independently approved and time-limited.</p>
        </div>
      </div>

      {message ? <div className="alert alert-danger" role="alert">{message}</div> : null}

      {canRequest ? (
        <form className="form-stack export-request-form" onSubmit={requestExport}>
          <label>Format<select value={format} onChange={(event) => setFormat(event.target.value as ExportFormat)}><option value="PDF">PDF</option><option value="CSV">CSV</option><option value="JSON">JSON</option><option value="XLSX">XLSX</option></select></label>
          <label>Purpose<textarea value={reason} onChange={(event) => setReason(event.target.value)} minLength={8} maxLength={500} required placeholder="Explain why this export is needed." /></label>
          <button className="primary" type="submit" disabled={busy !== '' || !reason.trim()}>{busy === 'request' ? 'Requesting…' : 'Request controlled export'}</button>
        </form>
      ) : (
        <p className="muted">{kind === 'PRIVACY_PACKAGE' ? 'You need privacy.read permission to request a controlled package.' : 'You need export.request permission to request a controlled export.'}</p>
      )}

      {canApprove ? (
        <label className="form-stack"><span>Checker decision note</span><textarea value={decisionReason} onChange={(event) => setDecisionReason(event.target.value)} maxLength={500} placeholder="Optional approval or rejection context." /></label>
      ) : null}

      <div className="workspace-stack export-list" aria-live="polite">
        {!items.length ? <p className="muted">No controlled exports yet.</p> : items.map((item) => {
          const download = downloadLinks[item.id];
          const pending = item.status === 'PENDING_APPROVAL';
          const isRequester = item.requestedBy === session?.id;
          const downloadable = item.status === 'READY';
          return (
            <article className="card card-subtle" key={item.id}>
              <div className="section-heading">
                <div><strong>{item.format} · {item.kind}</strong><p className="muted">{item.reason}</p></div>
                <StatusBadge status={item.status} />
              </div>
              <dl className="definition-list">
                <div><dt>Requested</dt><dd>{formatDate(item.createdAt)}</dd></div>
                <div><dt>Requester</dt><dd>{item.requestedBy}</dd></div>
                <div><dt>Version</dt><dd>{item.version}</dd></div>
                {item.expiresAt ? <div><dt>Expires</dt><dd>{formatDate(item.expiresAt)}</dd></div> : null}
                {item.rejectionReason ? <div><dt>Rejection</dt><dd>{item.rejectionReason}</dd></div> : null}
              </dl>
              <div className="button-row">
                {pending && canApprove && !isRequester ? <><button type="button" className="primary" onClick={() => void decideExport(item, true)} disabled={busy === item.id}>Approve export</button><button type="button" onClick={() => void decideExport(item, false)} disabled={busy === item.id}>Reject export</button></> : null}
                {pending && isRequester ? <p className="muted">Awaiting an independent checker; the requester cannot approve this export.</p> : null}
                {downloadable ? <button type="button" onClick={() => void authorizeDownload(item)} disabled={busy === item.id}>Authorise download</button> : null}
                {canApprove && !['EXPIRED', 'REJECTED', 'REVOKED'].includes(item.status) ? <button type="button" onClick={() => void revokeExport(item)} disabled={busy === item.id}>Revoke export</button> : null}
                {download ? <a className="button primary" href={download.href}>Download export</a> : null}
              </div>
              {download ? <p className="muted">Download authorisation expires {formatDate(download.expiresAt)} and can be used once.</p> : null}
            </article>
          );
        })}
      </div>
    </section>
  );
}
