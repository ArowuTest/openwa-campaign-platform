import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { OrganisationManager } from '../app/organisations/organisation-manager';

vi.mock('../components/auth-provider', () => ({
  useAuth: () => ({ session: { id: 'operator', sessionId: 'session', permissions: ['organisation.read', 'organisation.write', 'organisation.approve'] } })
}));

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

test('loads cursor-paginated organisations and edits the selected record with expected version', async () => {
  const calls: Array<{ path: string; init: RequestInit }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input);
    calls.push({ path, init });
    if (path.includes('/v1/organisations?')) {
      const cursor = new URL(path, 'https://admin.test').searchParams.get('cursor');
      return new Response(JSON.stringify(cursor
        ? { items: [{ id: 'org-2', legalName: 'Beta', status: 'UNDER_REVIEW', version: 1, createdAt: '2026-01-02T00:00:00Z' }], count: 1, hasMore: false }
        : { items: [{ id: 'org-1', legalName: 'Alpha', status: 'ACTIVE', version: 3, createdAt: '2026-01-01T00:00:00Z' }], count: 1, hasMore: true, nextCursor: 'page-2' }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path.endsWith('/v1/organisations/org-1')) {
      return new Response(JSON.stringify({ id: 'org-1', legalName: 'Alpha', status: 'ACTIVE', version: 3, createdAt: '2026-01-01T00:00:00Z' }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (init.method === 'PUT') return new Response(JSON.stringify({ id: 'org-1', legalName: 'Alpha edited', status: 'ACTIVE', version: 4, createdAt: '2026-01-01T00:00:00Z' }), { status: 200, headers: { 'content-type': 'application/json' } });
    return new Response(JSON.stringify({ items: [] }), { status: 200, headers: { 'content-type': 'application/json' } });
  });
  render(<OrganisationManager />);
  expect(await screen.findByRole('button', { name: 'Beta' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: /Alpha/ }));
  expect(await screen.findByRole('heading', { name: 'Organisation detail' })).toBeTruthy();
  fireEvent.change(screen.getAllByLabelText('Legal name')[1], { target: { value: 'Alpha edited' } });
  fireEvent.change(screen.getByLabelText('Change reason'), { target: { value: 'Updated vetted client details' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save organisation' }));
  await waitFor(() => expect(calls.some((call) => call.init.method === 'PUT')).toBe(true));
  const update = calls.find((call) => call.init.method === 'PUT');
  expect(JSON.parse(String(update?.init.body))).toMatchObject({ expectedVersion: 3, reason: 'Updated vetted client details', legalName: 'Alpha edited' });
});

test('status changes require a reason and send the selected expected version', async () => {
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input);
    if (path.includes('/v1/organisations?')) return new Response(JSON.stringify({ items: [{ id: 'org-1', legalName: 'Alpha', status: 'UNDER_REVIEW', version: 2, createdAt: '2026-01-01T00:00:00Z' }], count: 1, hasMore: false }), { status: 200, headers: { 'content-type': 'application/json' } });
    if (path.endsWith('/v1/organisations/org-1')) return new Response(JSON.stringify({ id: 'org-1', legalName: 'Alpha', status: 'UNDER_REVIEW', version: 2, createdAt: '2026-01-01T00:00:00Z' }), { status: 200, headers: { 'content-type': 'application/json' } });
    return new Response(JSON.stringify({ id: 'org-1', legalName: 'Alpha', status: 'ACTIVE', version: 3, createdAt: '2026-01-01T00:00:00Z' }), { status: 200, headers: { 'content-type': 'application/json' } });
  });
  render(<OrganisationManager />);
  fireEvent.click(await screen.findByRole('button', { name: /Alpha/ }));
  await screen.findByRole('heading', { name: 'Organisation detail' });
  fireEvent.change(screen.getByLabelText('Target status'), { target: { value: 'ACTIVE' } });
  expect((screen.getByRole('button', { name: 'Set ACTIVE' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByLabelText('Status reason'), { target: { value: 'Vetting completed and approved' } });
  expect((screen.getByRole('button', { name: 'Set ACTIVE' }) as HTMLButtonElement).disabled).toBe(false);
});
