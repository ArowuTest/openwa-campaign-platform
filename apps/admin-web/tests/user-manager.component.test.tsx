import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { UserManager } from '../app/admin/users/user-manager';

const navigation = vi.hoisted(() => ({ push: vi.fn() }));
const auth = vi.hoisted(() => ({
  session: { id: 'super-admin', sessionId: 'session-admin', permissions: ['identity.admin'] }
}));
vi.mock('next/navigation', () => ({ useRouter: () => navigation }));
vi.mock('../components/auth-provider', () => ({
  useAuth: () => ({ session: auth.session })
}));

afterEach(() => {
  auth.session = { id: 'super-admin', sessionId: 'session-admin', permissions: ['identity.admin'] };
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  navigation.push.mockReset();
});

const roles = [
  { code: 'ANALYST', name: 'Analyst', description: 'Views aggregated reports.', systemRole: true, permissions: ['report.read', 'campaign.read'] },
  { code: 'FINANCE_USER', name: 'Finance user', description: 'Records commercial evidence.', systemRole: true, permissions: ['finance.read', 'finance.write'] }
];

const account = {
  id: 'user-1',
  email: 'alice@example.test',
  displayName: 'Alice Admin',
  status: 'ACTIVE',
  mfaRequired: true,
  roleCodes: ['ANALYST'],
  version: 4,
  lastLoginAt: '2026-10-12T12:00:00Z',
  createdAt: '2026-10-01T12:00:00Z',
  updatedAt: '2026-10-12T12:00:00Z'
};

test('loads the server role catalogue and provisions a named user without persisting one-time secrets', async () => {
  const calls: Array<{ path: string; method: string; body?: string }> = [];
  let created = false;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    const method = init.method ?? 'GET';
    calls.push({ path, method, body: typeof init.body === 'string' ? init.body : undefined });

    if (path === '/v1/admin/roles') {
      return new Response(JSON.stringify(roles), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path.startsWith('/v1/admin/users?')) {
      return new Response(JSON.stringify({ items: created ? [account] : [], count: created ? 1 : 0, hasMore: false }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/admin/users' && method === 'POST') {
      created = true;
      return new Response(JSON.stringify({
        account: { ...account, roleCodes: ['FINANCE_USER'], status: 'INVITED', version: 1 },
        totpSecret: 'JBSWY3DPEHPK3PXP'
      }), { status: 201, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + method + ' ' + path);
  });

  render(<UserManager />);
  expect(await screen.findByText('No internal users')).toBeTruthy();
  expect(screen.getByText('Finance user')).toBeTruthy();

  const form = screen.getByRole('heading', { name: 'Provision internal user' }).closest('form');
  expect(form).not.toBeNull();
  const view = within(form as HTMLElement);
  const password = view.getByLabelText('Temporary password');
  expect(password.getAttribute('aria-describedby')).toBe('provision-password-hint');
  expect(view.getByText(/At least 20 characters/)).toBeTruthy();
  const provisioningReason = view.getByLabelText('Provisioning reason');
  expect(provisioningReason.getAttribute('aria-describedby')).toBe('provision-reason-hint');
  expect(view.getByText(/Required audit reason, 5 to 1,000 characters/)).toBeTruthy();
  fireEvent.change(view.getByLabelText('Email'), { target: { value: 'finance@example.test' } });
  fireEvent.change(view.getByLabelText('Display name'), { target: { value: 'Finance Operator' } });
  fireEvent.change(view.getByLabelText('Temporary password'), { target: { value: 'temporary-password-long-enough' } });
  fireEvent.click(view.getByLabelText('Finance user'));
  fireEvent.change(view.getByLabelText('Provisioning reason'), { target: { value: 'Approved finance operations onboarding' } });
  fireEvent.click(view.getByRole('button', { name: 'Provision user' }));

  expect((await screen.findByLabelText('One-time MFA enrolment secret')).textContent).toBe('JBSWY3DPEHPK3PXP');
  expect(await screen.findByText(/Internal user provisioned/)).toBeTruthy();
  expect((view.getByLabelText('Temporary password') as HTMLInputElement).value).toBe('');
  const createCall = calls.find((call) => call.path === '/v1/admin/users' && call.method === 'POST');
  expect(JSON.parse(String(createCall?.body))).toMatchObject({
    email: 'finance@example.test',
    displayName: 'Finance Operator',
    roleCodes: ['FINANCE_USER'],
    reason: 'Approved finance operations onboarding'
  });

  fireEvent.click(screen.getByRole('button', { name: 'Dismiss secret' }));
  expect(screen.queryByLabelText('One-time MFA enrolment secret')).toBeNull();
});

test('updates a selected user with expected version and routes step-up challenges safely', async () => {
  let failWithStepUp = false;
  const calls: Array<{ path: string; method: string; body?: string }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    const method = init.method ?? 'GET';
    calls.push({ path, method, body: typeof init.body === 'string' ? init.body : undefined });
    if (path === '/v1/admin/roles') return new Response(JSON.stringify(roles), { status: 200, headers: { 'content-type': 'application/json' } });
    if (path.startsWith('/v1/admin/users?')) return new Response(JSON.stringify({ items: [account], count: 1, hasMore: false }), { status: 200, headers: { 'content-type': 'application/json' } });
    if (path === '/v1/admin/users/user-1' && method === 'PUT') {
      if (failWithStepUp) return new Response(JSON.stringify({ code: 'STEP_UP_REQUIRED', message: 'Recent MFA required' }), { status: 403, headers: { 'content-type': 'application/json' } });
      return new Response(JSON.stringify({ ...account, displayName: 'Alice Updated', roleCodes: ['FINANCE_USER'], version: 5 }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + method + ' ' + path);
  });

  render(<UserManager />);
  fireEvent.click(await screen.findByRole('button', { name: /Alice Admin/ }));
  const editor = screen.getByRole('heading', { name: 'User access and lifecycle' }).closest('form');
  expect(editor).not.toBeNull();
  const view = within(editor as HTMLElement);
  expect(view.getByLabelText('Change reason').getAttribute('aria-describedby')).toBe('edit-reason-hint');
  expect(view.getByLabelText('New temporary password').getAttribute('aria-describedby')).toBe('reset-password-hint');
  expect(view.getByLabelText('Credential reset reason').getAttribute('aria-describedby')).toBe('reset-reason-hint');
  expect(view.getByLabelText('Security action reason').getAttribute('aria-describedby')).toBe('security-reason-hint');
  fireEvent.change(view.getByLabelText('Display name'), { target: { value: 'Alice Updated' } });
  fireEvent.click(view.getByLabelText('Analyst'));
  fireEvent.click(view.getByLabelText('Finance user'));
  fireEvent.change(view.getByLabelText('Change reason'), { target: { value: 'Move operator to approved finance role' } });
  fireEvent.click(view.getByRole('button', { name: 'Save user' }));

  await waitFor(() => expect(calls.some((call) => call.path === '/v1/admin/users/user-1' && call.method === 'PUT')).toBe(true));
  const update = calls.find((call) => call.path === '/v1/admin/users/user-1' && call.method === 'PUT');
  expect(JSON.parse(String(update?.body))).toMatchObject({
    displayName: 'Alice Updated',
    status: 'ACTIVE',
    mfaRequired: true,
    roleCodes: ['FINANCE_USER'],
    expectedVersion: 4,
    reason: 'Move operator to approved finance role'
  });

  failWithStepUp = true;
  fireEvent.change(view.getByLabelText('Change reason'), { target: { value: 'Second governed update attempt' } });
  fireEvent.click(view.getByRole('button', { name: 'Save user' }));
  await waitFor(() => expect(navigation.push).toHaveBeenCalledWith('/step-up?returnTo=' + encodeURIComponent('/admin/users')));
});

test('operator session replacement and permission loss discard the one-time MFA secret and prior user inventory', async () => {
  let created = false;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    const method = init.method ?? 'GET';
    if (path === '/v1/admin/roles') {
      return new Response(JSON.stringify(roles), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path.startsWith('/v1/admin/users?')) {
      const items = auth.session.id === 'super-admin' && created ? [account] : [];
      return new Response(JSON.stringify({ items, count: items.length, hasMore: false }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/admin/users' && method === 'POST') {
      created = true;
      return new Response(JSON.stringify({
        account: { ...account, roleCodes: ['FINANCE_USER'], status: 'INVITED', version: 1 },
        totpSecret: 'JBSWY3DPEHPK3PXP'
      }), { status: 201, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + method + ' ' + path);
  });

  const view = render(<UserManager />);
  const form = (await screen.findByRole('heading', { name: 'Provision internal user' })).closest('form');
  const controls = within(form as HTMLElement);
  fireEvent.change(controls.getByLabelText('Email'), { target: { value: 'finance@example.test' } });
  fireEvent.change(controls.getByLabelText('Display name'), { target: { value: 'Finance Operator' } });
  fireEvent.change(controls.getByLabelText('Temporary password'), { target: { value: 'temporary-password-long-enough' } });
  fireEvent.click(controls.getByLabelText('Finance user'));
  fireEvent.change(controls.getByLabelText('Provisioning reason'), { target: { value: 'Approved finance operations onboarding' } });
  fireEvent.click(controls.getByRole('button', { name: 'Provision user' }));
  expect(await screen.findByLabelText('One-time MFA enrolment secret')).toBeTruthy();

  auth.session = { id: 'second-admin', sessionId: 'second-session', permissions: ['identity.admin'] };
  view.rerender(<UserManager />);
  expect(screen.queryByLabelText('One-time MFA enrolment secret')).toBeNull();
  expect(screen.queryByText('Alice Admin')).toBeNull();
  expect(await screen.findByText('No internal users')).toBeTruthy();

  auth.session = { id: 'second-admin', sessionId: 'second-session', permissions: [] };
  view.rerender(<UserManager />);
  expect(screen.getByText('User administration requires identity.admin permission.')).toBeTruthy();
  expect(screen.queryByLabelText('One-time MFA enrolment secret')).toBeNull();
  expect(screen.queryByRole('heading', { name: 'Provision internal user' })).toBeNull();
});
