import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { FinanceApprovalDetail } from '../app/work/finance/[campaignId]/finance-approval-detail';

const auth = vi.hoisted(() => ({
  session: {id:'checker-one',sessionId:'session-1',permissions:['finance.read','finance.approve']}
}));
const navigation=vi.hoisted(()=>({push:vi.fn()}));
vi.mock('next/navigation',()=>({useRouter:()=>navigation}));
vi.mock('../components/auth-provider',()=>({useAuth:()=>({session:auth.session})}));

afterEach(()=>{
  cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); navigation.push.mockReset();
  auth.session={id:'checker-one',sessionId:'session-1',permissions:['finance.read','finance.approve']};
});
const record={
  id:'record-1',campaignId:'campaign-1',organisationId:'org-1',status:'PENDING_APPROVAL',version:4,
  quotationReference:'QUOTE-1',invoiceReference:'INV-1',currency:'NGN',approvedRecipients:250,
  totalAmountMinor:25000,paymentReference:'PAY-1',createdBy:'maker-1',submittedBy:'maker-2',
  updatedAt:'2026-10-13T12:00:00Z'
};
function ok(data:unknown,status=200){return new Response(JSON.stringify(data),{status,headers:{'content-type':'application/json'}});}

test('finance-only checker can review detail and event history without needing campaign.read',async()=>{
  auth.session={id:'reader-one',sessionId:'reader-session',permissions:['finance.read']};
  const calls:string[]=[];
  vi.stubGlobal('fetch',async(input:RequestInfo|URL)=>{
    const path=String(input).replace('/api','');calls.push(path);
    if(path==='/v1/campaigns/campaign-1/commercial-record')return ok(record);
    if(path==='/v1/commercial-records/record-1/events?limit=50')return ok({items:[{
      id:'event-1',eventType:'COMMERCIAL_SUBMITTED',recordVersion:4,actorId:'maker-2',reason:'Invoice verified',occurredAt:'2026-10-13T11:00:00Z',
      evidence:{restricted:'DO NOT RENDER RAW SNAPSHOT'}
    }],count:1,hasMore:false});
    throw new Error('Unexpected '+path);
  });
  render(<FinanceApprovalDetail campaignId="campaign-1"/>);
  expect(await screen.findByText('INV-1')).toBeTruthy();
  expect(await screen.findByText('COMMERCIAL_SUBMITTED')).toBeTruthy();
  expect(screen.queryByText('DO NOT RENDER RAW SNAPSHOT')).toBeNull();
  expect(screen.getByText(/A finance reader can inspect this record without campaign.read/)).toBeTruthy();
  expect(screen.queryByRole('button',{name:'Approve evidence'})).toBeNull();
  expect(calls).toEqual(['/v1/campaigns/campaign-1/commercial-record','/v1/commercial-records/record-1/events?limit=50']);
});

test('independent checker sends reasoned expectedVersion decision and refreshes authoritative data',async()=>{
  const writes:Array<{path:string;body:unknown}>=[];
  let current={...record};
  vi.stubGlobal('fetch',async(input:RequestInfo|URL,init:RequestInit={})=>{
    const path=String(input).replace('/api','');
    if(path==='/v1/campaigns/campaign-1/commercial-record')return ok(current);
    if(path==='/v1/commercial-records/record-1/events?limit=50')return ok({items:[],count:0,hasMore:false});
    if(path==='/v1/commercial-records/record-1/decision'&&init.method==='POST'){
      writes.push({path,body:JSON.parse(String(init.body))});
      current={...record,status:'APPROVED',version:5};
      return ok(current);
    }
    throw new Error('Unexpected '+path);
  });
  render(<FinanceApprovalDetail campaignId="campaign-1"/>);
  const form=(await screen.findByRole('heading',{name:'Independent finance decision'})).closest('form')!;
  const controls=within(form);
  expect((controls.getByRole('button',{name:'Approve evidence'}) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(controls.getByLabelText('Decision reason'),{target:{value:'Verified payment and invoice evidence independently'}});
  fireEvent.click(controls.getByRole('button',{name:'Approve evidence'}));
  await waitFor(()=>expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({path:'/v1/commercial-records/record-1/decision',body:{
    expectedVersion:4,approve:true,reason:'Verified payment and invoice evidence independently'
  }});
  expect(await screen.findByText(/Approved and confirmed by server readback/)).toBeTruthy();
});

test('maker-checker separation disables self approval',async()=>{
  auth.session={id:'maker-2',sessionId:'maker-session',permissions:['finance.read','finance.approve']};
  vi.stubGlobal('fetch',async(input:RequestInfo|URL)=>{
    const path=String(input).replace('/api','');
    if(path==='/v1/campaigns/campaign-1/commercial-record')return ok(record);
    if(path==='/v1/commercial-records/record-1/events?limit=50')return ok({items:[],count:0,hasMore:false});
    throw new Error('Unexpected '+path);
  });
  render(<FinanceApprovalDetail campaignId="campaign-1"/>);
  expect(await screen.findByText(/Your submission must be decided by another finance checker/)).toBeTruthy();
  const form=screen.getByRole('heading',{name:'Independent finance decision'}).closest('form')!;
  fireEvent.change(within(form).getByLabelText('Decision reason'),{target:{value:'Would otherwise approve own request'}});
  expect((within(form).getByRole('button',{name:'Approve evidence'}) as HTMLButtonElement).disabled).toBe(true);
});

test('a version conflict disables further decisions until newer server evidence is observed',async()=>{
  let writes=0;
  let current={...record};
  vi.stubGlobal('fetch',async(input:RequestInfo|URL,init:RequestInit={})=>{
    const path=String(input).replace('/api','');
    if(path==='/v1/campaigns/campaign-1/commercial-record')return ok(current);
    if(path==='/v1/commercial-records/record-1/events?limit=50')return ok({items:[],count:0,hasMore:false});
    if(path.endsWith('/decision')&&init.method==='POST'){writes++;return ok({code:'COMMERCIAL_CONFLICT',message:'Changed by another finance operator'},409);}
    throw new Error('Unexpected '+path);
  });
  render(<FinanceApprovalDetail campaignId="campaign-1"/>);
  const form=(await screen.findByRole('heading',{name:'Independent finance decision'})).closest('form')!;
  fireEvent.change(within(form).getByLabelText('Decision reason'),{target:{value:'Evidence independently verified'}});
  fireEvent.click(within(form).getByRole('button',{name:'Approve evidence'}));
  expect(await screen.findByText(/changed since the version you reviewed/)).toBeTruthy();
  expect((within(form).getByRole('button',{name:'Approve evidence'}) as HTMLButtonElement).disabled).toBe(true);
  current={...record,version:5};
  fireEvent.click(screen.getByRole('button',{name:'Refresh commercial evidence'}));
  await waitFor(()=>expect((within(form).getByRole('button',{name:'Approve evidence'}) as HTMLButtonElement).disabled).toBe(false));
  expect(writes).toBe(1);
});

test('finance record and decision controls are removed immediately on session permission loss',async()=>{
  vi.stubGlobal('fetch',async(input:RequestInfo|URL)=>{
    const path=String(input).replace('/api','');
    if(path==='/v1/campaigns/campaign-1/commercial-record')return ok(record);
    if(path==='/v1/commercial-records/record-1/events?limit=50')return ok({items:[],count:0,hasMore:false});
    throw new Error('Unexpected '+path);
  });
  const view=render(<FinanceApprovalDetail campaignId="campaign-1"/>);
  expect(await screen.findByText('INV-1')).toBeTruthy();
  auth.session={id:'new-operator',sessionId:'new-session',permissions:[]};
  view.rerender(<FinanceApprovalDetail campaignId="campaign-1"/>);
  expect(screen.queryByText('INV-1')).toBeNull();
  expect(screen.getByText('Commercial review requires finance.read permission.')).toBeTruthy();
});

test('a lost finance decision response locks actions and does not retry an uncertain approval',async()=>{
  let posts=0;
  let current={...record};
  vi.stubGlobal('fetch',async(input:RequestInfo|URL,init:RequestInit={})=>{
    const path=String(input).replace('/api','');
    if(path==='/v1/campaigns/campaign-1/commercial-record')return ok(current);
    if(path==='/v1/commercial-records/record-1/events?limit=50')return ok({items:[],count:0,hasMore:false});
    if(path==='/v1/commercial-records/record-1/decision'&&init.method==='POST'){
      posts++;
      throw new Error('Network response lost');
    }
    throw new Error('Unexpected '+path);
  });
  render(<FinanceApprovalDetail campaignId="campaign-1"/>);
  const form=(await screen.findByRole('heading',{name:'Independent finance decision'})).closest('form')!;
  const controls=within(form);
  fireEvent.change(controls.getByLabelText('Decision reason'),{target:{value:'Independent approval with payment evidence checked'}});
  fireEvent.click(controls.getByRole('button',{name:'Approve evidence'}));
  expect(await screen.findByText('Network response lost')).toBeTruthy();
  expect((controls.getByRole('button',{name:'Approve evidence'}) as HTMLButtonElement).disabled).toBe(true);
  expect(posts).toBe(1);
  fireEvent.click(screen.getByRole('button',{name:'Refresh commercial evidence'}));
  await waitFor(() => expect((screen.getByRole('button',{name:'Refresh commercial evidence'}) as HTMLButtonElement).disabled).toBe(false));
  expect((controls.getByRole('button',{name:'Approve evidence'}) as HTMLButtonElement).disabled).toBe(true);
  current={...record,version:5,status:'APPROVED'};
  fireEvent.click(screen.getByRole('button',{name:'Refresh commercial evidence'}));
  expect(await screen.findByText('APPROVED')).toBeTruthy();
  expect(posts).toBe(1);
});

test('server readback must match the requested decision, not merely increment a version', async () => {
  let current={...record};
  let writes=0;
  vi.stubGlobal('fetch',async(input:RequestInfo|URL,init:RequestInit={})=>{
    const path=String(input).replace('/api','');
    if(path==='/v1/campaigns/campaign-1/commercial-record')return ok(current);
    if(path==='/v1/commercial-records/record-1/events?limit=50')return ok({items:[],count:0,hasMore:false});
    if(path==='/v1/commercial-records/record-1/decision'&&init.method==='POST'){
      writes++;
      current={...record,version:5,status:'REJECTED'};
      return ok(current);
    }
    throw new Error('Unexpected '+path);
  });
  render(<FinanceApprovalDetail campaignId="campaign-1"/>);
  const form=(await screen.findByRole('heading',{name:'Independent finance decision'})).closest('form')!;
  fireEvent.change(within(form).getByLabelText('Decision reason'),{target:{value:'Finance evidence checked independently'}});
  fireEvent.click(within(form).getByRole('button',{name:'Approve evidence'}));
  expect(await screen.findByText(/Decision was submitted but readback is unconfirmed/)).toBeTruthy();
  expect(screen.queryByText('Approved and confirmed by server readback.')).toBeNull();
  expect(writes).toBe(1);
});
