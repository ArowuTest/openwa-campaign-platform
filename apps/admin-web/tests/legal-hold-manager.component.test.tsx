import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { LegalHoldManager } from '../app/privacy/legal-hold-manager';

const auth = vi.hoisted(() => ({
  session: { id: 'hold-maker', sessionId: 'hold-session', permissions: ['privacy.hold', 'privacy.approve'] }
}));

vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

test('searches, creates and submits a legal hold for independent approval', async () => {
  let created = false;
  let submitted = false;
  const getHold = () => ({
    id: 'hold-1',
    organisationId: 'org-1',
    scope: 'ALL',
    status: submitted ? 'PENDING_APPROVAL' : 'DRAFT',
    reason: 'Preserve evidence for an active privacy review',
    createdBy: 'hold-maker',
    version: submitted ? 2 : 1,
    createdAt: '2026-10-12T10:00:00Z'
  });

  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input).replace('/api', '');
    if (path.startsWith('/v1/privacy/legal-holds?')) {
      return new Response(JSON.stringify({ items: created ? [getHold()] : [], count: created ? 1 : 0, hasMore: false }), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      });
    }
    if (path === '/v1/privacy/legal-holds' && init?.method === 'POST') {
      created = true;
      return new Response(JSON.stringify(getHold()), { status: 201, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/privacy/legal-holds/hold-1/events?limit=100') {
      return new Response(JSON.stringify({ items: [], count: 0, hasMore: false }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/privacy/legal-holds/hold-1/submit' && init?.method === 'POST') {
      submitted = true;
      return new Response(JSON.stringify(getHold()), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + path);
  });

  render(<LegalHoldManager />);

  expect(await screen.findByText('Search a subject MSISDN')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Hold subject MSISDN'), { target: { value: '+2348012340123' } });
  fireEvent.change(screen.getByLabelText('Hold reason'), { target: { value: 'Preserve evidence for an active privacy review' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create legal hold' }));

  expect((await screen.findAllByText('DRAFT')).length).toBeGreaterThanOrEqual(1);
  fireEvent.change(screen.getByLabelText('Submission reason'), { target: { value: 'The review requires preservation while the case is assessed' } });
  fireEvent.click(screen.getByRole('button', { name: 'Submit hold' }));
  await waitFor(() => expect(screen.getAllByText('PENDING APPROVAL').length).toBeGreaterThanOrEqual(1));
});


test('clears the previous subject register when the lookup changes before a new search', async () => {
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    if (path.startsWith('/v1/privacy/legal-holds?')) {
      return new Response(JSON.stringify({
        items: [{ id: 'hold-a', scope: 'ALL', status: 'ACTIVE', reason: 'Preserve evidence for the active review', createdBy: 'checker', version: 2, createdAt: '2026-10-12T10:00:00Z' }],
        count: 1,
        hasMore: false
      }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + path);
  });

  render(<LegalHoldManager />);
  fireEvent.change(screen.getByLabelText('Lookup subject MSISDN'), { target: { value: '+2348012340123' } });
  fireEvent.click(screen.getByRole('button', { name: 'Search holds' }));
  expect(await screen.findByText('hold-a')).toBeTruthy();

  fireEvent.change(screen.getByLabelText('Lookup subject MSISDN'), { target: { value: '+2348099999999' } });
  expect(screen.queryByText('hold-a')).toBeNull();
  expect(screen.getByText('Search a subject MSISDN')).toBeTruthy();
});
