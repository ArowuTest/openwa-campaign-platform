import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { ControlledExportManager } from '../components/controlled-export-manager';

const auth = vi.hoisted(() => ({
  session: { id: 'maker', sessionId: 'session-1', permissions: ['export.request', 'export.approve'] }
}));

vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));
vi.mock('../components/step-up-gate', () => ({ StepUpGate: ({ children }: { children: React.ReactNode }) => <>{children}</> }));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  auth.session = { id: 'maker', sessionId: 'session-1', permissions: ['export.request', 'export.approve'] };
});

test('requests a campaign export and exposes independent checker approval', async () => {
  let requested = false;
  let approved = false;
  const calls: Array<{ path: string; method: string; body?: string }> = [];

  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input).replace('/api', '');
    calls.push({ path, method: init?.method ?? 'GET', body: typeof init?.body === 'string' ? init.body : undefined });

    if (path.startsWith('/v1/exports?')) {
      const item = requested
        ? {
            id: 'export-1',
            kind: 'CAMPAIGN_REPORT',
            objectId: 'campaign-1',
            format: 'PDF',
            status: approved ? 'APPROVED' : 'PENDING_APPROVAL',
            requestedBy: 'maker',
            reason: 'Monthly campaign review',
            templateVersion: 'EXPORT-V1',
            version: approved ? 2 : 1,
            createdAt: '2026-10-12T00:00:00Z',
            updatedAt: '2026-10-12T00:00:00Z'
          }
        : null;
      return new Response(JSON.stringify({ items: item ? [item] : [], count: item ? 1 : 0, hasMore: false }), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      });
    }

    if (path === '/v1/exports' && init?.method === 'POST') {
      requested = true;
      return new Response(JSON.stringify({
        id: 'export-1',
        kind: 'CAMPAIGN_REPORT',
        objectId: 'campaign-1',
        format: 'PDF',
        status: 'PENDING_APPROVAL',
        requestedBy: 'maker',
        reason: 'Monthly campaign review',
        templateVersion: 'EXPORT-V1',
        version: 1,
        createdAt: '2026-10-12T00:00:00Z',
        updatedAt: '2026-10-12T00:00:00Z'
      }), { status: 202, headers: { 'content-type': 'application/json' } });
    }

    if (path === '/v1/exports/export-1/decision' && init?.method === 'POST') {
      approved = true;
      return new Response(JSON.stringify({
        id: 'export-1',
        kind: 'CAMPAIGN_REPORT',
        objectId: 'campaign-1',
        format: 'PDF',
        status: 'APPROVED',
        requestedBy: 'maker',
        approvedBy: 'checker',
        reason: 'Monthly campaign review',
        templateVersion: 'EXPORT-V1',
        version: 2,
        createdAt: '2026-10-12T00:00:00Z',
        updatedAt: '2026-10-12T00:00:00Z'
      }), { status: 200, headers: { 'content-type': 'application/json' } });
    }

    throw new Error('Unexpected request: ' + path);
  });

  const view = render(<ControlledExportManager campaignId="campaign-1" campaignName="October campaign" />);

  expect(await screen.findByText('No controlled exports yet.')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Purpose'), { target: { value: 'Monthly campaign review' } });
  fireEvent.click(screen.getByRole('button', { name: 'Request controlled export' }));

  expect(await screen.findByText('PENDING APPROVAL')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Approve export' })).toBeNull();

  auth.session.id = 'checker';
  view.rerender(<ControlledExportManager campaignId="campaign-1" campaignName="October campaign" />);
  expect(screen.getByRole('button', { name: 'Approve export' })).toBeTruthy();

  fireEvent.click(screen.getByRole('button', { name: 'Approve export' }));
  await waitFor(() => expect(screen.getByText('APPROVED')).toBeTruthy());
  expect(calls.some((call) => call.path === '/v1/exports' && call.method === 'POST' && call.body?.includes('CAMPAIGN_REPORT'))).toBe(true);
  expect(calls.some((call) => call.path === '/v1/exports/export-1/decision' && call.method === 'POST')).toBe(true);
});

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { 'content-type': 'application/json' } });
}

function readyExport(id: string, objectId: string) {
  return {
    id, objectId, kind: 'CAMPAIGN_REPORT', format: 'PDF', status: 'READY',
    requestedBy: 'maker', reason: 'Evidence for ' + objectId, version: 3,
    createdAt: '2026-10-12T00:00:00Z', updatedAt: '2026-10-12T00:00:00Z'
  };
}

test('filters the inventory to the supplied campaign ID', async () => {
  const paths: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    paths.push(String(input));
    return jsonResponse({ items: [readyExport('export-1', 'campaign/one')], count: 1, hasMore: false });
  });

  render(<ControlledExportManager campaignId="campaign/one" />);

  expect(await screen.findByText('Evidence for campaign/one')).toBeTruthy();
  expect(new URL(paths[0], 'http://localhost').searchParams.get('objectId')).toBe('campaign/one');
});

test('loads every bounded page while preserving object scope', async () => {
  const paths: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input);
    paths.push(path);
    const after = new URL(path, 'http://localhost').searchParams.get('after');
    return after
      ? jsonResponse({ items: [readyExport('export-2', 'campaign-1')], count: 1, hasMore: false })
      : jsonResponse({ items: [readyExport('export-1', 'campaign-1')], count: 1, hasMore: true, nextCursor: 'page/2' });
  });

  render(<ControlledExportManager objectId="campaign-1" />);

  await waitFor(() => expect(screen.getAllByText('Evidence for campaign-1')).toHaveLength(2));
  expect(paths).toHaveLength(2);
  expect(new URL(paths[1], 'http://localhost').searchParams.get('after')).toBe('page/2');
  expect(new URL(paths[1], 'http://localhost').searchParams.get('objectId')).toBe('campaign-1');
});

test('requests a privacy package through the case endpoint with format and reason only', async () => {
  auth.session.permissions = ['privacy.read', 'export.request'];
  const requests: Array<{ path: string; body: unknown }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    if (init?.method === 'POST') {
      requests.push({ path, body: JSON.parse(String(init.body)) });
      return jsonResponse({ id: 'privacy-export-1' }, 202);
    }
    return jsonResponse({ items: [], count: 0, hasMore: false });
  });

  render(<ControlledExportManager kind="PRIVACY_PACKAGE" objectId="privacy/case-1" />);
  fireEvent.change(screen.getByLabelText('Format'), { target: { value: 'JSON' } });
  fireEvent.change(screen.getByLabelText('Purpose'), { target: { value: '  Provide completed access package  ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Request controlled export' }));

  await waitFor(() => expect(requests).toEqual([{
    path: '/api/v1/privacy/cases/privacy%2Fcase-1/exports',
    body: { format: 'JSON', reason: 'Provide completed access package' }
  }]));
});

test('an approval-only checker cannot authorise a READY export download', async () => {
  auth.session = { id: 'checker', sessionId: 'session-1', permissions: ['export.approve'] };
  const posts: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    if (init?.method === 'POST') posts.push(String(input));
    return jsonResponse({ items: [readyExport('export-1', 'campaign-1')], count: 1, hasMore: false });
  });

  render(<ControlledExportManager campaignId="campaign-1" />);

  expect(await screen.findByText('Evidence for campaign-1')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Authorise download' })).toBeNull();
  expect(screen.queryByRole('link', { name: 'Download export' })).toBeNull();
  expect(posts).toEqual([]);
});

test('clears the previous object inventory and download grant before the next queue loads', async () => {
  let resolveNext!: (response: Response) => void;
  const nextResponse = new Promise<Response>((resolve) => { resolveNext = resolve; });
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    if (init?.method === 'POST') {
      return jsonResponse({ token: 'single-use-old-token', grant: { expiresAt: '2026-10-13T00:00:00Z' } });
    }
    if (new URL(path, 'http://localhost').searchParams.get('objectId') === 'campaign-2') return nextResponse;
    return jsonResponse({ items: [readyExport('export-1', 'campaign-1')], count: 1, hasMore: false });
  });

  const view = render(<ControlledExportManager objectId="campaign-1" />);
  fireEvent.click(await screen.findByRole('button', { name: 'Authorise download' }));
  expect(await screen.findByRole('link', { name: 'Download export' })).toBeTruthy();

  view.rerender(<ControlledExportManager objectId="campaign-2" />);

  expect(screen.queryByText('Evidence for campaign-1')).toBeNull();
  expect(screen.queryByRole('link', { name: 'Download export' })).toBeNull();
  await act(async () => { resolveNext(jsonResponse({ items: [], count: 0, hasMore: false })); });
});

test('hides an existing grant when the user loses export.request permission', async () => {
  vi.stubGlobal('fetch', async (_input: RequestInfo | URL, init?: RequestInit) => init?.method === 'POST'
    ? jsonResponse({ token: 'single-use-token', grant: { expiresAt: '2026-10-13T00:00:00Z' } })
    : jsonResponse({ items: [readyExport('export-1', 'campaign-1')], count: 1, hasMore: false }));

  const view = render(<ControlledExportManager objectId="campaign-1" />);
  fireEvent.click(await screen.findByRole('button', { name: 'Authorise download' }));
  expect(await screen.findByRole('link', { name: 'Download export' })).toBeTruthy();

  auth.session.permissions = ['export.approve'];
  view.rerender(<ControlledExportManager objectId="campaign-1" />);

  expect(screen.queryByRole('button', { name: 'Authorise download' })).toBeNull();
  expect(screen.queryByRole('link', { name: 'Download export' })).toBeNull();
});

test('returns to the current local page and query after step-up', async () => {
  let resolveLoad!: (response: Response) => void;
  vi.stubGlobal('fetch', () => new Promise<Response>((resolve) => { resolveLoad = resolve; }));
  render(<ControlledExportManager kind="AUDIT_LOG" />);
  const assign = vi.fn();
  vi.stubGlobal('window', { location: { pathname: '/audit', search: '?action=LOGIN_FAILED', assign } });

  await act(async () => {
    resolveLoad(jsonResponse({ code: 'STEP_UP_REQUIRED', message: 'Fresh verification required' }, 403));
  });

  expect(assign).toHaveBeenCalledWith('/step-up?returnTo=' + encodeURIComponent('/audit?action=LOGIN_FAILED'));
});

test('a late error from a previous object cannot redirect the current scope', async () => {
  let resolvePrevious!: (response: Response) => void;
  const previousResponse = new Promise<Response>((resolve) => { resolvePrevious = resolve; });
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) =>
    new URL(String(input), 'http://localhost').searchParams.get('objectId') === 'campaign-1'
      ? previousResponse
      : jsonResponse({ items: [], count: 0, hasMore: false }));
  const view = render(<ControlledExportManager objectId="campaign-1" />);
  await act(async () => { view.rerender(<ControlledExportManager objectId="campaign-2" />); });
  const assign = vi.fn();
  vi.stubGlobal('window', { location: { pathname: '/campaigns', search: '', assign } });

  await act(async () => {
    resolvePrevious(jsonResponse({ code: 'STEP_UP_REQUIRED', message: 'Old scope verification expired' }, 403));
  });

  expect(assign).not.toHaveBeenCalled();
});
