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

test.each([
 ['ABCDEF01-2345-4678-9ABC-DEF012345678', 'abcdef01-2345-4678-9abc-def012345678'],
 ['AbCdEf01-2345-4678-9aBc-DeF012345678', 'ABCDEF01-2345-4678-9ABC-DEF012345678']
])('UUID spelling direct link %s renders the same saved campaign', async (routeId, responseId) => {
 const canonical = 'abcdef01-2345-4678-9abc-def012345678';
 const calls = http(path => /^\/v1\/campaigns\/[^/]+$/.test(path) ? {...detail(responseId), name:'UUID campaign'} : supplemental(path));
 render(<CampaignWorkspace campaignId={routeId}/>);
 expect(await screen.findByRole('heading',{name:'UUID campaign'})).toBeTruthy();
 expect(screen.queryByText(/Loading authoritative campaign/)).toBeNull();
 expect(calls[0].path).toBe('/v1/campaigns/'+canonical);
});
test('UUID direct link rejects a genuinely different response identity', async () => {
 http(path => /^\/v1\/campaigns\/[^/]+$/.test(path) ? {...detail('abcdef01-2345-4678-9abc-def012345679'),name:'Different private campaign'} : supplemental(path));
 render(<CampaignWorkspace campaignId="ABCDEF01-2345-4678-9ABC-DEF012345678"/>);
 await screen.findByRole('alert');
 expect(screen.queryByRole('heading',{name:'Different private campaign'})).toBeNull();
});


test('controlled test uses only approved recipients and exact-route READY session options', async () => {
  auth.session = {
    id: 'operator',
    sessionId: 'session-a',
    permissions: ['campaign.read', 'campaign.write']
  };
  const campaign = {
    ...detail('campaign-a'),
    status: 'MESSAGE_APPROVED',
    messageVersionId: 'message-v1',
    messageContentHash: 'content-hash',
    transport: {
      channel: 'WHATSAPP',
      provider: 'OPENWA',
      engine: 'BAILEYS',
      routingMode: 'SENDER_POOL',
      gatewayPoolId: 'gateway-a',
      senderPoolId: 'pool-a',
      adapterVersion: '1.2.3',
      requiredCapabilities: ['SEND_TEXT'],
      fallbackMode: 'NONE',
      routingPolicyVersion: 'policy-v1',
      capacityEvidenceVersion: 'capacity-v1'
    }
  };
  const calls = http((path, init) => {
    if (path === '/v1/campaigns/campaign-a') return campaign;
    if (path === '/v1/campaigns/campaign-a/test-message-options') {
      return {
        campaignId: 'campaign-a',
        campaignVersion: 3,
        messageVersionId: 'message-v1',
        provider: 'OPENWA',
        engine: 'BAILEYS',
        senderPoolId: 'pool-a',
        gatewayPoolId: 'gateway-a',
        supported: true,
        recipients: [
          { id: 'recipient-active', label: 'Approved QA', maskedMsisdn: '+234 *** 1234', status: 'ACTIVE' }
        ],
        sessions: [
          { id: 'session-ready', profileDisplayName: 'QA sender', maskedMsisdn: '+234 *** 5678', status: 'READY', version: 4 }
        ],
        recipientInventoryComplete: true,
        sessionInventoryComplete: true
      };
    }
    if (path === '/v1/campaigns/campaign-a/test-messages' && init.method === 'POST') {
      return { id: 'test-send-1', status: 'PENDING' };
    }
    return supplemental(path);
  });

  render(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  const recipient = await screen.findByLabelText('Approved test recipient');
  const session = await screen.findByLabelText('Compatible READY sender session');
  expect(recipient.textContent).toContain('Approved QA · +234 *** 1234');
  expect(session.textContent).toContain('QA sender · +234 *** 5678');
  expect(screen.queryByText(/recipient-draft|Wrong pool|raw MSISDN/i)).toBeNull();

  fireEvent.change(recipient, { target: { value: 'recipient-active' } });
  fireEvent.change(session, { target: { value: 'session-ready' } });
  fireEvent.change(screen.getByLabelText('Reason', { selector: 'textarea[name="reason"]' }), { target: { value: 'Approved exact-route controlled test' } });
  fireEvent.submit(screen.getByRole('button', { name: 'Send controlled test' }).closest('form')!);

  await waitFor(() => expect(calls.some(call => call.path === '/v1/campaigns/campaign-a/test-messages' && call.init.method === 'POST')).toBe(true));
  const request = calls.find(call => call.path === '/v1/campaigns/campaign-a/test-messages' && call.init.method === 'POST');
  const body = JSON.parse(String(request?.init.body));
  expect(body).toMatchObject({
    messageVersionId: 'message-v1',
    testRecipientId: 'recipient-active',
    gatewayPoolId: 'gateway-a',
    senderPoolId: 'pool-a',
    provider: 'OPENWA',
    engine: 'BAILEYS',
    senderSessionId: 'session-ready',
    reason: 'Approved exact-route controlled test'
  });
});

test('stale controlled-test options are rejected instead of enabling raw-id fallback', async () => {
  auth.session = {
    id: 'operator',
    sessionId: 'session-a',
    permissions: ['campaign.read', 'campaign.write']
  };
  const campaign = {
    ...detail('campaign-a'),
    messageVersionId: 'message-v2',
    transport: {
      channel: 'WHATSAPP',
      provider: 'OPENWA',
      engine: 'BAILEYS',
      routingMode: 'SENDER_POOL',
      gatewayPoolId: 'gateway-a',
      senderPoolId: 'pool-a',
      adapterVersion: '1.2.3',
      requiredCapabilities: ['SEND_TEXT'],
      fallbackMode: 'NONE',
      routingPolicyVersion: 'policy-v1',
      capacityEvidenceVersion: 'capacity-v1'
    }
  };
  http(path => {
    if (path === '/v1/campaigns/campaign-a') return campaign;
    if (path === '/v1/campaigns/campaign-a/test-message-options') {
      return {
        campaignId: 'campaign-a',
        campaignVersion: 2,
        messageVersionId: 'message-v1',
        provider: 'OPENWA',
        engine: 'BAILEYS',
        senderPoolId: 'pool-a',
        gatewayPoolId: 'gateway-a',
        supported: true,
        recipients: [{ id: 'stale-recipient', label: 'Stale', maskedMsisdn: '+234 *** 0000', status: 'ACTIVE' }],
        sessions: [{ id: 'stale-session', profileDisplayName: 'Stale', maskedMsisdn: '+234 *** 0001', status: 'READY', version: 1 }],
        recipientInventoryComplete: true,
        sessionInventoryComplete: true
      };
    }
    return supplemental(path);
  });

  render(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  expect(await screen.findByText(/controlled-test options changed with the campaign/i)).toBeTruthy();
  expect((screen.getByRole('button', { name: 'Send controlled test' }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.queryByDisplayValue('stale-recipient')).toBeNull();
  expect(screen.queryByDisplayValue('stale-session')).toBeNull();
});


test('capacity hold uses only server-derived proposal and reuses client request ID after an ambiguous failure', async () => {
  auth.session = {
    id: 'operator',
    sessionId: 'session-a',
    permissions: ['campaign.read', 'campaign.write', 'campaign.approve']
  };
  const current = {
    ...detail('campaign-a'),
    status: 'MESSAGE_APPROVED',
    messageVersionId: '50000000-0000-4000-8000-000000000001',
    messageContentHash: 'a'.repeat(64),
    requestedStartAt: '2026-10-12T10:00:00Z',
    completionDeadlineAt: '2026-10-12T12:00:00Z',
    transport: {
      channel: 'WHATSAPP',
      provider: 'OPENWA',
      engine: 'BAILEYS',
      routingMode: 'SENDER_POOL',
      gatewayPoolId: '20000000-0000-4000-8000-000000000001',
      gatewayPoolVersion: 3,
      senderPoolId: '30000000-0000-4000-8000-000000000001',
      adapterVersion: '1.2.3',
      providerDefinitionId: '60000000-0000-4000-8000-000000000001',
      providerDefinitionVersion: 4,
      requiredCapabilities: ['SEND_TEXT'],
      fallbackMode: 'NONE',
      routingPolicyVersion: 'route-v7',
      capacityEvidenceVersion: 'capacity-v9'
    }
  };
  const accepted = {
    id: 'test-send-accepted',
    campaignId: 'campaign-a',
    messageVersionId: current.messageVersionId,
    messageContentHash: current.messageContentHash,
    gatewayPoolId: current.transport.gatewayPoolId,
    gatewayPoolVersion: current.transport.gatewayPoolVersion,
    senderPoolId: current.transport.senderPoolId,
    provider: current.transport.provider,
    engine: current.transport.engine,
    senderSessionId: 'session-ready',
    routeReference: 'exact-route',
    status: 'ACCEPTED',
    createdAt: '2026-10-10T08:00:00Z'
  };
  const proposal = {
    campaignId: 'campaign-a',
    campaignVersion: 3,
    senderPoolId: current.transport.senderPoolId,
    gatewayPoolId: current.transport.gatewayPoolId,
    provider: 'OPENWA',
    engine: 'BAILEYS',
    reservationStart: '2026-10-12T10:00:00Z',
    reservationEnd: '2026-10-12T12:00:00Z',
    basisRecipientCount: 120,
    measurementAsOf: '2026-10-10T08:30:00Z',
    healthySessions: 3,
    healthyNodes: 2,
    minimumHealthyNodes: 1,
    availableMessagesPerMinute: 20,
    availableHourlyUnits: 1200,
    availableDailyUnits: 5000,
    safetyMarginPercent: 15,
    requiredMessagesPerMinute: 1,
    reservedMessagesPerMinute: 2,
    reservedHourlyUnits: 120,
    reservedDailyUnits: 120,
    pacingPolicyId: '40000000-0000-4000-8000-000000000001',
    pacingPolicyVersion: 4,
    pacingMessagesPerMinute: 10,
    pacingHourlyAllowance: 600,
    pacingDailyAllowance: 2000,
    routingPolicyVersion: current.transport.routingPolicyVersion,
    capacityEvidenceVersion: current.transport.capacityEvidenceVersion,
    classification: 'READY_TO_HOLD'
  };
  const holdBodies: Array<Record<string, unknown>> = [];
  let holdAttempts = 0;
  const calls = http((path, init) => {
    if (path === '/v1/campaigns/campaign-a') return current;
    if (path === '/v1/campaigns/campaign-a/test-messages?limit=100') return envelope([accepted]);
    if (path === '/v1/campaigns/campaign-a/routing-plans?limit=20') return envelope([]);
    if (path === '/v1/campaigns/campaign-a/test-message-options') {
      return {
        campaignId: 'campaign-a', campaignVersion: 3, messageVersionId: current.messageVersionId,
        provider: 'OPENWA', engine: 'BAILEYS', senderPoolId: current.transport.senderPoolId,
        gatewayPoolId: current.transport.gatewayPoolId, supported: true,
        recipients: [], sessions: [], recipientInventoryComplete: true, sessionInventoryComplete: true
      };
    }
    if (path === '/v1/campaigns/campaign-a/capacity-hold-proposal') return proposal;
    if (path === '/v1/campaigns/campaign-a/message-versions?limit=100') return envelope([]);
    if (path === '/v1/campaigns/campaign-a/capacity-hold' && init.method === 'POST') {
      holdBodies.push(JSON.parse(String(init.body)));
      holdAttempts++;
      if (holdAttempts === 1) throw new Error('response lost after upstream acceptance');
      return { plan: { id: 'plan-held', routes: [] }, proposal };
    }
    return supplemental(path);
  });

  render(<CampaignWorkspace campaignId="campaign-a" />);
  await screen.findByRole('heading', { name: 'campaign-a campaign' });
  expect(await screen.findByText('120 recipients')).toBeTruthy();
  expect(screen.getByText(/2 MPM · 120 hourly · 120 daily/)).toBeTruthy();
  expect(screen.queryByLabelText('Reserved messages per minute')).toBeNull();
  expect(screen.queryByLabelText('Reserved hourly units')).toBeNull();
  expect(screen.queryByLabelText('Reserved daily units')).toBeNull();
  expect(screen.queryByLabelText('Pacing policy version')).toBeNull();

  const reason = screen.getByLabelText('Capacity approval reason');
  fireEvent.change(reason, { target: { value: 'Approved exact-route measured capacity hold' } });
  fireEvent.submit(screen.getByRole('button', { name: 'Confirm governed hold' }).closest('form')!);
  await screen.findByText('response lost after upstream acceptance');

  fireEvent.submit(screen.getByRole('button', { name: 'Confirm governed hold' }).closest('form')!);
  await waitFor(() => expect(holdBodies).toHaveLength(2));

  expect(holdBodies[0]).toEqual(holdBodies[1]);
  expect(holdBodies[0]).toMatchObject({
    expectedCampaignVersion: 3,
    reason: 'Approved exact-route measured capacity hold'
  });
  expect(String(holdBodies[0].clientRequestId)).toMatch(/^[0-9a-f-]{16,}$/i);
  expect(Object.keys(holdBodies[0]).sort()).toEqual(['clientRequestId', 'expectedCampaignVersion', 'reason']);
  expect(calls.some(call => call.path === '/v1/campaigns/campaign-a/capacity-hold-proposal')).toBe(true);
});


test('capacity proposal recognises an accepted exact-route test on a later bounded page', async () => {
  auth.session = { id: 'operator', sessionId: 'session-a', permissions: ['campaign.read', 'campaign.write'] };
  const current = {
    ...detail('campaign-a'),
    status: 'MESSAGE_APPROVED',
    version: 3,
    maximumUniqueRecipients: 150,
    eligibleAudienceCount: 120,
    messageVersionId: '50000000-0000-4000-8000-000000000001',
    messageContentHash: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    requestedStartAt: '2026-10-12T10:00:00Z',
    completionDeadlineAt: '2026-10-12T12:00:00Z',
    transport: {
      channel: 'WHATSAPP',
      provider: 'OPENWA',
      engine: 'BAILEYS',
      routingMode: 'SENDER_POOL',
      senderPoolId: '30000000-0000-4000-8000-000000000001',
      gatewayPoolId: '20000000-0000-4000-8000-000000000001',
      gatewayPoolVersion: 3,
      adapterVersion: '1',
      providerDefinitionId: '60000000-0000-4000-8000-000000000001',
      providerDefinitionVersion: 4,
      requiredCapabilities: ['TEXT'],
      fallbackMode: 'NONE',
      routingPolicyVersion: 'route-v7',
      capacityEvidenceVersion: 'capacity-v9'
    }
  };
  const accepted = {
    id: '70000000-0000-4000-8000-000000000001',
    campaignId: current.id,
    messageVersionId: current.messageVersionId,
    messageContentHash: current.messageContentHash,
    gatewayPoolId: current.transport.gatewayPoolId,
    gatewayPoolVersion: current.transport.gatewayPoolVersion,
    senderPoolId: current.transport.senderPoolId,
    provider: current.transport.provider,
    engine: current.transport.engine,
    senderSessionId: 'session-ready',
    routeReference: 'exact-route',
    status: 'ACCEPTED',
    createdAt: '2026-10-10T08:00:00Z'
  };
  const proposal = {
    campaignId: current.id,
    campaignVersion: current.version,
    senderPoolId: current.transport.senderPoolId,
    gatewayPoolId: current.transport.gatewayPoolId,
    provider: current.transport.provider,
    engine: current.transport.engine,
    reservationStart: current.requestedStartAt,
    reservationEnd: current.completionDeadlineAt,
    basisRecipientCount: 120,
    measurementAsOf: '2026-10-10T08:30:00Z',
    healthySessions: 3,
    healthyNodes: 2,
    minimumHealthyNodes: 1,
    availableMessagesPerMinute: 20,
    availableHourlyUnits: 1200,
    availableDailyUnits: 5000,
    safetyMarginPercent: 15,
    requiredMessagesPerMinute: 1,
    reservedMessagesPerMinute: 2,
    reservedHourlyUnits: 120,
    reservedDailyUnits: 120,
    pacingPolicyId: '40000000-0000-4000-8000-000000000001',
    pacingPolicyVersion: 4,
    pacingMessagesPerMinute: 10,
    pacingHourlyAllowance: 600,
    pacingDailyAllowance: 2000,
    routingPolicyVersion: current.transport.routingPolicyVersion,
    capacityEvidenceVersion: current.transport.capacityEvidenceVersion,
    classification: 'READY_TO_HOLD'
  };
  const noise = Array.from({ length: 100 }, (_, index) => ({
    ...accepted,
    id: 'noise-' + index,
    messageContentHash: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    status: 'FAILED'
  }));
  const calls = http(path => {
    if (path === '/v1/campaigns/campaign-a') return current;
    if (path === '/v1/campaigns/campaign-a/test-messages?limit=100') {
      return { items: noise, count: noise.length, hasMore: true, nextCursor: 'page2' };
    }
    if (path === '/v1/campaigns/campaign-a/test-messages?limit=100&cursor=page2') return envelope([accepted]);
    if (path === '/v1/campaigns/campaign-a/routing-plans?limit=20') return envelope([]);
    if (path === '/v1/campaigns/campaign-a/test-message-options') {
      return {
        campaignId: current.id, campaignVersion: current.version, messageVersionId: current.messageVersionId,
        provider: current.transport.provider, engine: current.transport.engine,
        senderPoolId: current.transport.senderPoolId, gatewayPoolId: current.transport.gatewayPoolId,
        supported: true, recipients: [], sessions: [], recipientInventoryComplete: true, sessionInventoryComplete: true
      };
    }
    if (path === '/v1/campaigns/campaign-a/capacity-hold-proposal') return proposal;
    if (path === '/v1/campaigns/campaign-a/message-versions?limit=100') return envelope([]);
    return supplemental(path);
  });

  render(<CampaignWorkspace campaignId="campaign-a" />);
  expect(await screen.findByText('120 recipients')).toBeTruthy();
  expect(calls.some(call => call.path === '/v1/campaigns/campaign-a/test-messages?limit=100&cursor=page2')).toBe(true);
  expect(calls.some(call => call.path === '/v1/campaigns/campaign-a/capacity-hold-proposal')).toBe(true);
});
