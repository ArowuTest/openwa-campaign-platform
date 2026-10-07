'use client';

import { useCallback, useEffect, useRef, useState } from 'react';

import { StatusBadge } from './status-badge';
import { apiRequest, collectBoundedPages, type ListEnvelope } from '../lib/api';
import { filterGroupsEqual, type FilterGroup } from '../lib/audience-builder-model';

export type SavedSegment = {
  id: string;
  organisationId: string;
  name: string;
  description?: string;
  definition: FilterGroup;
  status: 'DRAFT' | 'ACTIVE' | 'ARCHIVED';
  version: number;
  createdAt: string;
  updatedAt: string;
};

type SegmentVersion = {
  segmentId: string;
  version: number;
  name: string;
  description?: string;
  definition: FilterGroup;
  status: string;
  changedBy: string;
  reason: string;
  createdAt: string;
};

type VersionComparison = {
  segmentId: string;
  fromVersion: number;
  toVersion: number;
  nameChanged: boolean;
  descriptionChanged: boolean;
  statusChanged: boolean;
  joinChanged: boolean;
  addedRules: Array<{ path: string }>;
  removedRules: Array<{ path: string }>;
  changedRules: Array<{ path: string }>;
  equivalent: boolean;
};

type Campaign = {
  id: string;
  name: string;
  organisationId: string;
  purposeId?: string;
  status: string;
  version: number;
  maximumUniqueRecipients: number;
  eligibleAudienceCount: number;
  audienceSnapshotId?: string;
};

type MaterialisationJob = {
  id: string;
  campaignId: string;
  segmentId?: string;
  definitionVersion: number;
  consentPolicyVersion: string;
  configurationVersion: string;
  status: 'PENDING' | 'RUNNING' | 'COMPLETED' | 'FAILED' | 'CANCELLED';
  processedCount: number;
  expectedCount: number;
  snapshotId?: string;
  attemptCount: number;
  failureCode?: string;
  failureReference?: string;
  version: number;
  requestedAt: string;
  startedAt?: string;
  completedAt?: string;
  cancellationReason?: string;
  progressPercent: number;
  updatedAt: string;
};

type AudienceSnapshot = {
  id: string;
  campaignId: string;
  segmentId?: string;
  definition: FilterGroup;
  definitionVersion: number;
  consentPolicyVersion: string;
  configurationVersion: string;
  eligibleCount: number;
  snapshotHash: string;
  createdAt: string;
};

type Props = {
  organisationId: string;
  currentDefinition?: FilterGroup;
  completedEstimateId?: string;
  loadedSegmentId: string;
  editorName: string;
  editorDescription: string;
  refreshToken: number;
  onOpenSegment: (segment: SavedSegment) => void;
};

export function SegmentSnapshotManager(props: Props) {
  // Organisation changes unmount all inventory, selection and action evidence together.
  return <OrganisationSegmentSnapshotManager key={props.organisationId} {...props} />;
}

function OrganisationSegmentSnapshotManager({
  organisationId,
  currentDefinition,
  completedEstimateId,
  loadedSegmentId,
  editorName,
  editorDescription,
  refreshToken,
  onOpenSegment
}: Props) {
  const [segments, setSegments] = useState<SavedSegment[]>([]);
  const [selectedSegmentId, setSelectedSegmentId] = useState('');
  const [versions, setVersions] = useState<SegmentVersion[]>([]);
  const [comparison, setComparison] = useState<VersionComparison>();
  const [fromVersion, setFromVersion] = useState('');
  const [toVersion, setToVersion] = useState('');
  const [versionReason, setVersionReason] = useState('Updated governed cohort definition');
  const [cloneName, setCloneName] = useState('');
  const [cloneReason, setCloneReason] = useState('Create an independently governed segment variant');
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [campaignId, setCampaignId] = useState('');
  const [jobs, setJobs] = useState<MaterialisationJob[]>([]);
  const [selectedJobId, setSelectedJobId] = useState('');
  const [snapshot, setSnapshot] = useState<AudienceSnapshot>();
  const [cancelReason, setCancelReason] = useState('Operator cancelled this audience materialisation');
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');

  const mounted = useRef(true);
  const selection = useRef({ segmentId: '', campaignId: '', jobId: '', generation: 0 });
  const requests = useRef({ segments: 0, campaigns: 0, versions: 0, jobs: 0, action: 0 });

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  const selectedSegment = segments.find((item) => item.id === selectedSegmentId && item.organisationId === organisationId);
  const selectedCampaign = campaigns.find((item) => item.id === campaignId && item.organisationId === organisationId);
  const selectedJob = jobs.find((item) => item.id === selectedJobId && item.campaignId === campaignId);
  const estimateMatchesSelectedSegment =
    Boolean(completedEstimateId) &&
    currentDefinition !== undefined &&
    selectedSegment !== undefined &&
    selectedSegment?.id === loadedSegmentId &&
    filterGroupsEqual(currentDefinition, selectedSegment?.definition);

  const invalidateAction = useCallback(() => {
    selection.current.generation += 1;
    requests.current.action += 1;
    setBusy('');
    setMessage('');
    setError('');
  }, []);

  const selectSegment = useCallback((id: string) => {
    if (selection.current.segmentId === id) return;
    selection.current.segmentId = id;
    requests.current.versions += 1;
    setSelectedSegmentId(id);
    setVersions([]);
    setFromVersion('');
    setToVersion('');
    setComparison(undefined);
    invalidateAction();
  }, [invalidateAction]);

  const selectCampaign = useCallback((id: string) => {
    if (selection.current.campaignId === id) return;
    selection.current.campaignId = id;
    selection.current.jobId = '';
    requests.current.jobs += 1;
    setCampaignId(id);
    setJobs([]);
    setSelectedJobId('');
    setSnapshot(undefined);
    invalidateAction();
  }, [invalidateAction]);

  const selectJob = useCallback((id: string) => {
    if (selection.current.jobId === id) return;
    selection.current.jobId = id;
    setSelectedJobId(id);
    setSnapshot(undefined);
    invalidateAction();
  }, [invalidateAction]);

  // A changed editor/evidence context invalidates responses, not an already submitted server mutation.
  useEffect(() => {
    const action = ++requests.current.action;
    const timer = window.setTimeout(() => {
      if (requests.current.action !== action) return;
      setBusy('');
      setMessage('');
      setError('');
    }, 0);
    return () => window.clearTimeout(timer);
  }, [currentDefinition, loadedSegmentId, editorName, editorDescription, completedEstimateId]);

  const loadSegments = useCallback(async () => {
    const request = ++requests.current.segments;
    const isCurrent = () => mounted.current && request === requests.current.segments;
    if (!organisationId) return false;
    try {
      const items = await collectBoundedPages<SavedSegment>(
        (cursor) => apiRequest<ListEnvelope<SavedSegment>>(
          '/v1/segments?organisationId=' + encodeURIComponent(organisationId) +
          '&limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
        ), 20
      );
      if (!isCurrent()) return false;
      const scoped = items.filter((item) => item.organisationId === organisationId);
      setSegments(scoped);
      const current = selection.current.segmentId;
      selectSegment(current && scoped.some((item) => item.id === current)
        ? current : scoped.find((item) => item.status === 'ACTIVE')?.id ?? '');
      return true;
    } catch (cause) {
      if (isCurrent()) setError(cause instanceof Error ? cause.message : 'Segments could not be loaded.');
      return false;
    }
  }, [organisationId, selectSegment]);

  const loadCampaigns = useCallback(async () => {
    const request = ++requests.current.campaigns;
    const isCurrent = () => mounted.current && request === requests.current.campaigns;
    if (!organisationId) return false;
    try {
      const items = await collectBoundedPages<Campaign>(
        (cursor) => apiRequest<ListEnvelope<Campaign>>(
          '/v1/campaigns?organisationId=' + encodeURIComponent(organisationId) +
          '&status=AUDIENCE_BUILDING&limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
        ), 20
      );
      if (!isCurrent()) return false;
      const eligible = items.filter((item) => item.organisationId === organisationId && item.status === 'AUDIENCE_BUILDING');
      setCampaigns(eligible);
      const current = selection.current.campaignId;
      selectCampaign(current && eligible.some((item) => item.id === current) ? current : eligible[0]?.id ?? '');
      return true;
    } catch (cause) {
      if (isCurrent()) setError(cause instanceof Error ? cause.message : 'Campaigns could not be loaded.');
      return false;
    }
  }, [organisationId, selectCampaign]);

  const loadVersions = useCallback(async (segmentId: string) => {
    const request = ++requests.current.versions;
    const action = requests.current.action;
    const isCurrent = () => mounted.current && request === requests.current.versions && selection.current.segmentId === segmentId;
    if (!segmentId) return false;
    try {
      const items = await collectBoundedPages<SegmentVersion>(
        (cursor) => apiRequest<ListEnvelope<SegmentVersion>>(
          '/v1/segments/' + segmentId + '/versions?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
        ), 20
      );
      if (!isCurrent()) return false;
      const scoped = items.filter((item) => item.segmentId === segmentId);
      setVersions(scoped);
      setFromVersion((current) => scoped.some((item) => String(item.version) === current)
        ? current : scoped.length ? String(scoped[scoped.length - 1].version) : '');
      setToVersion((current) => scoped.some((item) => String(item.version) === current)
        ? current : scoped.length >= 2 ? String(scoped[0].version) : '');
      // An inventory response does not own a newer mutation/comparison's busy or evidence state.
      if (requests.current.action === action) setComparison(undefined);
      return true;
    } catch (cause) {
      if (isCurrent()) setError(cause instanceof Error ? cause.message : 'Segment versions could not be loaded.');
      return false;
    }
  }, []);

  const loadJobs = useCallback(async (selectedCampaignId: string, autoSelect = true) => {
    const request = ++requests.current.jobs;
    const isCurrent = () => mounted.current && request === requests.current.jobs && selection.current.campaignId === selectedCampaignId;
    if (!selectedCampaignId) return false;
    try {
      const items = await collectBoundedPages<MaterialisationJob>(
        (cursor) => apiRequest<ListEnvelope<MaterialisationJob>>(
          '/v1/campaigns/' + selectedCampaignId + '/audience-materialisations?limit=100' +
          (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
        ), 20
      );
      if (!isCurrent()) return false;
      const scoped = items.filter((item) => item.campaignId === selectedCampaignId);
      setJobs((current) => scoped.map((item) => {
        const known = current.find((value) => value.id === item.id);
        return known && known.version > item.version ? known : item;
      }));
      const current = selection.current.jobId;
      if (autoSelect) selectJob(current && scoped.some((item) => item.id === current) ? current : scoped[0]?.id ?? '');
      return true;
    } catch (cause) {
      if (isCurrent()) setError(cause instanceof Error ? cause.message : 'Materialisation jobs could not be loaded.');
      return false;
    }
  }, [selectJob]);

  useEffect(() => {
    const timer = window.setTimeout(() => { void Promise.all([loadSegments(), loadCampaigns()]); }, 0);
    return () => window.clearTimeout(timer);
  }, [loadCampaigns, loadSegments, refreshToken]);

  useEffect(() => {
    const timer = window.setTimeout(() => { void loadVersions(selectedSegmentId); }, 0);
    return () => window.clearTimeout(timer);
  }, [loadVersions, selectedSegmentId]);

  useEffect(() => {
    const timer = window.setTimeout(() => { void loadJobs(campaignId); }, 0);
    return () => window.clearTimeout(timer);
  }, [campaignId, loadJobs]);

  useEffect(() => {
    if (!selectedJob || !['PENDING', 'RUNNING'].includes(selectedJob.status)) return;
    let cancelled = false;
    const isCurrent = () => !cancelled && mounted.current &&
      selection.current.campaignId === selectedJob.campaignId && selection.current.jobId === selectedJob.id;
    const timer = window.setInterval(() => {
      void apiRequest<MaterialisationJob>('/v1/audience-materialisations/' + selectedJob.id)
        .then((value) => {
          if (!isCurrent() || value.id !== selectedJob.id || value.campaignId !== selectedJob.campaignId) return;
          setJobs((current) => current.map((item) => item.id === value.id && value.version >= item.version ? value : item));
          // Snapshot loading has one guarded effect below; never launch detached nested work.
        })
        .catch((cause) => {
          if (isCurrent()) setError(cause instanceof Error ? cause.message : 'Materialisation progress could not be refreshed.');
        });
    }, 3000);
    return () => { cancelled = true; window.clearInterval(timer); };
  }, [selectedJob]);

  useEffect(() => {
    let cancelled = false;
    const snapshotId = selectedJob?.snapshotId;
    const timer = window.setTimeout(() => {
      if (!snapshotId) { setSnapshot(undefined); return; }
      void apiRequest<AudienceSnapshot>('/v1/audience-snapshots/' + snapshotId)
        .then((value) => {
          if (!cancelled && mounted.current && value.campaignId === campaignId && value.id === snapshotId) setSnapshot(value);
        })
        .catch((cause) => {
          if (!cancelled && mounted.current) setError(cause instanceof Error ? cause.message : 'Snapshot evidence could not be loaded.');
        });
    }, 0);
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [campaignId, selectedJobId, selectedJob?.snapshotId]);

  function beginAction(kind: string) {
    const token = { generation: selection.current.generation, request: ++requests.current.action };
    setBusy(kind);
    setError('');
    return token;
  }

  function isCurrentAction(token: { generation: number; request: number }) {
    return mounted.current && token.generation === selection.current.generation && token.request === requests.current.action;
  }

  async function saveNewVersion() {
    if (!selectedSegment || !currentDefinition || selectedSegment.id !== loadedSegmentId) return;
    const token = beginAction('version');
    try {
      const updated = await apiRequest<SavedSegment>('/v1/segments/' + selectedSegment.id, {
        method: 'PUT',
        body: JSON.stringify({
          name: editorName.trim() || selectedSegment.name,
          description: editorDescription.trim(),
          definition: currentDefinition,
          expectedVersion: selectedSegment.version,
          reason: versionReason.trim()
        })
      });
      if (!isCurrentAction(token)) return;
      setMessage('Saved segment version ' + updated.version + '.');
      if (!await loadSegments() || !isCurrentAction(token)) return;
      selectSegment(updated.id);
      await loadVersions(updated.id);
    } catch (cause) {
      if (!isCurrentAction(token)) return;
      setError(cause instanceof Error ? cause.message : 'Segment version could not be saved.');
    } finally {
      if (isCurrentAction(token)) setBusy('');
    }
  }

  async function cloneSegment() {
    if (!selectedSegment) return;
    const token = beginAction('clone');
    try {
      const cloned = await apiRequest<SavedSegment>('/v1/segments/' + selectedSegment.id + '/clone', {
        method: 'POST',
        body: JSON.stringify({
          name: cloneName.trim(),
          description: selectedSegment.description ?? '',
          reason: cloneReason.trim()
        })
      });
      if (!isCurrentAction(token)) return;
      setMessage('Created independent segment clone ' + cloned.name + '.');
      setCloneName('');
      if (!await loadSegments() || !isCurrentAction(token)) return;
      selectSegment(cloned.id);
    } catch (cause) {
      if (!isCurrentAction(token)) return;
      setError(cause instanceof Error ? cause.message : 'Segment could not be cloned.');
    } finally {
      if (isCurrentAction(token)) setBusy('');
    }
  }

  async function compareVersions() {
    if (!selectedSegmentId || !fromVersion || !toVersion || fromVersion === toVersion) return;
    const token = beginAction('compare');
    try {
      const value = await apiRequest<VersionComparison>(
        '/v1/segments/' + selectedSegmentId + '/compare?fromVersion=' +
        encodeURIComponent(fromVersion) + '&toVersion=' + encodeURIComponent(toVersion)
      );
      if (!isCurrentAction(token)) return;
      if (value.segmentId !== selectedSegmentId || String(value.fromVersion) !== fromVersion || String(value.toVersion) !== toVersion) {
        throw new Error('Comparison evidence does not match the selected versions.');
      }
      setComparison(value);
    } catch (cause) {
      if (!isCurrentAction(token)) return;
      setError(cause instanceof Error ? cause.message : 'Segment versions could not be compared.');
    } finally {
      if (isCurrentAction(token)) setBusy('');
    }
  }

  async function scheduleSnapshot() {
    if (!selectedCampaign || !selectedSegment || selectedSegment.status !== 'ACTIVE' || !completedEstimateId || !estimateMatchesSelectedSegment) return;
    const token = beginAction('materialise');
    try {
      const job = await apiRequest<MaterialisationJob>(
        '/v1/campaigns/' + selectedCampaign.id + '/audience-materialisations',
        {
          method: 'POST',
          body: JSON.stringify({
            segmentId: selectedSegment.id,
            definitionVersion: selectedSegment.version,
            estimateId: completedEstimateId
          })
        }
      );
      if (!isCurrentAction(token)) return;
      if (!await loadJobs(selectedCampaign.id, false) || !isCurrentAction(token)) return;
      selectJob(job.id);
      setMessage('Durable audience materialisation scheduled.');
    } catch (cause) {
      if (!isCurrentAction(token)) return;
      setError(cause instanceof Error ? cause.message : 'Audience materialisation could not be scheduled.');
    } finally {
      if (isCurrentAction(token)) setBusy('');
    }
  }

  async function cancelMaterialisation() {
    if (!selectedJob || !['PENDING', 'RUNNING'].includes(selectedJob.status)) return;
    const token = beginAction('cancel');
    try {
      const updated = await apiRequest<MaterialisationJob>(
        '/v1/audience-materialisations/' + selectedJob.id + '/cancel',
        {
          method: 'POST',
          body: JSON.stringify({ expectedVersion: selectedJob.version, reason: cancelReason.trim() })
        }
      );
      if (!isCurrentAction(token) || updated.campaignId !== campaignId || updated.id !== selectedJob.id) return;
      setJobs((current) => current.map((item) => item.id === updated.id && updated.version >= item.version ? updated : item));
      setMessage('Materialisation cancelled with durable evidence.');
    } catch (cause) {
      if (!isCurrentAction(token)) return;
      setError(cause instanceof Error ? cause.message : 'Materialisation could not be cancelled.');
    } finally {
      if (isCurrentAction(token)) setBusy('');
    }
  }

  if (!organisationId) {
    return (
      <section className="card">
        <span className="eyebrow">Saved segments and immutable snapshots</span>
        <h2>Select an organisation above</h2>
        <p className="muted">Segment lifecycle and campaign snapshot materialisation are organisation-scoped.</p>
      </section>
    );
  }

  return (
    <div className="workspace-stack">
      {error ? <div className="alert alert-danger" role="alert">{error}</div> : null}
      {message ? <div className="alert alert-success" role="status">{message}</div> : null}

      <section className="card">
        <div className="workspace-hero">
          <div>
            <span className="eyebrow">Reusable governed definitions</span>
            <h2>Saved segments</h2>
            <p className="muted">Open a saved definition into the editor, create immutable versions, clone variants and compare change evidence.</p>
          </div>
          <button type="button" className="secondary" onClick={() => void loadSegments()}>Refresh segments</button>
        </div>

        {segments.length === 0 ? (
          <div className="empty-state"><strong>No saved segments</strong><p>Validate and save the cohort above to create the first governed segment.</p></div>
        ) : (
          <div className="split-layout">
            <div className="table-wrap">
              <table>
                <thead><tr><th>Segment</th><th>Status</th><th>Version</th><th>Updated</th><th>Action</th></tr></thead>
                <tbody>
                  {segments.map((item) => (
                    <tr key={item.id}>
                      <td><strong>{item.name}</strong>{item.description ? <small>{item.description}</small> : null}</td>
                      <td><StatusBadge status={item.status} /></td>
                      <td>v{item.version}</td>
                      <td>{new Date(item.updatedAt).toLocaleString()}</td>
                      <td>
                        <button
                          type="button"
                          className="secondary compact-button"
                          onClick={() => {
                            selectSegment(item.id);
                            invalidateAction();
                            onOpenSegment(item);
                            setMessage('Loaded ' + item.name + ' v' + item.version + ' into the editor.');
                          }}
                        >
                          Open in editor
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <aside className="card nested-card">
              <label>Selected segment
                <select value={selectedSegmentId} onChange={(event) => selectSegment(event.target.value)}>
                  <option value="">Select segment</option>
                  {segments.map((item) => <option value={item.id} key={item.id}>{item.name} · v{item.version}</option>)}
                </select>
              </label>
              {selectedSegment ? (
                <>
                  <dl className="definition-list">
                    <div><dt>Status</dt><dd><StatusBadge status={selectedSegment.status} /></dd></div>
                    <div><dt>Current version</dt><dd>v{selectedSegment.version}</dd></div>
                    <div><dt>Updated</dt><dd>{new Date(selectedSegment.updatedAt).toLocaleString()}</dd></div>
                  </dl>

                  <h3>Save editor as new version</h3>
                  <label>Version reason<input value={versionReason} onChange={(event) => setVersionReason(event.target.value)} /></label>
                  <button
                    type="button"
                    className="primary"
                    disabled={busy === 'version' || selectedSegment.id !== loadedSegmentId || !currentDefinition || !versionReason.trim()}
                    onClick={() => void saveNewVersion()}
                  >
                    {busy === 'version' ? 'Saving…' : 'Save new version'}
                  </button>
                  {selectedSegment.id !== loadedSegmentId ? <small>Open this segment in the editor before creating a new version.</small> : null}

                  <h3>Clone independently</h3>
                  <label>Clone name<input value={cloneName} onChange={(event) => setCloneName(event.target.value)} placeholder={selectedSegment.name + ' variant'} /></label>
                  <label>Reason<input value={cloneReason} onChange={(event) => setCloneReason(event.target.value)} /></label>
                  <button type="button" className="secondary" disabled={busy === 'clone' || !cloneName.trim() || cloneReason.trim().length < 8} onClick={() => void cloneSegment()}>
                    {busy === 'clone' ? 'Cloning…' : 'Clone segment'}
                  </button>
                </>
              ) : null}
            </aside>
          </div>
        )}

        {selectedSegment && versions.length ? (
          <div className="segment-version-panel">
            <h3>Immutable version history</h3>
            <div className="table-wrap">
              <table>
                <thead><tr><th>Version</th><th>Status</th><th>Reason</th><th>Changed</th></tr></thead>
                <tbody>
                  {versions.map((item) => (
                    <tr key={item.version}>
                      <td>v{item.version}</td><td><StatusBadge status={item.status} /></td><td>{item.reason}</td><td>{new Date(item.createdAt).toLocaleString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {versions.length >= 2 ? (
              <div className="comparison-controls">
                <label>From<select value={fromVersion} onChange={(event) => { setFromVersion(event.target.value); setComparison(undefined); invalidateAction(); }}>{versions.map((item) => <option key={item.version} value={item.version}>v{item.version}</option>)}</select></label>
                <label>To<select value={toVersion} onChange={(event) => { setToVersion(event.target.value); setComparison(undefined); invalidateAction(); }}>{versions.map((item) => <option key={item.version} value={item.version}>v{item.version}</option>)}</select></label>
                <button type="button" className="secondary" disabled={busy === 'compare' || !fromVersion || !toVersion || fromVersion === toVersion} onClick={() => void compareVersions()}>
                  Compare versions
                </button>
              </div>
            ) : null}
            {comparison ? (
              <div className={comparison.equivalent ? 'alert alert-success' : 'alert alert-information'}>
                {comparison.equivalent
                  ? 'The selected versions are semantically equivalent.'
                  : `Changes: ${comparison.addedRules.length} added · ${comparison.removedRules.length} removed · ${comparison.changedRules.length} changed rules`}
                {comparison.joinChanged ? ' · group join changed' : ''}
                {comparison.nameChanged ? ' · name changed' : ''}
                {comparison.descriptionChanged ? ' · description changed' : ''}
              </div>
            ) : null}
          </div>
        ) : null}
      </section>

      <section className="card">
        <div className="workspace-hero">
          <div>
            <span className="eyebrow">Immutable campaign audience</span>
            <h2>Snapshot materialisation</h2>
            <p className="muted">Freeze an active saved segment into a restart-safe campaign snapshot with authoritative consent/configuration evidence.</p>
          </div>
          {selectedJob ? <StatusBadge status={selectedJob.status} /> : null}
        </div>

        <div className="grid grid-2">
          <label>Audience-building campaign
            <select value={campaignId} onChange={(event) => selectCampaign(event.target.value)}>
              <option value="">Select campaign</option>
              {campaigns.map((item) => <option value={item.id} key={item.id}>{item.name} · max {item.maximumUniqueRecipients.toLocaleString()}</option>)}
            </select>
          </label>
          <label>Saved active segment
            <select value={selectedSegmentId} onChange={(event) => selectSegment(event.target.value)}>
              <option value="">Select segment</option>
              {segments.filter((item) => item.status === 'ACTIVE').map((item) => <option value={item.id} key={item.id}>{item.name} · v{item.version}</option>)}
            </select>
          </label>
        </div>

        {selectedCampaign && selectedSegment ? (
          <div className="alert alert-information">
            Materialisation will use saved <strong>{selectedSegment.name} v{selectedSegment.version}</strong> for
            <strong> {selectedCampaign.name}</strong>. A completed durable estimate for this exact saved definition is required; unsaved editor changes cannot be materialised.
          </div>
        ) : null}

        {selectedSegment && !estimateMatchesSelectedSegment ? (
          <div className="alert alert-warning" role="status">
            Open this saved segment in the editor, validate it, and complete a governed audience estimate before scheduling its snapshot.
          </div>
        ) : null}

        <div className="button-row">
          <button type="button" className="primary" disabled={busy === 'materialise' || !selectedCampaign || !selectedSegment || selectedSegment.status !== 'ACTIVE' || !estimateMatchesSelectedSegment} onClick={() => void scheduleSnapshot()}>
            {busy === 'materialise' ? 'Scheduling…' : 'Schedule durable snapshot'}
          </button>
          <button type="button" className="secondary" disabled={!selectedCampaign} onClick={() => selectedCampaign && void loadJobs(selectedCampaign.id)}>Refresh jobs</button>
        </div>

        {jobs.length ? (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Requested</th><th>Status</th><th>Progress</th><th>Attempt</th><th>Snapshot</th><th>Action</th></tr></thead>
              <tbody>
                {jobs.map((item) => (
                  <tr key={item.id}>
                    <td>{new Date(item.requestedAt).toLocaleString()}</td>
                    <td><StatusBadge status={item.status} />{item.failureCode ? <small>{item.failureCode} · ref {item.failureReference}</small> : null}</td>
                    <td>{item.processedCount.toLocaleString()} / {item.expectedCount.toLocaleString()}<small>{Math.round(item.progressPercent)}%</small></td>
                    <td>{item.attemptCount}</td>
                    <td>{item.snapshotId ? <span className="mono-small">{item.snapshotId}</span> : '—'}</td>
                    <td><button type="button" className="secondary compact-button" onClick={() => selectJob(item.id)}>Review</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : selectedCampaign ? <div className="empty-state"><strong>No materialisation jobs</strong><p>Schedule the first immutable snapshot for this campaign.</p></div> : null}

        {selectedJob && ['PENDING', 'RUNNING'].includes(selectedJob.status) ? (
          <div className="materialisation-progress">
            <div className="progress-track"><span style={{ width: Math.min(100, Math.max(0, selectedJob.progressPercent)) + '%' }} /></div>
            <div className="progress-meta"><strong>{Math.round(selectedJob.progressPercent)}% durable</strong><span>{selectedJob.processedCount.toLocaleString()} / {selectedJob.expectedCount.toLocaleString()}</span></div>
            <label>Cancellation reason<input value={cancelReason} onChange={(event) => setCancelReason(event.target.value)} /></label>
            <button type="button" className="danger-ghost" disabled={busy === 'cancel' || !cancelReason.trim()} onClick={() => void cancelMaterialisation()}>
              {busy === 'cancel' ? 'Cancelling…' : 'Cancel materialisation'}
            </button>
          </div>
        ) : null}

        {snapshot ? (
          <div className="snapshot-evidence">
            <div className="section-heading">
              <div><span className="eyebrow">Immutable evidence</span><h3>Audience snapshot</h3></div>
              <StatusBadge status="FROZEN" />
            </div>
            <div className="grid grid-4">
              <div className="metric-tile success"><span>Eligible recipients</span><strong>{snapshot.eligibleCount.toLocaleString()}</strong></div>
              <div className="metric-tile"><span>Definition version</span><strong>v{snapshot.definitionVersion}</strong></div>
              <div className="metric-tile"><span>Created</span><strong>{new Date(snapshot.createdAt).toLocaleString()}</strong></div>
              <div className="metric-tile"><span>Snapshot ID</span><strong className="mono-small">{snapshot.id}</strong></div>
            </div>
            <dl className="definition-list">
              <div><dt>Snapshot hash</dt><dd className="mono-small">{snapshot.snapshotHash}</dd></div>
              <div><dt>Consent evidence</dt><dd>{snapshot.consentPolicyVersion}</dd></div>
              <div><dt>Configuration evidence</dt><dd>{snapshot.configurationVersion}</dd></div>
              <div><dt>Saved segment</dt><dd>{snapshot.segmentId || 'Inline definition'}</dd></div>
            </dl>
            <div className="alert alert-success">
              Membership is immutable. Recipient identities are intentionally not rendered in this review surface.
            </div>
          </div>
        ) : null}
      </section>
    </div>
  );
}
