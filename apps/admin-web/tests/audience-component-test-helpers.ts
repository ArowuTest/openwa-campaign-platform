import { act } from '@testing-library/react';
import { vi } from 'vitest';

export function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

export function envelope<T>(items: T[]) {
  return { items, count: items.length, hasMore: false };
}

export function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), { status: 200, headers: { 'content-type': 'application/json' } });
}

// Keep the real API client and pagination; substitute only the external HTTP boundary.
export function mockHTTP(route: (path: string, init: RequestInit) => unknown | Promise<unknown>) {
  const calls: Array<{ path: string; init: RequestInit }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace(/^\/api/, '');
    calls.push({ path, init });
    return jsonResponse(await route(path, init));
  });
  return calls;
}

export async function settle<T>(pending: ReturnType<typeof deferred<T>>, value: T) {
  await act(async () => { pending.resolve(value); await pending.promise; });
}

export const definition = {
  join: 'AND' as const,
  rules: [{ definitionCode: 'REPORTED_AGE', operator: 'greater_than', values: [18] }]
};

export function segment(id: string, organisationId = 'org-a') {
  return { id, organisationId, name: id + ' segment', description: 'Governed segment', definition, status: 'ACTIVE' as const,
    version: 3, createdAt: '2026-10-01T00:00:00Z', updatedAt: '2026-10-01T00:00:00Z' };
}

export function campaign(id: string, organisationId = 'org-a') {
  return { id, organisationId, name: id + ' campaign', purposeId: 'purpose-a', status: 'AUDIENCE_BUILDING',
    version: 1, maximumUniqueRecipients: 500, eligibleAudienceCount: 0 };
}

export function versions(id: string, reason = id + ' evidence') {
  return [3, 2, 1].map((version) => ({ segmentId: id, version, name: id + ' segment', description: 'Governed segment',
    definition, status: 'ACTIVE', changedBy: 'operator', reason: reason + ' ' + version, createdAt: '2026-10-01T00:00:00Z' }));
}

export function job(id: string, campaignId = 'campaign-a', status: 'PENDING' | 'COMPLETED' = 'PENDING') {
  return { id, campaignId, segmentId: 'segment-a', definitionVersion: 3, consentPolicyVersion: 'consent-v1',
    configurationVersion: 'config-v1', status, processedCount: 11, expectedCount: 100,
    ...(status === 'COMPLETED' ? { snapshotId: id + '-snapshot' } : {}),
    attemptCount: 1, version: 1, requestedAt: '2026-10-01T00:00:00Z',
    progressPercent: 11, updatedAt: '2026-10-01T00:00:00Z' };
}

export function snapshot(id: string, campaignId = 'campaign-a') {
  return { id, campaignId, segmentId: 'segment-a', definition, definitionVersion: 3,
    consentPolicyVersion: 'consent-v1', configurationVersion: 'config-v1', eligibleCount: 100,
    snapshotHash: id + '-hash', createdAt: '2026-10-01T00:00:00Z' };
}
