import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { CampaignClonePanel } from '../app/campaigns/[id]/campaign-clone-panel';
import type { CampaignDetail } from '../lib/campaign-preparation-model';

const auth = vi.hoisted(() => ({
  session: { id: 'operator-one', sessionId: 'session-one', permissions: ['campaign.read', 'campaign.write'] }
}));
vi.mock('../components/auth-provider', () => ({ useAuth: () => ({ session: auth.session }) }));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  auth.session = { id:'operator-one',sessionId:'session-one',permissions:['campaign.read','campaign.write'] };
});

const source: CampaignDetail = {
  id: 'campaign-source', name:'Autumn subscribers', organisationId:'organisation-a',
  purposeId:'purpose-a', consentReviewId:'consent-old', status:'COMPLETED',
  requestedStartAt:'2026-10-01T11:00:00Z',
  completionDeadlineAt:'2026-10-10T18:00:00Z',
  timezone:'Africa/Lagos', quietHoursStart:'21:00', quietHoursEnd:'08:00',
  maximumUniqueRecipients:500, maximumMessagesPerRecipient:1,
  audienceSnapshotId:'snapshot-sensitive', messageVersionId:'message-approved',
  commercialApprovalId:'commercial-approved', finalApprovedBy:'checker-one',
  eligibleAudienceCount:450,
  transport:{channel:'WHATSAPP',provider:'OPENWA',engine:'BAILEYS',routingMode:'SENDER_POOL',
    adapterVersion:'a1',fallbackMode:'NONE',routingPolicyVersion:'policy-1',capacityEvidenceVersion:'capacity-1'},
  createdBy:'maker-one', createdAt:'2026-09-25T10:00:00Z',updatedAt:'2026-10-10T19:00:00Z',version:8
};

function fillValidForm() {
  const form = screen.getByRole('heading', { name: 'Clone into a new draft' }).closest('section')!.querySelector('form')!;
  const controls=within(form);
  fireEvent.change(controls.getByLabelText('New campaign name'), { target:{value:'November subscribers'} });
  fireEvent.change(controls.getByLabelText('New requested start (UTC)'), { target:{value:'2099-11-10T10:00'} });
  fireEvent.change(controls.getByLabelText('New completion deadline (UTC)'), { target:{value:'2099-11-11T18:00'} });
  fireEvent.change(controls.getByLabelText('Maximum unique recipients'), { target:{value:'900'} });
  fireEvent.click(controls.getByLabelText(/I understand the new draft requires fresh operational evidence/));
  return controls;
}

test('creates a separate DRAFT with explicit new UTC window and never sends inherited release authority', async () => {
  const calls: Array<{path:string;body:Record<string,unknown>}> = [];
  vi.stubGlobal('fetch', async (input:RequestInfo|URL,init:RequestInit={})=>{
    const path=String(input).replace('/api','');
    if(path==='/v1/campaigns/campaign-source/clone'&&init.method==='POST'){
      calls.push({path,body:JSON.parse(String(init.body))});
      return new Response(JSON.stringify({
        ...source,id:'campaign-new',name:'November subscribers',status:'DRAFT',
        createdBy:'operator-one',
        requestedStartAt:'2099-11-10T10:00:00.000Z',
        completionDeadlineAt:'2099-11-11T18:00:00.000Z',
        audienceSnapshotId:'',audienceSnapshotHash:'',
        messageVersionId:'',messageContentHash:'',
        commercialApprovalId:'',finalApprovedBy:'',
        eligibleAudienceCount:0,version:1
      }),{status:201,headers:{'content-type':'application/json'}});
    }
    throw new Error('Unexpected '+path);
  });
  render(<CampaignClonePanel campaign={source} canWrite />);
  const button=screen.getByRole('button',{name:'Create separate draft'}) as HTMLButtonElement;
  expect(button.disabled).toBe(true);
  expect(screen.getByText(/possibly expired dates/)).toBeTruthy();
  fillValidForm();
  expect(button.disabled).toBe(false);
  fireEvent.click(button);
  expect(await screen.findByText(/New DRAFT confirmed/)).toBeTruthy();
  expect(screen.getByRole('link',{name:'Open cloned draft'}).getAttribute('href')).toBe('/campaigns/campaign-new');
  expect(calls).toHaveLength(1);
  expect(calls[0].body).toEqual({
    name:'November subscribers',
    requestedStartAt:'2099-11-10T10:00:00.000Z',
    completionDeadlineAt:'2099-11-11T18:00:00.000Z',
    timezone:'Africa/Lagos',
    quietHoursStart:'21:00',
    quietHoursEnd:'08:00',
    maximumUniqueRecipients:900
  });
});

test('read-only operators cannot clone, and loss of campaign.write hides the form immediately', () => {
  auth.session={id:'read-only',sessionId:'s1',permissions:['campaign.read']};
  const mock=vi.fn();
  vi.stubGlobal('fetch',mock);
  const view=render(<CampaignClonePanel campaign={source} canWrite={false} />);
  expect(screen.getByText(/Cloning requires campaign.write permission/)).toBeTruthy();
  expect(screen.queryByRole('button',{name:'Create separate draft'})).toBeNull();
  auth.session={id:'read-only',sessionId:'s1',permissions:['campaign.read','campaign.write']};
  view.rerender(<CampaignClonePanel campaign={source} canWrite />);
  expect(screen.getByRole('button',{name:'Create separate draft'})).toBeTruthy();
  auth.session={id:'read-only',sessionId:'s1',permissions:['campaign.read']};
  view.rerender(<CampaignClonePanel campaign={source} canWrite={false} />);
  expect(screen.queryByRole('button',{name:'Create separate draft'})).toBeNull();
  expect(mock).not.toHaveBeenCalled();
});

test('lost POST response is treated as uncertain and never causes a duplicate clone retry', async () => {
  let calls=0;
  vi.stubGlobal('fetch',async (_:RequestInfo|URL,init:RequestInit={})=>{
    if(init.method==='POST'){calls++;throw new Error('Network response lost');}
    throw new Error('Unexpected GET');
  });
  render(<CampaignClonePanel campaign={source} canWrite />);
  const controls=fillValidForm();
  fireEvent.click(controls.getByRole('button',{name:'Create separate draft'}));
  expect(await screen.findByText(/Clone outcome is unconfirmed/)).toBeTruthy();
  expect(calls).toBe(1);
  expect((controls.getByRole('button',{name:'Create separate draft'}) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(controls.getByRole('button',{name:'Create separate draft'}));
  expect(calls).toBe(1);
});

test('a clone returning inherited approvals or source identity is not claimed as a safe DRAFT', async () => {
  let calls=0;
  vi.stubGlobal('fetch',async (_:RequestInfo|URL,init:RequestInit={})=>{
    if(init.method==='POST'){
      calls++;
      return new Response(JSON.stringify({...source,id:'campaign-other',status:'DRAFT'}),
        {status:201,headers:{'content-type':'application/json'}});
    }
    throw new Error('Unexpected GET');
  });
  render(<CampaignClonePanel campaign={source} canWrite />);
  const controls=fillValidForm();
  fireEvent.click(controls.getByRole('button',{name:'Create separate draft'}));
  expect(await screen.findByText(/did not prove a fresh, unapproved DRAFT/)).toBeTruthy();
  expect(screen.queryByRole('link',{name:'Open cloned draft'})).toBeNull();
  expect(calls).toBe(1);
});

test('rejects old windows and reversed deadlines without sending a clone request', () => {
  const fetchMock=vi.fn();
  vi.stubGlobal('fetch',fetchMock);
  render(<CampaignClonePanel campaign={source} canWrite />);
  const controls=within(screen.getByRole('heading',{name:'Clone into a new draft'}).closest('section')!.querySelector('form')!);
  fireEvent.change(controls.getByLabelText('New requested start (UTC)'),{target:{value:'2026-10-01T10:00'}});
  fireEvent.change(controls.getByLabelText('New completion deadline (UTC)'),{target:{value:'2026-10-02T10:00'}});
  fireEvent.click(controls.getByLabelText(/I understand the new draft requires fresh operational evidence/));
  expect((controls.getByRole('button',{name:'Create separate draft'}) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(controls.getByLabelText('New requested start (UTC)'),{target:{value:'2099-11-20T10:00'}});
  fireEvent.change(controls.getByLabelText('New completion deadline (UTC)'),{target:{value:'2099-11-10T10:00'}});
  expect((controls.getByRole('button',{name:'Create separate draft'}) as HTMLButtonElement).disabled).toBe(true);
  expect(fetchMock).not.toHaveBeenCalled();
});
