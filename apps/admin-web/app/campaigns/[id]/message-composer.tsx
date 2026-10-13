'use client';

import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';

import { useAuth } from '../../../components/auth-provider';
import { StatusBadge } from '../../../components/status-badge';
import { apiRequest, collectBoundedPages, type ListEnvelope } from '../../../lib/api';
import { type CampaignDetail } from '../../../lib/campaign-preparation-model';
import { hasPermission } from '../../../lib/session';

type MessageType = 'TEXT' | 'IMAGE_CAPTION' | 'VIDEO' | 'DOCUMENT';
type MessageVariable = { name: string; dataType: string; fallback: string };
type MessageLink = { url: string; label?: string };
type MessageMedia = {
  assetId: string;
  sha256: string;
  mediaType: string;
  size: number;
  scanStatus: string;
};
type MessageVersion = {
  id: string;
  campaignId: string;
  version: number;
  type: MessageType;
  body?: string;
  media?: MessageMedia;
  links: MessageLink[];
  variables: MessageVariable[];
  contentHash: string;
  status: 'DRAFT' | 'APPROVED' | 'SUPERSEDED';
  createdBy: string;
  approvedBy?: string;
  createdAt: string;
  approvedAt?: string;
};
type TrustedAssetMetadata = {
  id: string;
  purpose: string;
  originalFilename: string;
  mediaType: string;
  size: number;
  sha256: string;
  status: string;
  rightsBasis?: string;
  rightsReference?: string;
  rightsExpiresAt?: string;
  version: number;
};
type PreviewResult = {
  body: string;
  resolved: Record<string, string>;
  usedFallbacks: string[];
  missing: string[];
  undeclared: string[];
  characterCount: number;
  byteCount: number;
};

const editableCampaignStates = new Set(['AUDIENCE_VALIDATED']);
const visibleCampaignStates = new Set([
  'AUDIENCE_VALIDATED', 'MESSAGE_REVIEW_PENDING', 'MESSAGE_APPROVED', 'COMMERCIAL_APPROVED',
  'FINAL_APPROVAL_PENDING', 'SCHEDULED', 'DISPATCHING', 'PAUSED', 'COMPLETED',
  'COMPLETED_WITH_EXCEPTIONS'
]);

function parseVariables(value: string): MessageVariable[] {
  return value.split(/\r?\n/).map(line => line.trim()).filter(Boolean).map((line, index) => {
    const [name = '', dataType = 'TEXT', ...fallback] = line.split('|');
    const cleanName = name.trim();
    if (!/^[A-Za-z][A-Za-z0-9_.-]{0,63}$/.test(cleanName)) {
      throw new Error('Variable line ' + (index + 1) + ' has an invalid name.');
    }
    return { name: cleanName, dataType: dataType.trim().toUpperCase() || 'TEXT', fallback: fallback.join('|').trim() };
  });
}

function parseLinks(value: string): MessageLink[] {
  return value.split(/\r?\n/).map(line => line.trim()).filter(Boolean).map((line, index) => {
    const parts = line.split('|').map(part => part.trim());
    const url = parts.length === 1 ? parts[0] : parts[parts.length - 1];
    const label = parts.length > 1 ? parts.slice(0, -1).join(' | ') : '';
    if (!url.startsWith('https://')) throw new Error('Link line ' + (index + 1) + ' must use HTTPS.');
    return { url, ...(label ? { label } : {}) };
  });
}

function parsePreviewValues(value: string): Record<string, string> {
  const result: Record<string, string> = {};
  for (const [index, line] of value.split(/\r?\n/).entries()) {
    if (!line.trim()) continue;
    const separator = line.indexOf('=');
    if (separator < 1) throw new Error('Preview value line ' + (index + 1) + ' must use name=value.');
    result[line.slice(0, separator).trim()] = line.slice(separator + 1).trim();
  }
  return result;
}

function shortHash(value: string): string {
  return value ? value.slice(0, 12) + '…' : '—';
}

export function MessageComposer({
  campaign,
  onChanged
}: {
  campaign: CampaignDetail;
  onChanged: () => Promise<void>;
}) {
  const { session } = useAuth();
  const canRead = hasPermission(session?.permissions, 'campaign.read');
  const canWrite = hasPermission(session?.permissions, 'campaign.write');
  const canApprove = hasPermission(session?.permissions, 'campaign.approve');

  const [versions, setVersions] = useState<MessageVersion[]>([]);
  const [selectedId, setSelectedId] = useState('');
  const [messageType, setMessageType] = useState<MessageType>('TEXT');
  const [asset, setAsset] = useState<TrustedAssetMetadata>();
  const [preview, setPreview] = useState<PreviewResult>();
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');

  const visible = visibleCampaignStates.has(campaign.status);
  const canCreate = canWrite && editableCampaignStates.has(campaign.status);
  const selected = useMemo(() => versions.find(item => item.id === selectedId), [versions, selectedId]);

  const readVersions = useCallback(async (): Promise<MessageVersion[]> => {
    if (!visible || !canRead) return [];
    return collectBoundedPages<MessageVersion>(
      cursor => apiRequest<ListEnvelope<MessageVersion>>(
        '/v1/campaigns/' + encodeURIComponent(campaign.id) + '/message-versions?limit=100' +
        (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
      ),
      10
    );
  }, [campaign.id, canRead, visible]);

  const applyVersions = useCallback((values: MessageVersion[]) => {
    setVersions(values);
    setSelectedId(current => {
      if (campaign.messageVersionId && values.some(item => item.id === campaign.messageVersionId)) return campaign.messageVersionId;
      if (current && values.some(item => item.id === current)) return current;
      return values[0]?.id ?? '';
    });
  }, [campaign.messageVersionId]);

  const loadVersions = useCallback(async () => {
    try {
      applyVersions(await readVersions());
    } catch (error) {
      setNotice(error instanceof Error ? error.message : 'Message versions could not be loaded.');
    }
  }, [applyVersions, readVersions]);

  useEffect(() => {
    if (!visible || !canRead) return;
    let cancelled = false;
    void readVersions()
      .then(values => {
        if (!cancelled) applyVersions(values);
      })
      .catch(error => {
        if (!cancelled) setNotice(error instanceof Error ? error.message : 'Message versions could not be loaded.');
      });
    return () => {
      cancelled = true;
    };
  }, [applyVersions, canRead, readVersions, visible]);

  if (!visible || !canRead) return null;

  async function uploadMedia(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canCreate || busy) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    const fileInput = form.elements.namedItem('file') as HTMLInputElement | null;
    const file = fileInput?.files?.[0];
    if (!file || file.size === 0) {
      setNotice('Choose a non-empty media file.');
      return;
    }
    const rightsReference = String(data.get('rightsReference') ?? '').trim();
    if (rightsReference.length < 3) {
      setNotice('Enter the governed media-rights evidence reference.');
      return;
    }
    const upload = new FormData();
    upload.set('file', file);
    upload.set('rightsBasis', String(data.get('rightsBasis') ?? '').trim());
    upload.set('rightsReference', rightsReference);
    const expiry = String(data.get('rightsExpiresAt') ?? '').trim();
    if (expiry) {
      const parsed = new Date(expiry);
      if (Number.isNaN(parsed.valueOf())) {
        setNotice('Enter a valid media-rights expiry.');
        return;
      }
      upload.set('rightsExpiresAt', parsed.toISOString());
    }
    setBusy('Scanning media');
    setNotice('');
    try {
      const created = await apiRequest<TrustedAssetMetadata>('/v1/assets?purpose=MESSAGE_MEDIA', {
        method: 'POST', body: upload
      });
      if (created.status !== 'CLEAN' || created.purpose !== 'MESSAGE_MEDIA') {
        throw new Error('The server did not return a clean governed message-media asset.');
      }
      setAsset(created);
      setNotice('Media passed malware scanning and rights validation. Only the governed asset ID will be attached.');
      form.reset();
    } catch (error) {
      setNotice(error instanceof Error ? error.message : 'Media could not be accepted.');
    } finally {
      setBusy('');
    }
  }

  async function loadExistingAsset(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const id = String(data.get('assetId') ?? '').trim();
    if (!id) return;
    setBusy('Loading media');
    try {
      const value = await apiRequest<TrustedAssetMetadata>('/v1/assets/' + encodeURIComponent(id));
      if (value.purpose !== 'MESSAGE_MEDIA' || value.status !== 'CLEAN' || !value.rightsBasis || !value.rightsReference) {
        throw new Error('The asset is not currently eligible governed message media.');
      }
      setAsset(value);
      setNotice('Existing governed media loaded. Rights and scan evidence remain server-authoritative.');
    } catch (error) {
      setNotice(error instanceof Error ? error.message : 'Media asset could not be loaded.');
    } finally {
      setBusy('');
    }
  }

  async function createVersion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canCreate || busy) return;
    const data = new FormData(event.currentTarget);
    let variables: MessageVariable[];
    let links: MessageLink[];
    try {
      variables = parseVariables(String(data.get('variables') ?? ''));
      links = parseLinks(String(data.get('links') ?? ''));
    } catch (error) {
      setNotice(error instanceof Error ? error.message : 'Message fields are invalid.');
      return;
    }
    if (messageType !== 'TEXT' && !asset?.id) {
      setNotice('Upload or load CLEAN rights-governed media before creating a media version.');
      return;
    }
    setBusy('Creating message version');
    setNotice('');
    try {
      const created = await apiRequest<MessageVersion>(
        '/v1/campaigns/' + encodeURIComponent(campaign.id) + '/message-versions',
        {
          method: 'POST',
          body: JSON.stringify({
            type: messageType,
            body: String(data.get('body') ?? ''),
            ...(messageType !== 'TEXT' ? { mediaAssetId: asset?.id } : {}),
            variables,
            links
          })
        },
        { idempotencyKey: crypto.randomUUID() }
      );
      await loadVersions();
      setSelectedId(created.id);
      setPreview(undefined);
      setNotice('Immutable message version ' + created.version + ' created.');
    } catch (error) {
      setNotice(error instanceof Error ? error.message : 'Message version could not be created.');
    } finally {
      setBusy('');
    }
  }

  async function previewVersion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected || busy) return;
    const data = new FormData(event.currentTarget);
    let values: Record<string, string>;
    try {
      values = parsePreviewValues(String(data.get('previewValues') ?? ''));
    } catch (error) {
      setNotice(error instanceof Error ? error.message : 'Preview values are invalid.');
      return;
    }
    setBusy('Rendering masked preview');
    try {
      const value = await apiRequest<PreviewResult>('/v1/message-versions/' + encodeURIComponent(selected.id) + '/preview', {
        method: 'POST',
        body: JSON.stringify({ values, maskSensitive: true })
      });
      setPreview(value);
      setNotice('Masked representative preview rendered by the server.');
    } catch (error) {
      setNotice(error instanceof Error ? error.message : 'Preview could not be rendered.');
    } finally {
      setBusy('');
    }
  }

  async function approveVersion() {
    if (!selected || !canApprove || busy) return;
    setBusy('Approving message version');
    try {
      const approved = await apiRequest<MessageVersion>('/v1/message-versions/' + encodeURIComponent(selected.id) + '/approve', {
        method: 'POST',
        body: JSON.stringify({ expectedContentHash: selected.contentHash })
      });
      await loadVersions();
      setSelectedId(approved.id);
      setNotice('Message version approved independently. Campaign approval remains a separate action.');
    } catch (error) {
      setNotice(error instanceof Error ? error.message : 'Message version could not be approved.');
    } finally {
      setBusy('');
    }
  }

  async function transitionMessage(action: 'SUBMIT_MESSAGE' | 'APPROVE_MESSAGE', version?: MessageVersion) {
    if (busy) return;
    setBusy(action === 'SUBMIT_MESSAGE' ? 'Submitting message review' : 'Approving campaign message');
    try {
      await apiRequest('/v1/campaigns/' + encodeURIComponent(campaign.id) + '/transition', {
        method: 'POST',
        body: JSON.stringify({
          action,
          expectedVersion: campaign.version,
          reason: action === 'SUBMIT_MESSAGE'
            ? 'Submitted immutable message version for independent review'
            : 'Approved after independent message-version review',
          ...(version ? { messageVersionId: version.id } : {})
        })
      });
      await onChanged();
      setNotice(action === 'SUBMIT_MESSAGE'
        ? 'Message version submitted for independent review.'
        : 'Campaign message approval recorded. Controlled testing may proceed.');
    } catch (error) {
      setNotice(error instanceof Error ? error.message : 'Campaign message transition failed.');
    } finally {
      setBusy('');
    }
  }

  const selectedIsCampaignMessage = Boolean(selected && campaign.messageVersionId === selected.id);
  const makerIsCurrentOperator = Boolean(selected && session?.id && selected.createdBy === session.id);

  return (
    <section className="card form-stack" aria-label="Message composer and versioning">
      <div className="section-heading">
        <div>
          <h2>Message & media</h2>
          <p className="muted">Create immutable message versions, attach only CLEAN rights-governed media, preview masked representative content, and keep maker/checker approval separate.</p>
        </div>
        {busy ? <span className="pill">{busy}…</span> : null}
      </div>

      {notice ? <div className="alert alert-information" role="status">{notice}</div> : null}

      {canCreate ? (
        <div className="grid grid-2">
          <form className="form-stack" onSubmit={createVersion}>
            <h3>Create immutable version</h3>
            <label>Message type
              <select value={messageType} onChange={event => { setMessageType(event.target.value as MessageType); if (event.target.value === 'TEXT') setAsset(undefined); }}>
                <option value="TEXT">Text</option>
                <option value="IMAGE_CAPTION">Image + caption</option>
                <option value="VIDEO">Video</option>
                <option value="DOCUMENT">Document</option>
              </select>
            </label>
            <label>Message body<textarea name="body" rows={6} required={messageType === 'TEXT'} placeholder="Use {{variable_name}} placeholders for governed variables." /></label>
            <label>Variables<small>One per line: name | type | fallback. Types: TEXT, INTEGER, DECIMAL, BOOLEAN, DATE, MSISDN, EMAIL, PERSONAL, SENSITIVE.</small><textarea name="variables" rows={4} placeholder="first_name | PERSONAL | Customer" /></label>
            <label>Links<small>One per line: label | https://allowed.example/path. The server enforces the destination allow-list.</small><textarea name="links" rows={3} /></label>
            {messageType !== 'TEXT' ? (
              <div className="alert alert-information">
                <strong>{asset ? 'Media ready' : 'Governed media required'}</strong>
                <small>{asset
                  ? asset.originalFilename + ' · ' + asset.mediaType + ' · ' + asset.size.toLocaleString() + ' bytes · ' + shortHash(asset.sha256)
                  : 'Upload or load an existing CLEAN media asset below.'}</small>
              </div>
            ) : null}
            <button className="primary" disabled={Boolean(busy) || (messageType !== 'TEXT' && !asset)}>Create message version</button>
          </form>

          {messageType !== 'TEXT' ? (
            <div className="form-stack">
              <form className="form-stack" onSubmit={uploadMedia}>
                <h3>Upload governed media</h3>
                <label>Media file<input name="file" type="file" required accept={messageType === 'IMAGE_CAPTION' ? 'image/*' : messageType === 'VIDEO' ? 'video/*' : undefined} /></label>
                <label>Rights basis<select name="rightsBasis" defaultValue="CLIENT_SUPPLIED"><option value="OWNED">Owned</option><option value="LICENSED">Licensed</option><option value="CLIENT_SUPPLIED">Client supplied</option><option value="PUBLIC_DOMAIN">Public domain</option></select></label>
                <label>Rights evidence reference<input name="rightsReference" required minLength={3} maxLength={500} placeholder="Licence, client approval or ownership evidence reference" /></label>
                <label>Rights expiry (optional)<input name="rightsExpiresAt" type="datetime-local" /></label>
                <button className="secondary" disabled={Boolean(busy)}>Upload, scan and govern media</button>
              </form>
              <form className="form-stack" onSubmit={loadExistingAsset}>
                <label>Existing governed asset ID<input name="assetId" placeholder="Trusted asset UUID" /></label>
                <button className="secondary" disabled={Boolean(busy)}>Load existing media</button>
              </form>
              {asset ? (
                <dl className="definition-list">
                  <div><dt>Asset</dt><dd>{asset.id}</dd></div>
                  <div><dt>Scan</dt><dd>{asset.status}</dd></div>
                  <div><dt>Rights</dt><dd>{asset.rightsBasis} · {asset.rightsReference}</dd></div>
                  <div><dt>Expiry</dt><dd>{asset.rightsExpiresAt ? new Date(asset.rightsExpiresAt).toLocaleString() : 'No expiry recorded'}</dd></div>
                </dl>
              ) : null}
            </div>
          ) : <div className="empty-state"><strong>Text version selected</strong><p>No media upload is required.</p></div>}
        </div>
      ) : (
        <div className="alert alert-information">New message versions are locked after the campaign leaves AUDIENCE_VALIDATED. Existing version evidence remains readable below.</div>
      )}

      <div className="table-wrap" tabIndex={0} aria-label="Message version history">
        <table>
          <thead><tr><th>Version</th><th>Status</th><th>Type</th><th>Content hash</th><th>Media</th><th>Created</th><th></th></tr></thead>
          <tbody>{versions.map(item => (
            <tr key={item.id} className={selectedId === item.id ? 'selected-row' : undefined}>
              <td>v{item.version}</td><td><StatusBadge status={item.status} /></td><td>{item.type}</td>
              <td className="mono-small">{shortHash(item.contentHash)}</td>
              <td>{item.media ? item.media.mediaType + ' · ' + shortHash(item.media.sha256) : '—'}</td>
              <td>{new Date(item.createdAt).toLocaleString()}</td>
              <td><button type="button" className="text-button" onClick={() => { setSelectedId(item.id); setPreview(undefined); }}>Select</button></td>
            </tr>
          ))}</tbody>
        </table>
      </div>
      {versions.length === 0 ? <div className="empty-state"><strong>No message versions</strong><p>Create the first immutable version after audience validation.</p></div> : null}

      {selected ? (
        <div className="grid grid-2">
          <form className="form-stack" onSubmit={previewVersion}>
            <h3>Masked representative preview</h3>
            <p className="muted">Sensitive variable types are masked by the server. One optional value per line: name=value.</p>
            <textarea aria-label="Representative preview values" name="previewValues" rows={4} />
            <button className="secondary" disabled={Boolean(busy)}>Render masked preview</button>
            {preview ? (
              <div className="alert alert-information">
                <strong>Preview</strong>
                <p>{preview.body}</p>
                <small>{preview.characterCount} characters · {preview.byteCount} bytes{preview.missing.length ? ' · missing: ' + preview.missing.join(', ') : ''}{preview.usedFallbacks.length ? ' · fallbacks: ' + preview.usedFallbacks.join(', ') : ''}</small>
              </div>
            ) : null}
          </form>

          <div className="form-stack">
            <h3>Review handoff</h3>
            <dl className="definition-list">
              <div><dt>Selected</dt><dd>v{selected.version} · {selected.status}</dd></div>
              <div><dt>Creator</dt><dd>{selected.createdBy}</dd></div>
              <div><dt>Hash</dt><dd className="mono-small">{selected.contentHash}</dd></div>
              <div><dt>Campaign binding</dt><dd>{selectedIsCampaignMessage ? 'Current campaign message' : 'Not bound'}</dd></div>
            </dl>

            {campaign.status === 'AUDIENCE_VALIDATED' && selected.status === 'DRAFT' && canWrite ? (
              <button className="secondary" type="button" disabled={Boolean(busy)} onClick={() => void transitionMessage('SUBMIT_MESSAGE', selected)}>Submit selected version for review</button>
            ) : null}

            {campaign.status === 'MESSAGE_REVIEW_PENDING' && selectedIsCampaignMessage && selected.status === 'DRAFT' && canApprove ? (
              <>
                {makerIsCurrentOperator ? <small>Maker/checker separation: the creator cannot approve this version.</small> : null}
                <button className="primary" type="button" disabled={Boolean(busy) || makerIsCurrentOperator} onClick={() => void approveVersion()}>Approve selected version</button>
              </>
            ) : null}

            {campaign.status === 'MESSAGE_REVIEW_PENDING' && selectedIsCampaignMessage && selected.status === 'APPROVED' && canApprove ? (
              <button className="primary" type="button" disabled={Boolean(busy)} onClick={() => void transitionMessage('APPROVE_MESSAGE')}>Record campaign message approval</button>
            ) : null}

            {campaign.messageVersionId && !selectedIsCampaignMessage ? <small>The campaign is currently bound to a different message version. Select that version before approval actions.</small> : null}
          </div>
        </div>
      ) : null}
    </section>
  );
}
