import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';

import { CommercialGovernance } from '../app/campaigns/[id]/commercial-governance';
import { type CampaignDetail } from '../lib/campaign-preparation-model';

const auth = vi.hoisted(() => ({
  session: {
    id: '60000000-0000-4000-8000-000000000001',
    sessionId: 'session-finance',
    permissions: ['finance.read', 'finance.write', 'finance.approve', 'campaign.approve']
  }
}));
const navigation = vi.hoisted(() => ({ push: vi.fn() }));

vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));
vi.mock('next/navigation', () => ({ useRouter: () => navigation }));

beforeEach(() => {
  auth.session = {
    id: '60000000-0000-4000-8000-000000000001',
    sessionId: 'session-finance',
    permissions: ['finance.read', 'finance.write', 'finance.approve', 'campaign.approve']
  };
  navigation.push.mockReset();
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function campaign(overrides: Partial<CampaignDetail> = {}): CampaignDetail {
  return {
    id: '10000000-0000-4000-8000-000000000001',
    organisationId: '20000000-0000-4000-8000-000000000001',
    purposeId: '30000000-0000-4000-8000-000000000001',
    consentReviewId: '40000000-0000-4000-8000-000000000001',
    name: 'Commercial campaign',
    status: 'MESSAGE_APPROVED',
    timezone: 'UTC',
    maximumUniqueRecipients: 100,
    maximumMessagesPerRecipient: 1,
    eligibleAudienceCount: 100,
    messageVersionId: '70000000-0000-4000-8000-000000000001',
    messageContentHash: 'a'.repeat(64),
    createdBy: '50000000-0000-4000-8000-000000000001',
    createdAt: '2026-10-11T07:00:00Z',
    updatedAt: '2026-10-11T07:00:00Z',
    version: 9,
    transport: {
      channel: 'WHATSAPP',
      provider: 'OPENWA',
      engine: 'BAILEYS',
      routingMode: 'SENDER_POOL',
      gatewayPoolId: '80000000-0000-4000-8000-000000000001',
      senderPoolId: '90000000-0000-4000-8000-000000000001',
      adapterVersion: '1',
      requiredCapabilities: ['SEND_TEXT'],
      fallbackMode: 'NONE',
      routingPolicyVersion: 'route-v1',
      capacityEvidenceVersion: 'capacity-v1'
    },
    ...overrides
  };
}

function notFoundResponse() {
  return new Response(JSON.stringify({
    error: 'COMMERCIAL_NOT_FOUND',
    message: 'No commercial record exists for this campaign.'
  }), { status: 404, headers: { 'content-type': 'application/json' } });
}

test('creates an offline commercial draft with server lifecycle authority and a derived total', async () => {
  let record: Record<string, unknown> | undefined;
  const calls: Array<{ path: string; init: RequestInit }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    calls.push({ path, init });

    if (path === '/v1/campaigns/10000000-0000-4000-8000-000000000001/commercial-record' && (!init.method || init.method === 'GET')) {
      if (!record) return notFoundResponse();
      return new Response(JSON.stringify(record), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/campaigns/10000000-0000-4000-8000-000000000001/commercial-record' && init.method === 'POST') {
      const body = JSON.parse(String(init.body));
      expect(body).toMatchObject({
        quotationReference: 'Q-2026-001',
        invoiceReference: 'INV-2026-001',
        currency: 'NGN',
        approvedRecipients: 100,
        unitPriceMinor: 50,
        managementFeeMinor: 1000,
        paymentReference: 'PAY-001',
        reason: 'Record offline commercial evidence'
      });
      expect(body).not.toHaveProperty('totalAmountMinor');
      expect(body).not.toHaveProperty('status');
      expect(body).not.toHaveProperty('version');
      expect(body).not.toHaveProperty('createdBy');
      expect(body).not.toHaveProperty('approvedBy');
      record = {
        id: '61000000-0000-4000-8000-000000000001',
        campaignId: '10000000-0000-4000-8000-000000000001',
        organisationId: '20000000-0000-4000-8000-000000000001',
        ...body,
        totalAmountMinor: 6000,
        status: 'DRAFT',
        version: 1,
        createdBy: auth.session.id,
        reason: body.reason,
        createdAt: '2026-10-11T08:00:00Z',
        updatedAt: '2026-10-11T08:00:00Z'
      };
      return new Response(JSON.stringify(record), { status: 201, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/commercial-records/61000000-0000-4000-8000-000000000001/events') {
      return new Response(JSON.stringify({
        items: [{
          id: 'event-1', commercialRecordId: '61000000-0000-4000-8000-000000000001',
          eventType: 'CREATED', actorId: auth.session.id, reason: 'Record offline commercial evidence',
          recordVersion: 1, occurredAt: '2026-10-11T08:00:00Z'
        }],
        count: 1, hasMore: false
      }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + path + ' ' + String(init.method));
  });

  render(<CommercialGovernance campaign={campaign()} pilotEvidenceReady={true} onChanged={vi.fn(async () => {})} />);
  await screen.findByText('No commercial record');

  const form = screen.getByRole('button', { name: 'Create commercial draft' }).closest('form')!;
  const scoped = within(form);
  fireEvent.change(scoped.getByLabelText('Quotation reference'), { target: { value: 'Q-2026-001' } });
  fireEvent.change(scoped.getByLabelText('Invoice reference'), { target: { value: 'INV-2026-001' } });
  fireEvent.change(scoped.getByLabelText('Currency'), { target: { value: 'ngn' } });
  fireEvent.change(scoped.getByLabelText('Approved recipients'), { target: { value: '100' } });
  fireEvent.change(scoped.getByLabelText('Unit price (minor units)'), { target: { value: '50' } });
  fireEvent.change(scoped.getByLabelText('Management fee (minor units)'), { target: { value: '1000' } });
  fireEvent.change(scoped.getByLabelText('Payment reference'), { target: { value: 'PAY-001' } });
  fireEvent.change(scoped.getByLabelText('Evidence reason'), { target: { value: 'Record offline commercial evidence' } });
  fireEvent.submit(form);

  await screen.findByText(/Create commercial draft completed/i);
  expect(await screen.findByText('Q-2026-001')).toBeTruthy();
  expect(calls.some(call => call.path.endsWith('/commercial-record') && call.init.method === 'POST')).toBe(true);
});

test('keeps finance approval separate from the campaign transition and requires pilot evidence to advance', async () => {
  auth.session = {
    id: '63000000-0000-4000-8000-000000000001',
    sessionId: 'session-checker',
    permissions: ['finance.read', 'finance.write', 'finance.approve', 'campaign.approve']
  };
  let record = {
    id: '62000000-0000-4000-8000-000000000001',
    campaignId: '10000000-0000-4000-8000-000000000001',
    organisationId: '20000000-0000-4000-8000-000000000001',
    quotationReference: 'Q-2',
    invoiceReference: 'I-2',
    currency: 'NGN',
    approvedRecipients: 100,
    unitPriceMinor: 50,
    managementFeeMinor: 1000,
    totalAmountMinor: 6000,
    paymentReference: 'PAY-2',
    paymentReceivedAt: '2026-10-11T07:30:00Z',
    status: 'PENDING_APPROVAL',
    version: 2,
    createdBy: '60000000-0000-4000-8000-000000000001',
    submittedBy: '64000000-0000-4000-8000-000000000001',
    approvedBy: undefined as string | undefined,
    reason: 'submit for finance approval',
    createdAt: '2026-10-11T07:00:00Z',
    updatedAt: '2026-10-11T07:30:00Z'
  };
  const calls: Array<{ path: string; init: RequestInit }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    calls.push({ path, init });
    if (path.endsWith('/commercial-record') && (!init.method || init.method === 'GET')) {
      return new Response(JSON.stringify(record), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/commercial-records/62000000-0000-4000-8000-000000000001/events') {
      return new Response(JSON.stringify({ items: [], count: 0, hasMore: false }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/commercial-records/62000000-0000-4000-8000-000000000001/decision' && init.method === 'POST') {
      const body = JSON.parse(String(init.body));
      expect(body).toEqual({ expectedVersion: 2, approve: true, reason: 'Independent payment and entitlement verification' });
      record = { ...record, status: 'APPROVED', version: 3, approvedBy: auth.session.id, reason: body.reason, updatedAt: '2026-10-11T08:00:00Z' };
      return new Response(JSON.stringify(record), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/campaigns/10000000-0000-4000-8000-000000000001/transition' && init.method === 'POST') {
      const body = JSON.parse(String(init.body));
      expect(body).toMatchObject({ action: 'APPROVE_COMMERCIAL', expectedVersion: 9 });
      return new Response(JSON.stringify({}), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + path + ' ' + String(init.method));
  });

  const onChanged = vi.fn(async () => {});
  const view = render(<CommercialGovernance campaign={campaign()} pilotEvidenceReady={false} onChanged={onChanged} />);
  const decision = await screen.findByRole('heading', { name: 'Independent finance decision' });
  const form = decision.closest('form')!;
  fireEvent.change(within(form).getByLabelText('Decision reason'), { target: { value: 'Independent payment and entitlement verification' } });
  fireEvent.click(within(form).getByRole('button', { name: 'Approve commercial evidence' }));

  await screen.findByText(/Approve commercial evidence completed/i);
  let transitionButton = await screen.findByRole('button', { name: 'Record campaign commercial approval' });
  expect((transitionButton as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByText(/Complete an accepted exact-route controlled test and authoritative capacity hold/i)).toBeTruthy();

  view.rerender(<CommercialGovernance campaign={campaign()} pilotEvidenceReady={true} onChanged={onChanged} />);
  transitionButton = await screen.findByRole('button', { name: 'Record campaign commercial approval' });
  expect((transitionButton as HTMLButtonElement).disabled).toBe(false);
  fireEvent.click(transitionButton);

  await screen.findByText(/Record campaign commercial approval completed/i);
  expect(onChanged).toHaveBeenCalledTimes(1);
  expect(calls.some(call => call.path.endsWith('/transition') && call.init.method === 'POST')).toBe(true);
});


test('a revoked commercial approval reopens as the same governed correction workflow', async () => {
  const revoked = {
    id: '65000000-0000-4000-8000-000000000001',
    campaignId: '10000000-0000-4000-8000-000000000001',
    organisationId: '20000000-0000-4000-8000-000000000001',
    quotationReference: 'Q-REVOKED',
    invoiceReference: 'I-REVOKED',
    currency: 'NGN',
    approvedRecipients: 100,
    unitPriceMinor: 50,
    managementFeeMinor: 0,
    totalAmountMinor: 5000,
    paymentReference: 'PAY-REVOKED',
    paymentReceivedAt: '2026-10-11T07:30:00Z',
    status: 'REVOKED',
    version: 4,
    createdBy: '60000000-0000-4000-8000-000000000001',
    submittedBy: '64000000-0000-4000-8000-000000000001',
    approvedBy: '63000000-0000-4000-8000-000000000001',
    reason: 'revoked obsolete commercial authority',
    createdAt: '2026-10-11T07:00:00Z',
    updatedAt: '2026-10-11T08:30:00Z'
  };
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    if (path.endsWith('/commercial-record') && (!init.method || init.method === 'GET')) {
      return new Response(JSON.stringify(revoked), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/commercial-records/65000000-0000-4000-8000-000000000001/events') {
      return new Response(JSON.stringify({ items: [], count: 0, hasMore: false }), {
        status: 200, headers: { 'content-type': 'application/json' }
      });
    }
    throw new Error('Unexpected request: ' + path + ' ' + String(init.method));
  });

  render(<CommercialGovernance campaign={campaign({ status: 'COMMERCIAL_APPROVED' })} pilotEvidenceReady={true} onChanged={vi.fn(async () => {})} />);

  const save = await screen.findByRole('button', { name: 'Save corrected draft' });
  const form = save.closest('form');
  expect(form).not.toBeNull();
  expect((within(form as HTMLFormElement).getByLabelText('Quotation reference') as HTMLInputElement).value).toBe('Q-REVOKED');
  expect(screen.getByText('REVOKED')).toBeTruthy();
});
