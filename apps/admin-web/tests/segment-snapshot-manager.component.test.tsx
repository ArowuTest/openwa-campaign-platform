import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { SegmentSnapshotManager } from '../components/segment-snapshot-manager';
import { campaign, deferred, definition, envelope, job, mockHTTP, segment, settle, snapshot, versions } from './audience-component-test-helpers';

afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

const props = {
  organisationId: 'org-a', currentDefinition: definition, completedEstimateId: 'estimate-a',
  loadedSegmentId: 'segment-a', editorName: 'Editor', editorDescription: '', refreshToken: 0,
  onOpenSegment: () => {}
};

function inventory(path: string) {
  if (path.startsWith('/v1/segments?')) return envelope([segment('segment-a'), segment('segment-b')]);
  if (path.startsWith('/v1/campaigns?')) return envelope([campaign('campaign-a'), campaign('campaign-b')]);
  if (path.includes('/versions?')) return envelope(versions(path.split('/')[3]));
  if (path.includes('/audience-materialisations?')) return envelope([]);
  throw new Error('Unexpected HTTP request: ' + path);
}

test('late organisation inventories cannot replace the current organisation segments or campaigns', async () => {
  const oldSegments = deferred<unknown>();
  const oldCampaigns = deferred<unknown>();
  const calls = mockHTTP((path) => {
    if (path.startsWith('/v1/segments?organisationId=org-a')) return oldSegments.promise;
    if (path.startsWith('/v1/campaigns?organisationId=org-a')) return oldCampaigns.promise;
    if (path.startsWith('/v1/segments?organisationId=org-b')) return envelope([segment('segment-b', 'org-b')]);
    if (path.startsWith('/v1/campaigns?organisationId=org-b')) return envelope([campaign('campaign-b', 'org-b')]);
    return inventory(path);
  });
  const view = render(<SegmentSnapshotManager {...props} />);
  await waitFor(() => expect(calls.filter((call) => call.path.includes('organisationId=org-a'))).toHaveLength(2));
  view.rerender(<SegmentSnapshotManager {...props} organisationId="org-b" />);
  await screen.findByRole('option', { name: 'campaign-b campaign · max 500' });
  await settle(oldSegments, envelope([segment('segment-a')]));
  await settle(oldCampaigns, envelope([campaign('campaign-a')]));
  expect(screen.queryByRole('option', { name: 'campaign-a campaign · max 500' })).toBeNull();
  expect(screen.queryByText('segment-a segment')).toBeNull();
  expect((screen.getByLabelText('Audience-building campaign') as HTMLSelectElement).value).toBe('campaign-b');
});

test('late version history cannot replace a newly selected segment history', async () => {
  const old = deferred<unknown>();
  const calls = mockHTTP((path) => path.includes('/segment-a/versions?') ? old.promise : inventory(path));
  render(<SegmentSnapshotManager {...props} />);
  await screen.findAllByRole('option', { name: 'segment-b segment · v3' });
  await waitFor(() => expect(calls.some((call) => call.path.includes('/segment-a/versions?'))).toBe(true));
  fireEvent.change(screen.getByLabelText('Selected segment'), { target: { value: 'segment-b' } });
  await screen.findByText('segment-b evidence 3');
  await settle(old, envelope(versions('segment-a')));
  expect(screen.queryByText('segment-a evidence 3')).toBeNull();
  expect(screen.getByText('segment-b evidence 3')).toBeDefined();
});

test('campaign changes immediately remove cancellable old jobs and ignore late job inventories', async () => {
  const oldRefresh = deferred<unknown>();
  let reads = 0;
  const calls = mockHTTP((path) => {
    if (path.includes('/campaign-a/audience-materialisations?')) {
      reads += 1;
      return reads === 1 ? envelope([job('job-a')]) : oldRefresh.promise;
    }
    return inventory(path);
  });
  render(<SegmentSnapshotManager {...props} />);
  await screen.findByRole('button', { name: 'Cancel materialisation' });
  fireEvent.click(screen.getByRole('button', { name: 'Refresh jobs' }));
  await waitFor(() => expect(reads).toBe(2));
  fireEvent.change(screen.getByLabelText('Audience-building campaign'), { target: { value: 'campaign-b' } });
  expect(screen.queryByRole('button', { name: 'Cancel materialisation' })).toBeNull();
  await screen.findByText('No materialisation jobs');
  await settle(oldRefresh, envelope([job('job-a')]));
  expect(screen.queryByRole('button', { name: 'Cancel materialisation' })).toBeNull();
  expect(calls.some((call) => call.path.endsWith('/job-a/cancel'))).toBe(false);
});

test('comparison evidence clears on version selection and cannot return from a delayed old comparison', async () => {
  const old = deferred<unknown>();
  let reads = 0;
  const comparison = { segmentId: 'segment-a', fromVersion: 1, toVersion: 3, nameChanged: false,
    descriptionChanged: false, statusChanged: false, joinChanged: false,
    addedRules: [{ path: 'root' }], removedRules: [], changedRules: [], equivalent: false };
  mockHTTP((path) => {
    if (path.includes('/compare?')) { reads += 1; return reads === 1 ? comparison : old.promise; }
    return inventory(path);
  });
  render(<SegmentSnapshotManager {...props} />);
  const button = await screen.findByRole('button', { name: 'Compare versions' });
  fireEvent.click(button);
  await screen.findByText('Changes: 1 added · 0 removed · 0 changed rules');
  fireEvent.change(screen.getByLabelText('From'), { target: { value: '2' } });
  expect(screen.queryByText(/Changes:/)).toBeNull();
  fireEvent.click(button);
  await waitFor(() => expect(reads).toBe(2));
  fireEvent.change(screen.getByLabelText('To'), { target: { value: '1' } });
  await settle(old, comparison);
  expect(screen.queryByText(/Changes:/)).toBeNull();
});

test('comparison evidence clears on segment selection', async () => {
  mockHTTP((path) => path.includes('/compare?') ? {
    segmentId: 'segment-a', fromVersion: 1, toVersion: 3, nameChanged: false,
    descriptionChanged: false, statusChanged: false, joinChanged: false,
    addedRules: [], removedRules: [], changedRules: [], equivalent: true
  } : inventory(path));
  render(<SegmentSnapshotManager {...props} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Compare versions' }));
  await screen.findByText('The selected versions are semantically equivalent.');
  fireEvent.change(screen.getByLabelText('Selected segment'), { target: { value: 'segment-b' } });
  expect(screen.queryByText('The selected versions are semantically equivalent.')).toBeNull();
});

test('an old polling response cannot expose its snapshot in a different campaign', async () => {
  let tick: () => void = () => {};
  const originalSetInterval = window.setInterval.bind(window);
  vi.spyOn(window, 'setInterval').mockImplementation((callback, delay) => {
    if (delay !== 3000) return originalSetInterval(callback, delay) as unknown as ReturnType<typeof window.setInterval>;
    tick = callback as () => void; return -1 as unknown as ReturnType<typeof window.setInterval>;
  });
  const progress = deferred<unknown>();
  const evidence = deferred<unknown>();
  const calls = mockHTTP((path) => {
    if (path.includes('/campaign-a/audience-materialisations?')) return envelope([job('job-a')]);
    if (path === '/v1/audience-materialisations/job-a') return progress.promise;
    if (path === '/v1/audience-snapshots/job-a-snapshot') return evidence.promise;
    return inventory(path);
  });
  render(<SegmentSnapshotManager {...props} />);
  await screen.findByRole('button', { name: 'Cancel materialisation' });
  await act(async () => { tick(); });
  expect(calls.some((call) => call.path === '/v1/audience-materialisations/job-a')).toBe(true);
  fireEvent.change(screen.getByLabelText('Audience-building campaign'), { target: { value: 'campaign-b' } });
  await screen.findByText('No materialisation jobs');
  await settle(progress, job('job-a', 'campaign-a', 'COMPLETED'));
  // If an obsolete poll incorrectly starts nested snapshot work, resolve it too.
  await settle(evidence, snapshot('job-a-snapshot'));
  expect(screen.queryByText('job-a-snapshot-hash')).toBeNull();
});

test('a Go-style child-only saved definition matches its normalized editor definition for scheduling', async () => {
  const wire = JSON.parse('{"join":"OR","children":[{"join":"AND","rules":[{"definitionCode":"REPORTED_AGE","operator":"greater_than","values":[18]}]}]}');
  mockHTTP((path) => path.startsWith('/v1/segments?') ? envelope([{ ...segment('segment-a'), definition: wire }]) : inventory(path));
  render(<SegmentSnapshotManager {...props} currentDefinition={{ ...wire, rules: [], children: [{ ...wire.children[0], children: [] }] }} />);
  await screen.findByRole('option', { name: 'campaign-a campaign · max 500' });
  expect((screen.getByRole('button', { name: 'Schedule durable snapshot' }) as HTMLButtonElement).disabled).toBe(false);
});

test('a delayed comparison cannot return after the selected version pair changes', async () => {
  const old = deferred<unknown>();
  let started = false;
  mockHTTP((path) => {
    if (path.includes('/compare?')) { started = true; return old.promise; }
    return inventory(path);
  });
  render(<SegmentSnapshotManager {...props} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Compare versions' }));
  await waitFor(() => expect(started).toBe(true));
  fireEvent.change(screen.getByLabelText('From'), { target: { value: '2' } });
  await settle(old, { segmentId: 'segment-a', fromVersion: 1, toVersion: 3, nameChanged: false,
    descriptionChanged: false, statusChanged: false, joinChanged: false,
    addedRules: [], removedRules: [], changedRules: [], equivalent: true });
  expect(screen.queryByText('The selected versions are semantically equivalent.')).toBeNull();
});

test('late old-campaign job inventories cannot populate the new campaign', async () => {
  const old = deferred<unknown>();
  const calls = mockHTTP((path) => path.includes('/campaign-a/audience-materialisations?') ? old.promise : inventory(path));
  render(<SegmentSnapshotManager {...props} />);
  await screen.findByRole('option', { name: 'campaign-b campaign · max 500' });
  await waitFor(() => expect(calls.some((call) => call.path.includes('/campaign-a/audience-materialisations?'))).toBe(true));
  fireEvent.change(screen.getByLabelText('Audience-building campaign'), { target: { value: 'campaign-b' } });
  await screen.findByText('No materialisation jobs');
  await settle(old, envelope([job('job-a')]));
  expect(screen.queryByRole('button', { name: 'Cancel materialisation' })).toBeNull();
});

test('a late version inventory cannot clear a newer clone action busy state', async () => {
  const history = deferred<unknown>();
  const clone = deferred<unknown>();
  let cloneStarted = false;
  mockHTTP((path, init) => {
    if (path.includes('/segment-a/versions?')) return history.promise;
    if (path.endsWith('/segment-a/clone') && init.method === 'POST') { cloneStarted = true; return clone.promise; }
    return inventory(path);
  });
  render(<SegmentSnapshotManager {...props} />);
  fireEvent.change(await screen.findByLabelText('Clone name'), { target: { value: 'New variant' } });
  fireEvent.click(screen.getByRole('button', { name: 'Clone segment' }));
  await waitFor(() => expect(cloneStarted).toBe(true));
  await settle(history, envelope(versions('segment-a')));
  expect(screen.getByRole('button', { name: 'Cloning…' })).toBeDefined();
  await settle(clone, { ...segment('segment-c'), name: 'New variant' });
});

test('an old scheduling reload cannot select its job or clear a newer cancellation busy state', async () => {
  const reload = deferred<unknown>();
  const cancellation = deferred<unknown>();
  let jobReads = 0;
  let cancelStarted = false;
  mockHTTP((path, init) => {
    if (path.includes('/campaign-a/audience-materialisations?')) {
      jobReads += 1;
      return jobReads === 1 ? envelope([job('job-a')]) : reload.promise;
    }
    if (path === '/v1/campaigns/campaign-a/audience-materialisations' && init.method === 'POST') return job('new-job');
    if (path === '/v1/audience-materialisations/job-a/cancel') { cancelStarted = true; return cancellation.promise; }
    return inventory(path);
  });
  render(<SegmentSnapshotManager {...props} />);
  await screen.findByRole('button', { name: 'Cancel materialisation' });
  await screen.findByText('segment-a evidence 3');
  fireEvent.click(screen.getByRole('button', { name: 'Schedule durable snapshot' }));
  await waitFor(() => expect(jobReads).toBe(2));
  fireEvent.click(screen.getByRole('button', { name: 'Cancel materialisation' }));
  await waitFor(() => expect(cancelStarted).toBe(true));
  await settle(reload, envelope([job('new-job'), job('job-a')]));
  expect(screen.getByRole('button', { name: 'Cancelling…' })).toBeDefined();
  await settle(cancellation, { ...job('job-a'), status: 'CANCELLED' });
  await screen.findByText('Materialisation cancelled with durable evidence.');
});

test('a late pending job inventory cannot restore cancellation after newer terminal evidence', async () => {
  const old = deferred<unknown>();
  let reads = 0;
  mockHTTP((path, init) => {
    if (path.includes('/campaign-a/audience-materialisations?')) {
      reads += 1; return reads === 1 ? envelope([job('job-a')]) : old.promise;
    }
    if (path === '/v1/audience-materialisations/job-a/cancel' && init.method === 'POST') return { ...job('job-a'), status: 'CANCELLED', version: 2 };
    return inventory(path);
  });
  render(<SegmentSnapshotManager {...props} />);
  await screen.findByRole('button', { name: 'Cancel materialisation' });
  fireEvent.click(screen.getByRole('button', { name: 'Refresh jobs' }));
  await waitFor(() => expect(reads).toBe(2));
  fireEvent.click(screen.getByRole('button', { name: 'Cancel materialisation' }));
  await screen.findByText('Materialisation cancelled with durable evidence.');
  await settle(old, envelope([job('job-a')]));
  expect(screen.queryByRole('button', { name: 'Cancel materialisation' })).toBeNull();
});

test('current scheduling and cancellation display durable evidence with the exact governed payload', async () => {
  let reads = 0;
  let scheduleBody: unknown;
  mockHTTP((path, init) => {
    if (path.includes('/campaign-a/audience-materialisations?')) {
      reads += 1; return envelope(reads === 1 ? [] : [job('job-a')]);
    }
    if (path === '/v1/campaigns/campaign-a/audience-materialisations' && init.method === 'POST') {
      scheduleBody = JSON.parse(String(init.body)); return job('job-a');
    }
    if (path === '/v1/audience-materialisations/job-a/cancel' && init.method === 'POST') return { ...job('job-a'), status: 'CANCELLED', version: 2 };
    return inventory(path);
  });
  render(<SegmentSnapshotManager {...props} />);
  await screen.findByText('segment-a evidence 3');
  await screen.findByText('No materialisation jobs');
  fireEvent.click(screen.getByRole('button', { name: 'Schedule durable snapshot' }));
  await screen.findByRole('button', { name: 'Cancel materialisation' });
  expect(scheduleBody).toEqual({ segmentId: 'segment-a', definitionVersion: 3, estimateId: 'estimate-a' });
  fireEvent.click(screen.getByRole('button', { name: 'Cancel materialisation' }));
  await screen.findByText('Materialisation cancelled with durable evidence.');
  expect(screen.queryByRole('button', { name: 'Cancel materialisation' })).toBeNull();
});

test('a current scheduling reload can finish while existing-job polling continues', async () => {
  let tick: () => void = () => {};
  const originalSetInterval = window.setInterval.bind(window);
  vi.spyOn(window, 'setInterval').mockImplementation((callback, delay) => {
    if (delay !== 3000) return originalSetInterval(callback, delay) as unknown as ReturnType<typeof window.setInterval>;
    tick = callback as () => void; return -1 as unknown as ReturnType<typeof window.setInterval>;
  });
  const reload = deferred<unknown>();
  let reads = 0;
  const newJob = { ...job('new-job'), progressPercent: 77, processedCount: 77 };
  mockHTTP((path, init) => {
    if (path.includes('/campaign-a/audience-materialisations?')) {
      reads += 1; return reads === 1 ? envelope([job('job-a')]) : reload.promise;
    }
    if (path === '/v1/campaigns/campaign-a/audience-materialisations' && init.method === 'POST') return newJob;
    if (path === '/v1/audience-materialisations/job-a') return job('job-a');
    return inventory(path);
  });
  render(<SegmentSnapshotManager {...props} />);
  await screen.findByRole('button', { name: 'Cancel materialisation' });
  await screen.findByText('segment-a evidence 3');
  fireEvent.click(screen.getByRole('button', { name: 'Schedule durable snapshot' }));
  await waitFor(() => expect(reads).toBe(2));
  await act(async () => { tick(); });
  await settle(reload, envelope([newJob, job('job-a')]));
  expect(screen.getByText('77% durable')).toBeDefined();
  expect(screen.getByText('Durable audience materialisation scheduled.')).toBeDefined();
});

for (const valuesWire of ['null', 'omitted']) {
  test('a saved Go no-value rule with ' + valuesWire + ' values matches the editor for durable scheduling', async () => {
    const wire = JSON.parse('{"join":"AND","rules":[{"definitionCode":"REPORTED_AGE","operator":"is_known"' +
      (valuesWire === 'null' ? ',"values":null' : '') + '}]}');
    let scheduleBody: unknown;
    let scheduled = false;
    mockHTTP((path, init) => {
      if (path.startsWith('/v1/segments?')) return envelope([{ ...segment('segment-a'), definition: wire }]);
      if (path.includes('/campaign-a/audience-materialisations?')) return envelope(scheduled ? [job('job-a')] : []);
      if (path === '/v1/campaigns/campaign-a/audience-materialisations' && init.method === 'POST') {
        scheduleBody = JSON.parse(String(init.body)); scheduled = true; return job('job-a');
      }
      return inventory(path);
    });
    render(<SegmentSnapshotManager {...props} currentDefinition={{
      join: 'AND', rules: [{ definitionCode: 'REPORTED_AGE', operator: 'is_known', values: [] }]
    }} />);
    await screen.findByText('segment-a evidence 3');
    const schedule = screen.getByRole('button', { name: 'Schedule durable snapshot' }) as HTMLButtonElement;
    expect(schedule.disabled).toBe(false);
    fireEvent.click(schedule);
    await screen.findByText('Durable audience materialisation scheduled.');
    expect(scheduleBody).toEqual({ segmentId: 'segment-a', definitionVersion: 3, estimateId: 'estimate-a' });
  });
}
