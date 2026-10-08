import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';

import { CampaignWorkspace } from '../app/campaigns/[id]/campaign-workspace';
import { deferred, envelope, settle } from './audience-component-test-helpers';

const auth = vi.hoisted(() => ({ session: { permissions: ['campaign.read'] } as { permissions: string[]; id?: string; sessionId?: string } }));
const navigation = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));
vi.mock('next/navigation', () => ({ useRouter: () => navigation }));

beforeEach(() => { auth.session = { id: 'operator', sessionId: 'session-a', permissions: ['campaign.read'] }; navigation.push.mockClear(); });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

function detail(id: string) {
  return { id, organisationId: 'org-a', name: id + ' campaign', purposeId: 'purpose-a', consentReviewId: 'consent-a',
    status: 'DRAFT', timezone: 'Africa/Lagos', maximumUniqueRecipients: 500, maximumMessagesPerRecipient: 1,
    eligibleAudienceCount: 0, createdBy: 'operator', createdAt: '2026-10-01T00:00:00Z', updatedAt: '2026-10-01T00:00:00Z', version: 3,
    transport: { channel: 'WHATSAPP', provider: 'META', engine: 'CLOUD_API', routingMode: 'SPECIFIC_SESSION',
      metaSenderId: 'saved-meta', sessionId: 'saved-session', adapterVersion: '1.2.3', requiredCapabilities: ['TEXT'],
      fallbackMode: 'NONE', routingPolicyVersion: 'policy-v1', capacityEvidenceVersion: 'capacity-v1' } };
}
function error(status: number, code: string) {
  return new Response(JSON.stringify({ error: code, message: 'Safe server error.' }), { status, headers: { 'content-type': 'application/json' } });
}
function http(route: (path: string, init: RequestInit) => unknown | Promise<unknown>) {
  const calls: Array<{ path: string; init: RequestInit }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace(/^\/api/, '');
    calls.push({ path, init });
    const value = await route(path, init);
    return value instanceof Response ? value : new Response(JSON.stringify(value), { status: 200, headers: { 'content-type': 'application/json' } });
  });
  return calls;
}
function supplemental(path: string) {
  if (path.endsWith('/workspace')) return { campaignId: path.split('/')[3], tags: [], archive: { archived: false, version: 1 } };
  if (path.endsWith('/metrics')) return {};
  if (path.includes('/test-messages?') || path.includes('/routing-plans?') || path.endsWith('/reservations')) return envelope([]);
  throw new Error('Unexpected request: ' + path);
}

test('campaign detail renders from one direct GET with no campaign inventory scan', async () => {
  const calls = http(path => path === '/v1/campaigns/campaign-a' ? detail('campaign-a') : path.startsWith('/v1/campaigns?') ? envelope([detail('campaign-a')]) : supplemental(path));
  render(<CampaignWorkspace campaignId="campaign-a" />);
  expect(await screen.findByRole('heading', { name: 'campaign-a campaign' })).toBeTruthy();
  expect(calls.filter(call => call.path === '/v1/campaigns/campaign-a')).toHaveLength(1);
  expect(calls.filter(call => call.path.startsWith('/v1/campaigns?'))).toHaveLength(0);
  expect(screen.getAllByText(/META \/ CLOUD_API/)[0]).toBeTruthy();
});

test.each([[403, 'PERMISSION_DENIED'], [404, 'CAMPAIGN_NOT_FOUND'], [503, 'CAMPAIGNS_UNAVAILABLE']])('core failure %s is explicit and finishes loading', async (status, code) => {
  http(path => path === '/v1/campaigns/campaign-a' || path.startsWith('/v1/campaigns?') ? error(Number(status), String(code)) : supplemental(path));
  render(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('alert');
  expect(screen.queryByText(/Loading authoritative campaign/)).toBeNull();
  expect(screen.queryByRole('heading', { name: 'campaign-a campaign' })).toBeNull();
});

test('late campaign A cannot replace B and prior detail is hidden while B loads', async () => {
  const pendingA = deferred<unknown>();
  const pendingB = deferred<unknown>();
  let inventory = 0;
  const calls = http(path => {
    if (path === '/v1/campaigns/campaign-a') return pendingA.promise;
    if (path === '/v1/campaigns/campaign-b') return pendingB.promise;
    if (path.startsWith('/v1/campaigns?')) return ++inventory === 1 ? pendingA.promise.then(value => envelope([value])) : pendingB.promise.then(value => envelope([value]));
    return supplemental(path);
  });
  const view = render(<CampaignWorkspace campaignId="campaign-a" />);
  await waitFor(() => expect(calls.length).toBeGreaterThan(0));
  view.rerender(<CampaignWorkspace campaignId="campaign-b" />);
  await settle(pendingB, detail('campaign-b'));
  expect(await screen.findByRole('heading', { name: 'campaign-b campaign' })).toBeTruthy();
  await settle(pendingA, detail('campaign-a'));
  expect(screen.queryByRole('heading', { name: 'campaign-a campaign' })).toBeNull();
  const oldRequest = calls.find(call => call.path === '/v1/campaigns/campaign-a');
  expect(oldRequest?.init.signal?.aborted).toBe(true);
});

test('loaded A disappears immediately on B context change and cannot survive B permission failure', async () => {
  const pending = deferred<unknown>();
  http(path => {
    if (path === '/v1/campaigns/campaign-a') return detail('campaign-a');
    if (path === '/v1/campaigns/campaign-b') return pending.promise;
    if (path.startsWith('/v1/campaigns?')) return envelope([detail('campaign-a'), detail('campaign-b')]);
    return supplemental(path);
  });
  const view = render(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  view.rerender(<CampaignWorkspace campaignId="campaign-b" />);
  expect(screen.queryByRole('heading', { name: 'campaign-a campaign' })).toBeNull();
  await settle(pending, error(403, 'PERMISSION_DENIED'));
  await screen.findByRole('alert');
  expect(screen.queryByRole('heading')).toBeNull();
});

test('core detail is visible while supplemental evidence is still pending', async () => {
  const pending = deferred<unknown>();
  http(path => {
    if (path === '/v1/campaigns/campaign-a') return detail('campaign-a');
    if (path.startsWith('/v1/campaigns?')) return envelope([detail('campaign-a')]);
    if (path.endsWith('/workspace')) return pending.promise;
    return supplemental(path);
  });
  render(<CampaignWorkspace campaignId="campaign-a" />);
  expect(await screen.findByRole('heading', { name: 'campaign-a campaign' })).toBeTruthy();
  await settle(pending, supplemental('/v1/campaigns/campaign-a/workspace'));
});

test('reservation, workspace and metrics failures do not hide core detail or report zero as authoritative', async () => {
  http(path => {
    if (path === '/v1/campaigns/campaign-a') return detail('campaign-a');
    if (path.startsWith('/v1/campaigns?')) return envelope([detail('campaign-a')]);
    if (path.includes('/routing-plans?')) return envelope([{ id: 'plan-a', version: 1, routes: [] }]);
    if (path.endsWith('/reservations') || path.endsWith('/workspace') || path.endsWith('/metrics')) return error(503, 'SUPPLEMENTAL_UNAVAILABLE');
    return supplemental(path);
  });
  render(<CampaignWorkspace campaignId="campaign-a" />);
  expect(await screen.findByRole('heading', { name: 'campaign-a campaign' })).toBeTruthy();
  expect(await screen.findByText(/Supplemental evidence unavailable:/)).toBeTruthy();
  expect(screen.getByText(/Supplemental evidence unavailable:/).textContent).toMatch(/workspace.*metrics.*reservations/i);
});

test('permission loss clears loaded campaign and aborts active evidence requests', async () => {
  const pending = deferred<unknown>();
  const calls = http(path => {
    if (path === '/v1/campaigns/campaign-a') return detail('campaign-a');
    if (path.startsWith('/v1/campaigns?')) return envelope([detail('campaign-a')]);
    return pending.promise;
  });
  const view = render(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  auth.session = { permissions: [] };
  view.rerender(<CampaignWorkspace campaignId="campaign-a" />);
  expect(screen.queryByRole('heading', { name: 'campaign-a campaign' })).toBeNull();
  await screen.findByRole('alert');
  expect(calls.filter(call => call.init.signal).every(call => call.init.signal?.aborted)).toBe(true);
  await act(async () => pending.resolve(envelope([])));
});

test('late A mutation success cannot reload A, abort B reads or retain A action status', async () => {
  auth.session.permissions = ['campaign.read', 'campaign.operate'];
  const action = deferred<unknown>();
  const calls = http((path, init) => {
    if (init.method === 'POST' && path === '/v1/campaigns/campaign-a/execution/cancel') return action.promise;
    if (path === '/v1/campaigns/campaign-a') return detail('campaign-a');
    if (path === '/v1/campaigns/campaign-b') return detail('campaign-b');
    return supplemental(path);
  });
  const view = render(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  fireEvent.click(screen.getByRole('button', { name: 'Cancel campaign' }));
  await waitFor(() => expect(calls.filter(call => call.init.method === 'POST')).toHaveLength(1));
  view.rerender(<CampaignWorkspace campaignId="campaign-b" />);
  await screen.findByRole('heading', { name: 'campaign-b campaign' });
  await settle(action, { status: 'CANCELLED' });
  expect(screen.queryByRole('heading', { name: 'campaign-b campaign' })).toBeTruthy();
  expect(calls.filter(call => call.path === '/v1/campaigns/campaign-a')).toHaveLength(1);
  expect(calls.find(call => call.path === '/v1/campaigns/campaign-b')?.init.signal?.aborted).toBe(false);
  expect(screen.queryByText(/Cancel campaign completed/)).toBeNull();
  expect(screen.queryByText(/Cancel campaign/, { selector: 'span.pill' })).toBeNull();
});

test('late A mutation error cannot replace B status or clear a pending B action', async () => {
  auth.session.permissions = ['campaign.read', 'campaign.operate'];
  const actionA = deferred<unknown>();
  const actionB = deferred<unknown>();
  const calls = http((path, init) => {
    if (init.method === 'POST') return path.includes('/campaign-a/') ? actionA.promise : actionB.promise;
    if (path === '/v1/campaigns/campaign-a') return detail('campaign-a');
    if (path === '/v1/campaigns/campaign-b') return detail('campaign-b');
    return supplemental(path);
  });
  const view = render(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  fireEvent.click(screen.getByRole('button', { name: 'Cancel campaign' }));
  view.rerender(<CampaignWorkspace campaignId="campaign-b" />);
  await screen.findByRole('heading', { name: 'campaign-b campaign' });
  fireEvent.click(screen.getByRole('button', { name: 'Cancel campaign' }));
  await waitFor(() => expect(calls.filter(call => call.init.method === 'POST' && call.path.includes('/campaign-b/'))).toHaveLength(1));
  await settle(actionA, new Response(JSON.stringify({ code: 'MFA_STEP_UP_REQUIRED', message: 'STALE_A_ERROR' }), { status: 403, headers: { 'content-type': 'application/json' } }));
  expect(screen.queryByText('STALE_A_ERROR')).toBeNull();
  expect(navigation.push).not.toHaveBeenCalled();
  expect(screen.getByText(/Cancel campaign/, { selector: 'span.pill' })).toBeTruthy();
  await settle(actionB, { status: 'CANCELLED' });
  await waitFor(() => expect(screen.queryByText(/Cancel campaign/, { selector: 'span.pill' })).toBeNull());
});

test('a replaced session invalidates pending mutations even for the same campaign and permissions', async () => {
  auth.session.permissions = ['campaign.read', 'campaign.operate'];
  const action = deferred<unknown>();
  const calls = http((path, init) => init.method === 'POST' ? action.promise : path === '/v1/campaigns/campaign-a' ? detail('campaign-a') : supplemental(path));
  const view = render(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  fireEvent.click(screen.getByRole('button', { name: 'Cancel campaign' }));
  auth.session = { ...auth.session, sessionId: 'session-b' };
  view.rerender(<CampaignWorkspace campaignId="campaign-a" />);
  await waitFor(() => expect(calls.filter(call => call.path === '/v1/campaigns/campaign-a')).toHaveLength(2));
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  await settle(action, { status: 'CANCELLED' });
  expect(calls.filter(call => call.path === '/v1/campaigns/campaign-a')).toHaveLength(2);
  expect(screen.queryByText(/Cancel campaign completed/)).toBeNull();
  expect(screen.queryByText(/Cancel campaign/, { selector: 'span.pill' })).toBeNull();
});

test.each(['success', 'error'])('permission loss rejects late mutation %s and a restored context has no old busy state', async outcome => {
  auth.session.permissions = ['campaign.read', 'campaign.operate'];
  const action = deferred<unknown>();
  const calls = http((path, init) => init.method === 'POST' ? action.promise : path === '/v1/campaigns/campaign-a' ? detail('campaign-a') : supplemental(path));
  const view = render(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  fireEvent.click(screen.getByRole('button', { name: 'Cancel campaign' }));
  auth.session = { ...auth.session, permissions: [] };
  view.rerender(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('alert');
  await settle(action, outcome === 'success' ? { status: 'CANCELLED' } : error(403, 'PERMISSION_DENIED'));
  expect(calls.filter(call => call.path === '/v1/campaigns/campaign-a')).toHaveLength(1);
  auth.session = { ...auth.session, permissions: ['campaign.read', 'campaign.operate'] };
  view.rerender(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  expect(screen.queryByText(/Cancel campaign completed|Safe server error/)).toBeNull();
  expect(screen.queryByText(/Cancel campaign/, { selector: 'span.pill' })).toBeNull();
});

test.each(['success', 'error'])('unmount rejects late mutation %s before reload or step-up navigation', async outcome => {
  auth.session.permissions = ['campaign.read', 'campaign.operate'];
  const action = deferred<unknown>();
  const calls = http((path, init) => init.method === 'POST' ? action.promise : path === '/v1/campaigns/campaign-a' ? detail('campaign-a') : supplemental(path));
  const view = render(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  fireEvent.click(screen.getByRole('button', { name: 'Cancel campaign' }));
  view.unmount();
  await settle(action, outcome === 'success' ? { status: 'CANCELLED' } : new Response(JSON.stringify({ code: 'MFA_STEP_UP_REQUIRED' }), { status: 403, headers: { 'content-type': 'application/json' } }));
  expect(calls.filter(call => call.path === '/v1/campaigns/campaign-a')).toHaveLength(1);
  expect(navigation.push).not.toHaveBeenCalled();
});
