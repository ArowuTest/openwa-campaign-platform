import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';

import { MessageComposer } from '../app/campaigns/[id]/message-composer';
import { type CampaignDetail } from '../lib/campaign-preparation-model';

const auth = vi.hoisted(() => ({
  session: { id: 'maker', sessionId: 'session-a', permissions: ['campaign.read', 'campaign.write', 'campaign.approve'] }
}));
vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));

beforeEach(() => {
  auth.session = { id: 'maker', sessionId: 'session-a', permissions: ['campaign.read', 'campaign.write', 'campaign.approve'] };
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function campaign(overrides: Partial<CampaignDetail> = {}): CampaignDetail {
  return {
    id: 'campaign-a',
    organisationId: '20000000-0000-4000-8000-000000000001',
    purposeId: '30000000-0000-4000-8000-000000000001',
    consentReviewId: '40000000-0000-4000-8000-000000000001',
    name: 'Campaign A',
    status: 'AUDIENCE_VALIDATED',
    timezone: 'UTC',
    maximumUniqueRecipients: 50,
    maximumMessagesPerRecipient: 1,
    eligibleAudienceCount: 25,
    createdBy: 'campaign-maker',
    createdAt: '2026-10-09T00:00:00Z',
    updatedAt: '2026-10-09T00:00:00Z',
    version: 7,
    transport: {
      channel: 'WHATSAPP',
      provider: 'OPENWA',
      engine: 'BAILEYS',
      routingMode: 'SENDER_POOL',
      gatewayPoolId: '50000000-0000-4000-8000-000000000001',
      senderPoolId: '60000000-0000-4000-8000-000000000001',
      adapterVersion: '1',
      requiredCapabilities: ['SEND_IMAGE'],
      fallbackMode: 'NONE',
      routingPolicyVersion: 'route-v1',
      capacityEvidenceVersion: 'capacity-v1'
    },
    ...overrides
  };
}

function envelope<T>(items: T[]) {
  return { items, count: items.length, hasMore: false };
}

type MessageVersionFixture = {
  id: string;
  campaignId: string;
  version: number;
  type: string;
  body: string;
  media?: { assetId: string; sha256: string; mediaType: string; size: number; scanStatus: string };
  links: Array<{ url: string; label?: string }>;
  variables: Array<{ name: string; dataType: string; fallback: string }>;
  contentHash: string;
  status: string;
  createdBy: string;
  approvedBy?: string;
  createdAt: string;
  approvedAt?: string;
};

test('uploads rights-governed media and creates an immutable version without forwarding private storage fields', async () => {
  const calls: Array<{ path: string; init: RequestInit }> = [];
  let versions: MessageVersionFixture[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    calls.push({ path, init });
    if (path.startsWith('/v1/campaigns/campaign-a/message-versions?')) {
      return new Response(JSON.stringify(envelope(versions)), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/assets?purpose=MESSAGE_MEDIA' && init.method === 'POST') {
      const body = init.body as FormData;
      expect(typeof body?.get).toBe('function');
      expect(body.get('rightsBasis')).toBe('LICENSED');
      expect(body.get('rightsReference')).toBe('licence/creative/001');
      return new Response(JSON.stringify({
        id: 'asset-1', purpose: 'MESSAGE_MEDIA', originalFilename: 'creative.png', mediaType: 'image/png',
        size: 10, sha256: 'a'.repeat(64), status: 'CLEAN', rightsBasis: 'LICENSED',
        rightsReference: 'licence/creative/001', version: 2, objectKey: 'private/must-not-render'
      }), { status: 201, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/campaigns/campaign-a/message-versions' && init.method === 'POST') {
      const body = JSON.parse(String(init.body));
      expect(body).toMatchObject({ type: 'IMAGE_CAPTION', body: 'Hello {{first_name}}', mediaAssetId: 'asset-1' });
      expect(body).not.toHaveProperty('objectKey');
      expect(body.variables).toEqual([{ name: 'first_name', dataType: 'PERSONAL', fallback: 'Customer' }]);
      versions = [{
        id: 'message-1', campaignId: 'campaign-a', version: 1, type: 'IMAGE_CAPTION',
        body: body.body, media: { assetId: 'asset-1', sha256: 'a'.repeat(64), mediaType: 'image/png', size: 10, scanStatus: 'CLEAN' },
        links: [], variables: body.variables, contentHash: 'b'.repeat(64), status: 'DRAFT',
        createdBy: 'maker', createdAt: '2026-10-10T00:00:00Z'
      }];
      return new Response(JSON.stringify(versions[0]), { status: 201, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + path + ' ' + String(init.method));
  });

  render(<MessageComposer campaign={campaign()} onChanged={vi.fn(async () => {})} />);
  await screen.findByText('No message versions');
  fireEvent.change(screen.getByLabelText('Message type'), { target: { value: 'IMAGE_CAPTION' } });

  const uploadHeading = await screen.findByRole('heading', { name: 'Upload governed media' });
  const uploadForm = within(uploadHeading.closest('form') as HTMLFormElement);
  const file = new File([new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10])], 'creative.png', { type: 'image/png' });
  fireEvent.change(uploadForm.getByLabelText('Media file'), { target: { files: [file] } });
  fireEvent.change(uploadForm.getByLabelText('Rights basis'), { target: { value: 'LICENSED' } });
  fireEvent.change(uploadForm.getByLabelText('Rights evidence reference'), { target: { value: 'licence/creative/001' } });
  fireEvent.submit(uploadHeading.closest('form') as HTMLFormElement);

  await screen.findByText(/Media passed malware scanning and rights validation/i);
  expect(screen.queryByText(/must-not-render/i)).toBeNull();

  const createHeading = screen.getByRole('heading', { name: 'Create immutable version' });
  const createForm = within(createHeading.closest('form') as HTMLFormElement);
  fireEvent.change(createForm.getByLabelText('Message body'), { target: { value: 'Hello {{first_name}}' } });
  fireEvent.change(createForm.getByRole('textbox', { name: /Variables/i }), { target: { value: 'first_name | PERSONAL | Customer' } });
  fireEvent.click(createForm.getByRole('button', { name: 'Create message version' }));

  await screen.findByText('Immutable message version 1 created.');
  expect(calls.some(call => call.path === '/v1/campaigns/campaign-a/message-versions' && call.init.method === 'POST')).toBe(true);
});

test('renders a server-masked preview and submits the selected draft for independent review', async () => {
  const draft = {
    id: 'message-1', campaignId: 'campaign-a', version: 1, type: 'TEXT',
    body: 'Hello {{phone}}', links: [], variables: [{ name: 'phone', dataType: 'MSISDN', fallback: '+2348012345678' }],
    contentHash: 'c'.repeat(64), status: 'DRAFT', createdBy: 'maker', createdAt: '2026-10-10T00:00:00Z'
  };
  const calls: Array<{ path: string; init: RequestInit }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    calls.push({ path, init });
    if (path.startsWith('/v1/campaigns/campaign-a/message-versions?')) {
      return new Response(JSON.stringify(envelope([draft])), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/message-versions/message-1/preview' && init.method === 'POST') {
      expect(JSON.parse(String(init.body))).toMatchObject({ maskSensitive: true });
      return new Response(JSON.stringify({
        body: 'Hello +*********5678', resolved: { phone: '+*********5678' }, usedFallbacks: ['phone'],
        missing: [], undeclared: [], characterCount: 19, byteCount: 19
      }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/campaigns/campaign-a/transition' && init.method === 'POST') {
      expect(JSON.parse(String(init.body))).toMatchObject({
        action: 'SUBMIT_MESSAGE', expectedVersion: 7, messageVersionId: 'message-1'
      });
      return new Response(JSON.stringify({ ...campaign(), status: 'MESSAGE_REVIEW_PENDING', messageVersionId: 'message-1', version: 8 }), {
        status: 200, headers: { 'content-type': 'application/json' }
      });
    }
    throw new Error('Unexpected request: ' + path + ' ' + String(init.method));
  });
  const onChanged = vi.fn(async () => {});
  render(<MessageComposer campaign={campaign()} onChanged={onChanged} />);

  await screen.findByText('v1');
  const previewForm = within(screen.getByRole('heading', { name: 'Masked representative preview' }).closest('form') as HTMLFormElement);
  fireEvent.click(previewForm.getByRole('button', { name: 'Render masked preview' }));
  await screen.findByText('Hello +*********5678');

  fireEvent.click(screen.getByRole('button', { name: 'Submit selected version for review' }));
  await screen.findByText('Message version submitted for independent review.');
  expect(onChanged).toHaveBeenCalledTimes(1);
});

test('enforces maker-checker separation and lets an independent checker complete message approval in two explicit steps', async () => {
  auth.session = { id: 'checker', sessionId: 'session-b', permissions: ['campaign.read', 'campaign.write', 'campaign.approve'] };
  let version: MessageVersionFixture = {
    id: 'message-1', campaignId: 'campaign-a', version: 1, type: 'TEXT', body: 'Approved body',
    links: [], variables: [], contentHash: 'd'.repeat(64), status: 'DRAFT',
    createdBy: 'maker', createdAt: '2026-10-10T00:00:00Z'
  };
  const calls: Array<{ path: string; init: RequestInit }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    calls.push({ path, init });
    if (path.startsWith('/v1/campaigns/campaign-a/message-versions?')) {
      return new Response(JSON.stringify(envelope([version])), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/message-versions/message-1/approve' && init.method === 'POST') {
      expect(JSON.parse(String(init.body))).toEqual({ expectedContentHash: 'd'.repeat(64) });
      version = { ...version, status: 'APPROVED', approvedBy: 'checker', approvedAt: '2026-10-10T00:05:00Z' };
      return new Response(JSON.stringify(version), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/campaigns/campaign-a/transition' && init.method === 'POST') {
      expect(JSON.parse(String(init.body))).toMatchObject({ action: 'APPROVE_MESSAGE', expectedVersion: 8 });
      return new Response(JSON.stringify({}), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + path + ' ' + String(init.method));
  });
  const onChanged = vi.fn(async () => {});
  render(<MessageComposer campaign={campaign({ status: 'MESSAGE_REVIEW_PENDING', messageVersionId: 'message-1', messageContentHash: 'd'.repeat(64), version: 8 })} onChanged={onChanged} />);

  const approve = await screen.findByRole('button', { name: 'Approve selected version' });
  fireEvent.click(approve);
  await screen.findByText(/Message version approved independently/i);
  const finalize = await screen.findByRole('button', { name: 'Record campaign message approval' });
  fireEvent.click(finalize);
  await screen.findByText(/Campaign message approval recorded/i);
  expect(onChanged).toHaveBeenCalledTimes(1);
  expect(calls.filter(call => call.path.includes('/approve')).length).toBe(1);
});
