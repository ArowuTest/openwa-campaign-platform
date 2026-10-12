import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
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
