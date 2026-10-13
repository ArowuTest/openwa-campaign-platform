import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { SenderConsole } from '../app/senders/sender-console';

const navigation = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock('next/navigation', () => ({ useRouter: () => navigation }));
vi.mock('../components/auth-provider', () => ({
  useAuth: () => ({
    session: {
      id: 'operator',
      sessionId: 'session-a',
      permissions: ['sender.read', 'sender.write', 'sender.operate', 'sender.admin', 'sender.approve']
    }
  })
}));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  navigation.push.mockReset();
});

const session = {
  id: 'session-1',
  nodeId: 'node-1',
  poolId: 'pool-1',
  gatewayPoolId: 'gateway-1',
  maskedMsisdn: '+234 *** 1234',
  ownerReference: 'operations',
  registrationCountryIso2: 'NG',
  profileDisplayName: 'Pairing sender',
  recoveryReferenceConfigured: true,
  engineType: 'BAILEYS',
  engineVersion: '1',
  status: 'PAIRING',
  safeMessagesPerMinute: 5,
  safeDailyCapacity: 500,
  inFlightLimit: 1,
  sentToday: 0,
  version: 2
};

function inventoryResponse(path: string) {
  if (path.startsWith('/v1/sender-pools?')) return { items: [], count: 0, hasMore: false };
  if (path.startsWith('/v1/sender-sessions?')) return { items: [session], count: 1, hasMore: false };
  if (path.startsWith('/v1/sender-nodes?')) return { items: [], count: 0, hasMore: false };
  if (path.startsWith('/v1/admin/test-recipients?')) return { items: [], count: 0, hasMore: false };
  return undefined;
}

test('requests a governed pairing code with current version and renders only transient material', async () => {
  const calls: Array<{ path: string; init: RequestInit }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    calls.push({ path, init });
    const inventory = inventoryResponse(path);
    if (inventory) return new Response(JSON.stringify(inventory), { status: 200, headers: { 'content-type': 'application/json' } });
    if (path === '/v1/sender-sessions/session-1/pairing/code' && init.method === 'POST') {
      return new Response(JSON.stringify({
        sessionId: 'session-1',
        status: 'authenticating',
        payload: {
          pairingCode: 'ABCD-EFGH',
          internalDebugToken: 'must-not-render'
        }
      }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + path + ' ' + String(init.method));
  });

  render(<SenderConsole />);
  const senderText = await screen.findByText('Pairing sender');
  fireEvent.click(senderText.closest('tr') as HTMLTableRowElement);

  const pairingForm = screen.getByRole('heading', { name: 'Pairing code' }).closest('article');
  expect(pairingForm).not.toBeNull();
  const form = within(pairingForm as HTMLElement);
  fireEvent.change(form.getByLabelText('Pairing phone number'), { target: { value: '+2348012345678' } });
  fireEvent.change(form.getByLabelText('Reason'), { target: { value: 'Approved operator pairing request' } });
  fireEvent.click(form.getByRole('button', { name: 'Request pairing code' }));

  const code = await screen.findByLabelText('Temporary pairing code');
  expect(code.textContent).toBe('ABCD-EFGH');
  expect(screen.queryByText('must-not-render')).toBeNull();
  expect(screen.getByText(/clears automatically after 60 seconds/i)).toBeTruthy();

  const pairingCall = calls.find((call) => call.path === '/v1/sender-sessions/session-1/pairing/code');
  expect(pairingCall).toBeTruthy();
  expect(JSON.parse(String(pairingCall?.init.body))).toEqual({
    expectedVersion: 2,
    phoneNumber: '+2348012345678',
    reason: 'Approved operator pairing request'
  });

  fireEvent.click(screen.getByRole('button', { name: 'Dismiss now' }));
  await waitFor(() => expect(screen.queryByLabelText('Temporary pairing code')).toBeNull());
});

test('renders only a bounded PNG data URL for QR pairing material', async () => {
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    const inventory = inventoryResponse(path);
    if (inventory) return new Response(JSON.stringify(inventory), { status: 200, headers: { 'content-type': 'application/json' } });
    if (path === '/v1/sender-sessions/session-1/pairing/qr' && (!init.method || init.method === 'GET')) {
      return new Response(JSON.stringify({
        sessionId: 'session-1',
        status: 'qr_ready',
        payload: { qr: 'data:image/png;base64,QUJDRA==', internal: 'must-not-render' }
      }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + path + ' ' + String(init.method));
  });

  render(<SenderConsole />);
  const senderText = await screen.findByText('Pairing sender');
  fireEvent.click(senderText.closest('tr') as HTMLTableRowElement);
  fireEvent.click(screen.getByRole('button', { name: 'Request QR' }));

  const image = await screen.findByRole('img', { name: 'Temporary WhatsApp pairing QR code' });
  expect((image as HTMLImageElement).src).toContain('data:image/png;base64,QUJDRA==');
  expect(screen.queryByText('must-not-render')).toBeNull();
});
