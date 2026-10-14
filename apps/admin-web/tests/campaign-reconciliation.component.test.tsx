import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { CompletionReconciliationPanel } from '../app/campaigns/[id]/reconciliation/completion-reconciliation-panel';

const auth=vi.hoisted(()=>({session:{
  id:'operator-1',sessionId:'session-1',permissions:['campaign.read','campaign.operate','report.read','finance.read','operations.read']
}}));
vi.mock('../components/auth-provider',()=>({useAuth:()=>({session:auth.session})}));
vi.mock('next/navigation',()=>({useRouter:()=>({push:vi.fn()})}));
const cid='campaign-1';
const campaign={id:cid,name:'Lagos campaign',status:'DISPATCHING',version:7,eligibleAudienceCount:10,updatedAt:'2026-10-13T11:00:00Z'};
const metrics={authorisedTotal:0,queuedTotal:0,submittedTotal:0,gatewayAcceptedTotal:0,sentTotal:0,
  deliveredTotal:10,readTotal:0,failedTotal:0,unknownTotal:0,suppressedTotal:0,excludedFinalCheckTotal:0};
const report={campaignId:cid,status:'DISPATCHING',generatedAt:'2026-10-13T11:30:00Z',warnings:[],
  privacy:{policyId:'privacy-1',policyVersion:2}};
const finance={campaignId:cid,invoiceReference:'invoice-1',reconciliationStatus:'BALANCED',approvedRecipients:10,
  recipientVariance:0,warnings:[],generatedAt:'2026-10-13T11:30:00Z'};
function json(value:unknown,status=200){return new Response(JSON.stringify(value),{status,headers:{'content-type':'application/json'}});}
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks();
  auth.session={id:'operator-1',sessionId:'session-1',permissions:['campaign.read','campaign.operate','report.read','finance.read','operations.read']};
});
function http(overrides:Record<string,unknown>={}, onPost?:()=>unknown) {
  const calls:Array<{path:string;method:string;body?:string}>=[];
  vi.stubGlobal('fetch',async(input:RequestInfo|URL,init:RequestInit={})=>{
    const path=String(input).replace('/api','');const method=(init.method||'GET').toUpperCase();
    calls.push({path,method,body:typeof init.body==='string'?init.body:undefined});
    if(method==='POST'&&path==='/v1/campaigns/campaign-1/execution/complete'){
      if(!onPost)throw Error('Unexpected completion request');
      return json(await onPost());
    }
    const defaults:Record<string,unknown>={
      '/v1/campaigns/campaign-1':campaign,
      '/v1/campaigns/campaign-1/metrics':metrics,
      '/v1/campaigns/campaign-1/report':report,
      '/v1/campaigns/campaign-1/financial-reconciliation':finance,
      '/v1/operations/delivery-exceptions?campaignId=campaign-1&limit=50':{items:[],count:0,hasMore:false}
    };
    if(Object.prototype.hasOwnProperty.call(overrides,path))return json(overrides[path]);
    if(Object.prototype.hasOwnProperty.call(defaults,path))return json(defaults[path]);
    throw Error('Unexpected '+method+' '+path);
  });
  return calls;
}

test('reader sees only campaign detail and metrics, without calling inaccessible finance/report/operations APIs',async()=>{
  auth.session={id:'reader',sessionId:'one',permissions:['campaign.read']};
  const calls=http();
  render(<CompletionReconciliationPanel campaignId={cid}/>);
  expect(await screen.findByText('10 accounted recipients')).toBeTruthy();
  expect(calls.map(v=>v.path)).toEqual(['/v1/campaigns/campaign-1','/v1/campaigns/campaign-1/metrics']);
  expect(screen.getByText('Financial reconciliation requires finance.read permission.')).toBeTruthy();
  expect(screen.getByText('Delivery exception details require operations.read permission.')).toBeTruthy();
  expect(screen.queryByRole('button',{name:'Request server completion assessment'})).toBeNull();
});

test('active obligations or mismatched counts block close-out',async()=>{
  http({'/v1/campaigns/campaign-1/metrics':{...metrics,queuedTotal:2,deliveredTotal:6}});
  render(<CompletionReconciliationPanel campaignId={cid}/>);
  expect(await screen.findByText(/2 active delivery obligations remain/)).toBeTruthy();
  expect(screen.getByText(/Delivery totals are provisional/)).toBeTruthy();
  expect((screen.getByRole('button',{name:'Request server completion assessment'}) as HTMLButtonElement).disabled).toBe(true);
});

test('a server IN_PROGRESS assessment never becomes a false completion, and no automatic retry is sent',async()=>{
  let attempts=0;
  const calls=http({},()=>{attempts++;return {campaign:{},assessment:{state:'IN_PROGRESS',outstanding:2,failed:0,unknown:0}};});
  render(<CompletionReconciliationPanel campaignId={cid}/>);
  fireEvent.click(await screen.findByLabelText('I understand completion is assessed and controlled by the server'));
  fireEvent.click(screen.getByRole('button',{name:'Request server completion assessment'}));
  expect(await screen.findByText(/Server assessed the campaign as still in progress or unconfirmed/)).toBeTruthy();
  expect(calls.filter(v=>v.method==='POST')).toHaveLength(1);
  expect(JSON.parse(String(calls.find(v=>v.method==='POST')?.body))).toMatchObject({expectedVersion:7});
  expect((screen.getByRole('button',{name:'Request server completion assessment'}) as HTMLButtonElement).disabled).toBe(true);
  expect(attempts).toBe(1);
});

test('exceptional completion requires a matching server assessment and final readback',async()=>{
  let current={...campaign};
  const exceptional={...metrics,deliveredTotal:7,failedTotal:1,unknownTotal:2};
  const calls:Array<{path:string;method:string;body?:string}>=[];
  vi.stubGlobal('fetch',async(input:RequestInfo|URL,init:RequestInit={})=>{
    const path=String(input).replace('/api','');const method=(init.method||'GET').toUpperCase();
    calls.push({path,method,body:typeof init.body==='string'?init.body:undefined});
    if(path==='/v1/campaigns/campaign-1')return json(current);
    if(path.endsWith('/metrics'))return json(exceptional);
    if(path.endsWith('/report'))return json({...report,warnings:['Unknown outcomes require reconciliation']});
    if(path.endsWith('/financial-reconciliation'))return json({...finance,reconciliationStatus:'UNDER_ALLOCATED'});
    if(path.includes('delivery-exceptions?'))return json({items:[{
      recipientId:'private-recipient-identifier',campaignId:cid,status:'UNKNOWN',attemptCount:1,updatedAt:'2026-10-13T12:00:00Z'
    }],hasMore:false,count:1});
    if(path.endsWith('/execution/complete')&&method==='POST'){
      current={...campaign,version:8,status:'COMPLETED_WITH_EXCEPTIONS'};
      return json({campaign:current,assessment:{state:'READY_WITH_EXCEPTIONS',outstanding:0,failed:1,unknown:2}});
    }
    throw Error('Unexpected '+path);
  });
  render(<CompletionReconciliationPanel campaignId={cid}/>);
  expect(await screen.findByText(/2 UNKNOWN outcomes require governed reconciliation/)).toBeTruthy();
  expect(screen.queryByText('private-recipient-identifier')).toBeNull();
  fireEvent.click(screen.getByLabelText('I understand completion is assessed and controlled by the server'));
  fireEvent.click(screen.getByRole('button',{name:'Request server completion assessment'}));
  expect(await screen.findByText(/Completion confirmed: COMPLETED WITH EXCEPTIONS/)).toBeTruthy();
  expect(calls.filter(v=>v.method==='POST')).toHaveLength(1);
  expect(screen.getByText(/UNKNOWN outcomes remain sticky/)).toBeTruthy();
});

test('lost completion response blocks repeat POSTs until authoritative evidence changes',async()=>{
  let posts=0;
  vi.stubGlobal('fetch',async(input:RequestInfo|URL,init:RequestInit={})=>{
    const path=String(input).replace('/api','');const method=(init.method||'GET').toUpperCase();
    if(path.endsWith('/execution/complete')&&method==='POST'){posts++;throw Error('Network response lost');}
    if(path==='/v1/campaigns/campaign-1')return json(campaign);
    if(path.endsWith('/metrics'))return json(metrics);
    if(path.endsWith('/report'))return json(report);
    if(path.endsWith('/financial-reconciliation'))return json(finance);
    if(path.includes('delivery-exceptions?'))return json({items:[],hasMore:false,count:0});
    throw Error('Unexpected '+path);
  });
  render(<CompletionReconciliationPanel campaignId={cid}/>);
  fireEvent.click(await screen.findByLabelText('I understand completion is assessed and controlled by the server'));
  fireEvent.click(screen.getByRole('button',{name:'Request server completion assessment'}));
  expect(await screen.findByText(/Completion outcome unconfirmed/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button',{name:'Request server completion assessment'}));
  expect(posts).toBe(1);
});

test('new operator or missing campaign permission immediately clears financial data',async()=>{
  http({'/v1/campaigns/campaign-1/financial-reconciliation':{...finance,invoiceReference:'CONFIDENTIAL-INVOICE'}});
  const rendered=render(<CompletionReconciliationPanel campaignId={cid}/>);
  expect(await screen.findByText('CONFIDENTIAL-INVOICE')).toBeTruthy();
  auth.session={id:'other',sessionId:'other-session',permissions:[]};
  rendered.rerender(<CompletionReconciliationPanel campaignId={cid}/>);
  expect(screen.getByText('Campaign reconciliation requires campaign.read permission.')).toBeTruthy();
  expect(screen.queryByText('CONFIDENTIAL-INVOICE')).toBeNull();
});
