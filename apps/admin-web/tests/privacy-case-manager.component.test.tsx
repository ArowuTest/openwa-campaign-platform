import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { PrivacyCaseManager } from '../app/privacy/privacy-case-manager';

const auth = vi.hoisted(() => ({
  session: { id: 'privacy-maker', sessionId: 'privacy-session', permissions: ['privacy.read', 'privacy.write', 'privacy.approve', 'privacy.execute'] }
}));

vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

test('creates a privacy case, loads masked detail and submits it for independent approval', async () => {
  let created = false;
  let submitted = false;
  const calls: Array<{ path: string; method: string; body?: string }> = [];

  const getCase = () => ({
    id: 'case-1',
    type: 'ACCESS',
    status: submitted ? 'PENDING_APPROVAL' : 'OPEN',
    subjectMasked: '+234******0123',
    organisationId: 'org-1',
    requestedAt: '2026-10-12T10:00:00Z',
    dueAt: '2026-10-19T10:00:00Z',
    createdBy: 'privacy-maker',
    requestReason: 'Subject access request',
    version: submitted ? 2 : 1,
    createdAt: '2026-10-12T10:00:00Z',
    updatedAt: '2026-10-12T10:00:00Z'
  });

  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input).replace('/api', '');
    calls.push({ path, method: init?.method ?? 'GET', body: typeof init?.body === 'string' ? init.body : undefined });

    if (path.startsWith('/v1/privacy/cases?')) {
      return new Response(JSON.stringify({ items: created ? [getCase()] : [], count: created ? 1 : 0, hasMore: false }), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      });
    }

    if (path === '/v1/privacy/cases' && init?.method === 'POST') {
      created = true;
      return new Response(JSON.stringify(getCase()), { status: 201, headers: { 'content-type': 'application/json' } });
    }

    if (path === '/v1/privacy/cases/case-1' && (!init?.method || init.method === 'GET')) {
      return new Response(JSON.stringify(getCase()), { status: 200, headers: { 'content-type': 'application/json' } });
    }

    if (path.startsWith('/v1/privacy/cases/case-1/events')) {
      return new Response(JSON.stringify({ items: [{ id: 'event-1', caseId: 'case-1', type: 'CASE_CREATED', actorId: 'privacy-maker', caseVersion: 1, occurredAt: '2026-10-12T10:00:00Z' }], count: 1, hasMore: false }), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      });
    }

    if (path === '/v1/privacy/cases/case-1/submit' && init?.method === 'POST') {
      submitted = true;
      return new Response(JSON.stringify(getCase()), { status: 200, headers: { 'content-type': 'application/json' } });
    }

    throw new Error('Unexpected request: ' + path);
  });

  render(<PrivacyCaseManager />);

  expect(await screen.findByText('No privacy cases')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Subject MSISDN'), { target: { value: '+2348012340123' } });
  fireEvent.change(screen.getByLabelText('Request reason'), { target: { value: 'Subject access request' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create privacy case' }));

  expect((await screen.findAllByText('+234******0123')).length).toBeGreaterThanOrEqual(1);
  fireEvent.click(screen.getByRole('button', { name: /ACCESS/ }));
  expect(await screen.findByText('CASE_CREATED')).toBeTruthy();

  fireEvent.change(screen.getByLabelText('Submission reason'), { target: { value: 'Ready for independent review' } });
  fireEvent.click(screen.getByRole('button', { name: 'Submit for approval' }));

  await waitFor(() => expect(screen.getAllByText('PENDING APPROVAL').length).toBeGreaterThanOrEqual(1));
  expect(calls.some((call) => call.path === '/v1/privacy/cases' && call.method === 'POST' && call.body?.includes('ACCESS'))).toBe(true);
  expect(calls.some((call) => call.path === '/v1/privacy/cases/case-1/submit' && call.method === 'POST')).toBe(true);
});
