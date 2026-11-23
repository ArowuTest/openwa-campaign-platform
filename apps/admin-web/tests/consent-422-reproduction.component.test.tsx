import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ConsentReviewManager } from '../app/consent-reviews/review-manager';

// Regression: preserves the API's safe field validation contract through the Admin UI.
const auth = vi.hoisted(() => ({
  session: { id: 'repro-maker', sessionId: 'repro-session', permissions: ['consent.read', 'consent.write'] }
}));
vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

test('consent form must show the specific server validation reason instead of the generic 422 checklist', async () => {
  let posted = 0;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    if (path === '/v1/consent-reviews?limit=100') {
      return new Response(JSON.stringify({ items: [], count: 0, hasMore: false }),
        { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/consent-reviews' && init.method === 'POST') {
      posted += 1;
      return new Response(JSON.stringify({
        error: 'CONSENT_REVIEW_INVALID',
        message: 'The consent review could not be created.',
        details: { fields: { sampleReviewNotes: 'sample review notes must not contain email addresses or telephone numbers' } },
        requestId: 'repro-request-001'
      }), { status: 422, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request ' + path);
  });
  render(<ConsentReviewManager />);
  await screen.findByRole('heading', { name: 'Record consent assessment' });
  const form = screen.getByRole('button', { name: 'Create draft review' }).closest('form')!;
  fireEvent.change(screen.getByLabelText('Organisation ID'), { target: { value: '00000000-0000-4000-8000-000000000001' } });
  fireEvent.change(screen.getByLabelText('Review name'), { target: { value: 'Safe synthetic review' } });
  // Fire the submit event directly; this test concerns API error handling, not HTML5 required constraints.
  fireEvent.submit(form);
  await waitFor(() => expect(posted).toBe(1));
  expect(await screen.findByText(/sample review notes must not contain email addresses or telephone numbers/)).toBeTruthy();
  expect(screen.getByPlaceholderText('Record what was checked without entering contact PII.').getAttribute('aria-invalid')).toBe('true');
  expect((screen.getByLabelText('Review name') as HTMLInputElement).value).toBe('Safe synthetic review');
  expect(screen.getByText(/Review not saved/).getAttribute('role')).toBe('alert');
  expect(screen.getByText(/repro-request-001/)).toBeTruthy();
});


test('a service failure is not misreported as form input, and raw database details are never displayed', async () => {
  let posted = 0;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    if (path === '/v1/consent-reviews?limit=100') {
      return new Response(JSON.stringify({ items: [], count: 0, hasMore: false }),
        { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/consent-reviews' && init.method === 'POST') {
      posted += 1;
      return new Response(JSON.stringify({
        error: 'INTERNAL_ERROR', message: 'The request could not be completed.',
        details: { detail: 'pq: confidential-connection-credentials' },
        requestId: 'synthetic-error-002'
      }), { status: 500, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request ' + path);
  });
  render(<ConsentReviewManager />);
  await screen.findByRole('heading', { name: 'Record consent assessment' });
  const form = screen.getByRole('button', { name: 'Create draft review' }).closest('form')!;
  fireEvent.change(screen.getByLabelText('Review name'), { target: { value: 'Preserve my work' } });
  fireEvent.submit(form);
  await waitFor(() => expect(posted).toBe(1));
  expect(await screen.findByText(/service could not complete this request/i)).toBeTruthy();
  expect(screen.queryByText(/confidential-connection-credentials/)).toBeNull();
  expect((screen.getByLabelText('Review name') as HTMLInputElement).value).toBe('Preserve my work');
  expect(screen.getByText(/synthetic-error-002/)).toBeTruthy();
});

test('inactive organisation shows a clear conflict and highlights its identifier', async () => {
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    if (path === '/v1/consent-reviews?limit=100') {
      return new Response(JSON.stringify({ items: [], count: 0, hasMore: false }),
        { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/consent-reviews' && init.method === 'POST') {
      return new Response(JSON.stringify({
        error: 'ORGANISATION_NOT_ACTIVE',
        message: 'The selected organisation must be active before creating a consent review.',
        details: { fields: { organisationId: 'Select an active organisation.' } },
        requestId: 'synthetic-conflict-003'
      }), { status: 409, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request ' + path);
  });
  render(<ConsentReviewManager />);
  await screen.findByRole('heading', { name: 'Record consent assessment' });
  const form = screen.getByRole('button', { name: 'Create draft review' }).closest('form')!;
  fireEvent.change(screen.getByLabelText('Organisation ID'), { target: { value: '00000000-0000-4000-8000-000000000001' } });
  fireEvent.submit(form);
  expect(await screen.findByText('Select an active organisation.')).toBeTruthy();
  expect(screen.getByText(/organisation must be active/)).toBeTruthy();
  expect(screen.getByLabelText(/Organisation ID/).getAttribute('aria-invalid')).toBe('true');
});
