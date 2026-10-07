import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { FilterBuilder } from '../components/filter-builder';
import { deferred, envelope, mockHTTP, segment, settle } from './audience-component-test-helpers';

afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });

const age = { code: 'REPORTED_AGE', displayName: 'Reported age', description: 'Governed age',
  dataType: 'integer', operators: ['is_known', 'greater_than'], allowedValues: [] };
type Route = (path: string, init: RequestInit) => unknown | Promise<unknown>;

function builderHTTP(action: Route, savedSegment = segment('segment-a')) {
  return mockHTTP((path, init) => {
    if (path === '/v1/filter-definitions') return envelope([age]);
    if (path === '/v1/geography/countries') return envelope([]);
    if (path.startsWith('/v1/organisations?')) return envelope(['org-a', 'org-b'].map((id) => ({ id, legalName: id, status: 'ACTIVE' })));
    if (path.startsWith('/v1/consent-purposes?')) return envelope(['purpose-a', 'purpose-b'].map((id) => ({
      id, organisationId: 'org-a', code: id, name: id, channel: 'WHATSAPP', wordingVersion: '1', active: true
    })));
    if (path.startsWith('/v1/segments?')) return envelope([savedSegment]);
    if (path.includes('/versions?') || path.startsWith('/v1/campaigns?')) return envelope([]);
    return action(path, init);
  });
}

async function ready(validate = true) {
  render(<FilterBuilder />);
  fireEvent.change(await screen.findByLabelText('Organisation'), { target: { value: 'org-a' } });
  await screen.findByRole('option', { name: 'purpose-a — purpose-a' });
  fireEvent.change(screen.getByLabelText('Consent purpose'), { target: { value: 'purpose-a' } });
  fireEvent.change(screen.getByLabelText('Segment name'), { target: { value: 'Current cohort' } });
  if (validate) {
    fireEvent.click(screen.getByRole('button', { name: 'Validate cohort' }));
    await screen.findByText(/Cohort definition is valid for/);
  }
}

async function changeContext(kind: string) {
  if (kind === 'definition') fireEvent.change(screen.getByLabelText('Join'), { target: { value: 'OR' } });
  if (kind === 'organisation') fireEvent.change(screen.getByLabelText('Organisation'), { target: { value: 'org-b' } });
  if (kind === 'purpose') fireEvent.change(screen.getByLabelText('Consent purpose'), { target: { value: 'purpose-b' } });
  if (kind === 'opened segment') {
    fireEvent.click(await screen.findByRole('button', { name: 'Open in editor' }));
  }
}

const estimateAsOf = '2026-09-30T01:00:00Z';
const estimateCalculatedAt = '2026-10-01T02:00:00Z';

// Exact Go response shape: asOf belongs to the job, never cohort.Estimate/result.
function estimateJob(id: string, clientRequestId: string, count = 999, status = 'COMPLETED') {
  return { id, organisationId: 'org-a', purposeId: 'purpose-a', channel: 'WHATSAPP', clientRequestId,
    asOf: estimateAsOf, requestedBy: 'operator',
    evidence: { consentReviewId: 'review-a', consentReviewVersion: 1, consentWordingVersion: '1',
      organisationPolicyId: 'policy-a', organisationPolicyVersion: 1 },
    status, attemptCount: 1, maxAttempts: 3,
    createdAt: '2026-10-01T00:00:00Z', updatedAt: '2026-10-01T03:00:00Z',
    ...(status === 'COMPLETED' ? { completedAt: '2026-10-01T04:00:00Z',
      result: { eligibleCount: count, calculatedAt: estimateCalculatedAt } } : {}) };
}

for (const context of ['definition', 'organisation', 'purpose', 'opened segment']) {
  test('late validation cannot authorize a changed ' + context, async () => {
    const pending = deferred<unknown>();
    let started = false;
    builderHTTP((path) => {
      if (path === '/v1/cohorts/validate') { started = true; return pending.promise; }
      throw new Error('Unexpected request ' + path);
    });
    await ready(false);
    fireEvent.click(screen.getByRole('button', { name: 'Validate cohort' }));
    await waitFor(() => expect(started).toBe(true));
    await changeContext(context);
    await settle(pending, {});
    expect(screen.queryByText(/Cohort definition is valid for/)).toBeNull();
    expect((screen.getByRole('button', { name: 'Save governed segment' }) as HTMLButtonElement).disabled).toBe(true);
  });

  test('late estimate cannot restore counts for a changed ' + context, async () => {
    const pending = deferred<unknown>();
    let requestId = '';
    builderHTTP((path, init) => {
      if (path === '/v1/cohorts/validate') return {};
      if (path === '/v1/cohort-estimates') { requestId = JSON.parse(String(init.body)).clientRequestId; return pending.promise; }
      throw new Error('Unexpected request ' + path);
    });
    await ready();
    fireEvent.click(screen.getByRole('button', { name: 'Estimate governed audience' }));
    await waitFor(() => expect(requestId).not.toBe(''));
    await changeContext(context);
    await settle(pending, estimateJob('old-estimate', requestId));
    expect(screen.queryByText('999')).toBeNull();
    expect(screen.queryByText(/Final eligible count completed/)).toBeNull();
  });
}

test('late saved response cannot restore a segment identity after the definition changes', async () => {
  const pending = deferred<unknown>();
  let started = false;
  builderHTTP((path) => {
    if (path === '/v1/cohorts/validate') return {};
    if (path === '/v1/segments') { started = true; return pending.promise; }
    throw new Error('Unexpected request ' + path);
  });
  await ready();
  fireEvent.click(screen.getByRole('button', { name: 'Save governed segment' }));
  await waitFor(() => expect(started).toBe(true));
  await changeContext('definition');
  await settle(pending, { id: 'segment-a', name: 'OLD SAVE', version: 1 });
  expect(screen.queryByText(/Saved OLD SAVE/)).toBeNull();
  expect((screen.getByRole('button', { name: 'Save new version' }) as HTMLButtonElement).disabled).toBe(true);
});

test('a stale estimate scheduling failure cannot start recovery in the changed context', async () => {
  const pending = deferred<unknown>();
  let started = false;
  const calls = builderHTTP((path) => {
    if (path === '/v1/cohorts/validate') return {};
    if (path === '/v1/cohort-estimates') { started = true; return pending.promise; }
    if (path.startsWith('/v1/cohort-estimates?')) return envelope([]);
    throw new Error('Unexpected request ' + path);
  });
  await ready();
  fireEvent.click(screen.getByRole('button', { name: 'Estimate governed audience' }));
  await waitFor(() => expect(started).toBe(true));
  await changeContext('organisation');
  await act(async () => { pending.reject(new Error('Lost POST response')); await pending.promise.catch(() => {}); });
  expect(calls.filter((call) => call.path.startsWith('/v1/cohort-estimates?'))).toHaveLength(0);
  expect(screen.queryByText('Lost POST response')).toBeNull();
});

test('late recovery inventory cannot restore an old estimate', async () => {
  const recovery = deferred<unknown>();
  let requestId = '';
  const calls = builderHTTP((path, init) => {
    if (path === '/v1/cohorts/validate') return {};
    if (path === '/v1/cohort-estimates') { requestId = JSON.parse(String(init.body)).clientRequestId; throw new Error('Lost response'); }
    if (path.startsWith('/v1/cohort-estimates?')) return recovery.promise;
    throw new Error('Unexpected request ' + path);
  });
  await ready();
  fireEvent.click(screen.getByRole('button', { name: 'Estimate governed audience' }));
  await waitFor(() => expect(calls.some((call) => call.path.startsWith('/v1/cohort-estimates?'))).toBe(true));
  await changeContext('purpose');
  await settle(recovery, envelope([estimateJob('old-estimate', requestId)]));
  expect(screen.queryByText('999')).toBeNull();
});

test('an old estimate response cannot clear the newer estimate busy state', async () => {
  const old = deferred<unknown>();
  const current = deferred<unknown>();
  const ids: string[] = [];
  builderHTTP((path, init) => {
    if (path === '/v1/cohorts/validate') return {};
    if (path === '/v1/cohort-estimates') {
      ids.push(JSON.parse(String(init.body)).clientRequestId);
      return ids.length === 1 ? old.promise : current.promise;
    }
    throw new Error('Unexpected request ' + path);
  });
  await ready();
  fireEvent.click(screen.getByRole('button', { name: 'Estimate governed audience' }));
  await waitFor(() => expect(ids).toHaveLength(1));
  await changeContext('definition');
  fireEvent.click(screen.getByRole('button', { name: 'Validate cohort' }));
  await screen.findByText(/Cohort definition is valid for/);
  fireEvent.click(screen.getByRole('button', { name: 'Estimate governed audience' }));
  await waitFor(() => expect(ids).toHaveLength(2));
  await settle(old, estimateJob('old-estimate', ids[0]));
  expect(screen.getByRole('button', { name: 'Scheduling…' })).toBeDefined();
  await settle(current, estimateJob('current-estimate', ids[1], 42));
  expect(screen.getByText('42')).toBeDefined();
});

for (const ingress of ['POST', 'poll', 'recovery']) {
  test('exact Go estimate ' + ingress + ' wire displays job as-of separately from result calculation time', async () => {
    let requestId = '';
    const calls = builderHTTP((path, init) => {
      if (path === '/v1/cohorts/validate') return {};
      if (path === '/v1/cohort-estimates') {
        requestId = JSON.parse(String(init.body)).clientRequestId;
        if (ingress === 'recovery') throw new Error('Lost POST response');
        return estimateJob('estimate-a', requestId, 42, ingress === 'poll' ? 'PENDING' : 'COMPLETED');
      }
      if (ingress === 'poll' && path === '/v1/cohort-estimates/estimate-a?organisationId=org-a') return estimateJob('estimate-a', requestId, 42);
      if (ingress === 'recovery' && path.startsWith('/v1/cohort-estimates?')) {
        return envelope([estimateJob('unrelated', 'different-request', 987), estimateJob('estimate-a', requestId, 42)]);
      }
      throw new Error('Unexpected request ' + path);
    });
    await ready();
    if (ingress === 'poll') vi.useFakeTimers();
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Estimate governed audience' })); });
    if (ingress === 'poll') {
      expect(screen.queryByText('42')).toBeNull();
      await act(async () => { await vi.advanceTimersByTimeAsync(800); });
      expect(calls.some((call) => call.path === '/v1/cohort-estimates/estimate-a?organisationId=org-a')).toBe(true);
    } else {
      await screen.findByText('42');
    }
    expect(calls.filter((call) => call.path === '/v1/cohort-estimates')).toHaveLength(1);
    expect(calls.filter((call) => call.path.startsWith('/v1/cohort-estimates?'))).toHaveLength(ingress === 'recovery' ? 1 : 0);
    expect(calls.filter((call) => call.path.startsWith('/v1/cohort-estimates/'))).toHaveLength(ingress === 'poll' ? 1 : 0);
    const exactWire = estimateJob('estimate-a', requestId, 42);
    expect(exactWire.asOf).toBe(estimateAsOf);
    expect(exactWire.result).not.toHaveProperty('asOf');
    expect(Object.keys(exactWire.result ?? {}).sort()).toEqual(['calculatedAt', 'eligibleCount']);
    expect(screen.getByText('42')).toBeDefined();
    expect(within(screen.getByText('As of').parentElement!).getByText(new Date(estimateAsOf).toLocaleString())).toBeDefined();
    expect(within(screen.getByText('Calculated').parentElement!).getByText(new Date(estimateCalculatedAt).toLocaleString())).toBeDefined();
  });
}

test('current validated estimate and saved segment remain usable until their context changes', async () => {
  let savedBody: unknown;
  builderHTTP((path, init) => {
    if (path === '/v1/cohorts/validate') return {};
    if (path === '/v1/cohort-estimates') return estimateJob('estimate-a', JSON.parse(String(init.body)).clientRequestId, 42);
    if (path === '/v1/segments') {
      savedBody = JSON.parse(String(init.body)); return { id: 'segment-a', name: 'Current cohort', version: 4 };
    }
    throw new Error('Unexpected request ' + path);
  });
  await ready();
  fireEvent.click(screen.getByRole('button', { name: 'Estimate governed audience' }));
  await screen.findByText('42');
  fireEvent.click(screen.getByRole('button', { name: 'Save governed segment' }));
  await screen.findByText('Saved Current cohort as segment segment-a · version 4.');
  expect(savedBody).toEqual({ organisationId: 'org-a', name: 'Current cohort',
    definition: { join: 'AND', rules: [{ definitionCode: 'REPORTED_AGE', operator: 'is_known', values: [] }] } });
  expect((screen.getByRole('button', { name: 'Save new version' }) as HTMLButtonElement).disabled).toBe(false);
  await changeContext('purpose');
  expect(screen.queryByText('42')).toBeNull();
  expect((screen.getByRole('button', { name: 'Save governed segment' }) as HTMLButtonElement).disabled).toBe(true);
});

for (const valuesWire of ['null', 'omitted']) {
  test('a saved Go no-value rule with ' + valuesWire + ' values opens in the editor without semantic drift', async () => {
    const wire = JSON.parse('{"join":"AND","rules":[{"definitionCode":"REPORTED_AGE","operator":"is_known"' +
      (valuesWire === 'null' ? ',"values":null' : '') + '}]}');
    const calls = builderHTTP((path) => {
      if (path === '/v1/cohorts/validate') return {};
      throw new Error('Unexpected request ' + path);
    }, { ...segment('segment-a'), definition: wire });
    await ready(false);
    fireEvent.change(screen.getByLabelText('Operator'), { target: { value: 'greater_than' } });
    fireEvent.click(await screen.findByRole('button', { name: 'Open in editor' }));
    expect((screen.getByLabelText('Segment name') as HTMLInputElement).value).toBe('segment-a segment');
    expect((screen.getByLabelText('Operator') as HTMLSelectElement).value).toBe('is_known');
    fireEvent.click(screen.getByRole('button', { name: 'Validate cohort' }));
    await screen.findByText(/Cohort definition is valid for/);
    const validation = calls.find((call) => call.path === '/v1/cohorts/validate');
    expect(JSON.parse(String(validation?.init.body))).toEqual({
      join: 'AND', rules: [{ definitionCode: 'REPORTED_AGE', operator: 'is_known', values: [] }]
    });
    expect((screen.getByRole('button', { name: 'Save governed segment' }) as HTMLButtonElement).disabled).toBe(false);
  });
}
