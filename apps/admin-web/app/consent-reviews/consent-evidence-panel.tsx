'use client';

import { useEffect, useRef, useState } from 'react';

import { useAuth } from '../../components/auth-provider';
import { StatusBadge } from '../../components/status-badge';
import { apiRequest } from '../../lib/api';
import { hasPermission } from '../../lib/session';

type ExternalEvidence = { reference: string; sha256Checksum: string };

type TrustedAsset = {
  id: string;
  purpose: string;
  originalFilename: string;
  mediaType: string;
  size: number;
  sha256: string;
  status: string;
  version: number;
  rightsBasis?: string;
  rightsReference?: string;
  rightsExpiresAt?: string | null;
  createdAt: string;
  updatedAt: string;
};

type AssetInspection = {
  loading: boolean;
  asset?: TrustedAsset;
  error?: string;
};

const visibleBatch = 20;
const maximumReferences = 100;
const sha256Pattern = /^[a-fA-F0-9]{64}$/;

function formatDate(value?: string) {
  if (!value) return 'Not recorded';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? 'Invalid timestamp' : date.toLocaleString();
}

function formatSize(value: number): string {
  return Number.isFinite(value) && value >= 0 ? value.toLocaleString() + ' bytes' : 'Invalid file size';
}

function safeReference(value: string) {
  return value.trim();
}

function inspectionWarning(asset: TrustedAsset) {
  if (asset.purpose !== 'CONSENT_EVIDENCE') return 'Asset purpose does not match consent evidence.';
  if (!sha256Pattern.test(asset.sha256)) return 'Invalid SHA-256 metadata. Do not rely on this asset.';
  if (asset.status === 'REVOKED') return 'Revoked evidence cannot be used to establish consent.';
  if (asset.status === 'INFECTED') return 'Malware-infected evidence cannot be used to establish consent.';
  if (asset.status === 'SCANNING') return 'Scanning is incomplete. Do not treat this asset as clean.';
  if (asset.status === 'SCAN_FAILED') return 'Malware scanning failed. Do not treat this asset as clean.';
  if (asset.status !== 'CLEAN') return 'Unrecognised asset scan status. Do not rely on this asset.';
  return 'Clean scan status alone does not approve consent. Review legal basis, exact wording and provenance independently.';
}

export function ConsentEvidencePanel({
  reviewId, version, assetIds = [], externalReferences = []
}: {
  reviewId: string;
  version: number;
  assetIds?: string[];
  externalReferences?: ExternalEvidence[];
}) {
  const { session } = useAuth();
  const canRead = hasPermission(session?.permissions, 'consent.read');
  const generation = JSON.stringify([
    reviewId, version, session?.id, session?.sessionId,
    [...(session?.permissions ?? [])].sort()
  ]);
  if (!canRead) return <p role="status">Consent evidence requires consent.read permission.</p>;
  return (
    <ConsentEvidenceGeneration
      key={generation}
      assetIds={assetIds}
      externalReferences={externalReferences}
    />
  );
}

function ConsentEvidenceGeneration({
  assetIds, externalReferences
}: {
  assetIds: string[];
  externalReferences: ExternalEvidence[];
}) {
  const mounted = useRef(false);
  const requestNumbers = useRef(new Map<string, number>());
  const [visibleCount, setVisibleCount] = useState(visibleBatch);
  const [inspections, setInspections] = useState<Record<string, AssetInspection>>({});

  useEffect(() => {
    mounted.current = true;
    const numbers = requestNumbers.current;
    return () => {
      mounted.current = false;
      numbers.clear();
    };
  }, []);

  // The server's review record supplies only references. No automatic document
  // retrieval or uncontrolled file fetch is permitted by this workbench.
  const references = Array.from(new Set(assetIds.map((item) => String(item).trim()).filter(Boolean)));
  const bounded = references.slice(0, maximumReferences);
  const shown = bounded.slice(0, visibleCount);
  const external = externalReferences.slice(0, maximumReferences);

  async function inspect(id: string) {
    if (!shown.includes(id) || inspections[id]?.loading) return;
    const request = (requestNumbers.current.get(id) ?? 0) + 1;
    requestNumbers.current.set(id, request);
    setInspections((previous) => ({ ...previous, [id]: { loading: true } }));
    try {
      const asset = await apiRequest<TrustedAsset>('/v1/assets/' + encodeURIComponent(id));
      if (!mounted.current || requestNumbers.current.get(id) !== request) return;
      if (!asset || asset.id !== id || typeof asset.sha256 !== 'string' ||
        typeof asset.purpose !== 'string' || typeof asset.status !== 'string') {
        throw new Error('Unreliable asset metadata or reference mismatch; do not rely on this evidence.');
      }
      setInspections((previous) => ({ ...previous, [id]: { loading: false, asset } }));
    } catch (error) {
      if (!mounted.current || requestNumbers.current.get(id) !== request) return;
      setInspections((previous) => ({
        ...previous,
        [id]: {
          loading: false,
          error: error instanceof Error ? error.message : 'Asset metadata could not be loaded.'
        }
      }));
    }
  }

  return (
    <section className="card form-stack" aria-label="Consent evidence verification">
      <div className="section-heading">
        <div>
          <h3>Consent evidence verification</h3>
          <p className="muted">Inspect existing server-owned asset metadata without downloading files. Evidence existence and malware status are not approval or lawful-consent proof.</p>
        </div>
        <span className="pill">{references.length + externalReferences.length} referenced</span>
      </div>
      <h4>Trusted evidence assets</h4>
      {!references.length ? <p>No trusted asset references recorded for this review.</p> : null}
      {references.length > maximumReferences
        ? <p role="status">Only the first 100 asset references can be inspected in this bounded view. Additional evidence exists; use an authorised evidence process before making a decision.</p>
        : null}
      {shown.map((id) => {
        const state = inspections[id];
        const asset = state?.asset;
        const acceptableMetadata = Boolean(asset &&
          asset.purpose === 'CONSENT_EVIDENCE' && asset.status === 'CLEAN' &&
          sha256Pattern.test(asset.sha256));
        return (
          <article key={id} className="card form-stack" aria-label={'Asset reference ' + id}>
            <div className="section-heading">
              <div><strong>Asset reference</strong><p><code>{id}</code></p></div>
              <button type="button" onClick={() => void inspect(id)} disabled={Boolean(state?.loading)}>
                {state?.loading ? 'Loading metadata…' : 'Inspect metadata for ' + id}
              </button>
            </div>
            {state?.error ? <p role="alert">{state.error}</p> : null}
            {asset && !acceptableMetadata ? (
              <p role="alert">Evidence metadata blocked. {inspectionWarning(asset)} No asset details can be relied on for this consent review.</p>
            ) : null}
            {asset && acceptableMetadata ? (
              <div className="form-stack">
                <dl className="definition-list">
                  <div><dt>Filename</dt><dd>{asset.originalFilename}</dd></div>
                  <div><dt>Purpose</dt><dd>{asset.purpose}</dd></div>
                  <div><dt>Scan status</dt><dd><StatusBadge status={asset.status} /></dd></div>
                  <div><dt>Media type</dt><dd>{asset.mediaType}</dd></div>
                  <div><dt>File size</dt><dd>{formatSize(asset.size)}</dd></div>
                  <div><dt>SHA-256 digest</dt><dd><code style={{ overflowWrap: 'anywhere' }}>{asset.sha256}</code></dd></div>
                  <div><dt>Asset version</dt><dd>{asset.version}</dd></div>
                  <div><dt>Last updated</dt><dd>{formatDate(asset.updatedAt)}</dd></div>
                </dl>
                <p role="status">{inspectionWarning(asset)}</p>
              </div>
            ) : null}
          </article>
        );
      })}
      {visibleCount < bounded.length ? (
        <button type="button" onClick={() => setVisibleCount((count) => Math.min(count + visibleBatch, bounded.length))}>
          Show 20 more asset references
        </button>
      ) : null}
      <h4>External evidence references</h4>
      {externalReferences.length > maximumReferences ? (
        <p role="status">Only the first 100 external references are displayed. The remainder must be reconciled through the approved evidence process.</p>
      ) : null}
      {!externalReferences.length ? <p>No external evidence references recorded.</p> : null}
      {external.map((item, index) => {
        const reference = safeReference(String(item.reference ?? ''));
        const digest = String(item.sha256Checksum ?? '');
        return (
          <article className="card form-stack" key={index}>
            <dl className="definition-list">
              <div><dt>External reference</dt><dd style={{ overflowWrap: 'anywhere' }}>{reference || 'Missing reference'}</dd></div>
              <div><dt>Claimed SHA-256</dt><dd><code style={{ overflowWrap: 'anywhere' }}>{digest || 'Missing digest'}</code></dd></div>
            </dl>
            {reference && sha256Pattern.test(digest)
              ? <p>External checksum has not been independently validated. No remote content is downloaded or opened.</p>
              : <p role="alert">Invalid external SHA-256 reference. The external evidence cannot be considered verified.</p>}
          </article>
        );
      })}
      <p className="muted">File content preview and controlled download are not available through the current asset metadata API. Reviewers must obtain document content through an independently authorised evidence process.</p>
    </section>
  );
}
