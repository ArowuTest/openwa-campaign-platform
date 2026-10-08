import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';

import { CampaignWorkspace } from '../app/campaigns/[id]/campaign-workspace';
import { type CampaignDetail } from '../lib/campaign-preparation-model';
import { deferred, envelope, settle } from './audience-component-test-helpers';

const auth = vi.hoisted(() => ({ session: { id: 'operator', sessionId: 'session-a', permissions: ['campaign.read', 'campaign.write'] } }));
vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));
vi.mock('next/navigation', () => ({ useRouter: () => ({ push: vi.fn() }) }));
beforeEach(() => { auth.session = { id: 'operator', sessionId: 'session-a', permissions: ['campaign.read', 'campaign.write'] }; });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

function detail(id = 'campaign-a'): CampaignDetail {
 return { id, organisationId: '20000000-0000-4000-8000-000000000001', purposeId: '30000000-0000-4000-8000-000000000001',
  consentReviewId: '40000000-0000-4000-8000-000000000001', name: id+' draft', status:'DRAFT', timezone:'UTC',
  requestedStartAt:'2026-10-09T12:34:56Z', maximumUniqueRecipients:50, maximumMessagesPerRecipient:1, eligibleAudienceCount:0,
  createdBy:'maker', createdAt:'2026-10-08T00:00:00Z', updatedAt:'2026-10-08T00:00:00Z', version:3,
  transport:{channel:'WHATSAPP',provider:'OPENWA',engine:'BAILEYS',routingMode:'SENDER_POOL',gatewayPoolId:'50000000-0000-4000-8000-000000000001',
   senderPoolId:'60000000-0000-4000-8000-000000000001',adapterVersion:'v1',providerDefinitionId:'authority',providerDefinitionVersion:1,
   gatewayPoolVersion:2,requiredCapabilities:['SEND_TEXT'],fallbackMode:'NONE',routingPolicyVersion:'r1',capacityEvidenceVersion:'c1'} };
}
function response(status: number, error: string) { return new Response(JSON.stringify({error,message:'Safe save failure.'}),{status,headers:{'content-type':'application/json'}}); }
function http(route:(path:string,init:RequestInit)=>unknown|Promise<unknown>) {
 const calls:Array<{path:string;init:RequestInit}>=[];
 vi.stubGlobal('fetch',async(input:RequestInfo|URL,init:RequestInit={})=>{
  const path=String(input).replace(/^\/api/,''); calls.push({path,init});
  let value:unknown;
  if (path.endsWith('/workspace')) value={campaignId:path.split('/')[3],tags:[],archive:{archived:false,version:1}};
  else if (path.endsWith('/metrics')) value={};
  else if (path.includes('/test-messages?')||path.includes('/routing-plans?')) value=envelope([]);
  else value=await route(path,init);
  return value instanceof Response?value:new Response(JSON.stringify(value),{status:200,headers:{'content-type':'application/json'}});
 });
 return calls;
}
async function edit(name='Edited draft') {
 fireEvent.change(await screen.findByLabelText('Draft name'),{target:{value:name}});
 fireEvent.change(screen.getByLabelText('Save reason'),{target:{value:'Correct the operator draft'}});
}
test('draft save sends only the restricted full DTO and reload restores persisted values',async()=>{
 let saved=detail();
 const calls=http((path,init)=>{
  if(init.method==='PUT') {const input=JSON.parse(String(init.body)); saved={...saved,...input,version:4}; return saved;}
  return saved;
 });
 const view=render(<CampaignWorkspace campaignId="campaign-a"/>);
 await edit();
 fireEvent.change(screen.getByLabelText('Completion deadline (UTC)'),{target:{value:''}});
 fireEvent.change(screen.getByLabelText('Timezone'),{target:{value:'Africa/Lagos'}});
 fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await screen.findByText('Draft saved.');
 const put=calls.filter(c=>c.init.method==='PUT');
 expect(put).toHaveLength(1); expect(put[0].path).toBe('/v1/campaigns/campaign-a/draft');
 const dto=JSON.parse(String(put[0].init.body));
 expect(dto.name).toBe('Edited draft'); expect(dto.expectedVersion).toBe(3);
 expect(dto.requestedStartAt).toBe('2026-10-09T12:34:56Z'); expect(dto.completionDeadlineAt).toBeNull();
 expect(dto.transport).not.toHaveProperty('providerDefinitionId'); expect(dto.transport).not.toHaveProperty('gatewayPoolVersion');
 expect(dto).not.toHaveProperty('actorId'); expect(dto).not.toHaveProperty('status');
 expect(calls.filter(c=>c.init.method==='POST')).toHaveLength(0);
 view.unmount(); render(<CampaignWorkspace campaignId="campaign-a"/>);
 expect(await screen.findByLabelText('Draft name')).toHaveProperty('value','Edited draft');
});
test('failed draft save keeps unsaved values and reason',async()=>{
 http((_path,init)=>init.method==='PUT'?response(422,'CAMPAIGN_DRAFT_INVALID'):detail());
 render(<CampaignWorkspace campaignId="campaign-a"/>); await edit();
 fireEvent.click(screen.getByRole('button',{name:'Save draft'})); await screen.findByText(/Safe save failure/);
 expect(screen.getByLabelText('Draft name')).toHaveProperty('value','Edited draft');
 expect(screen.getByLabelText('Save reason')).toHaveProperty('value','Correct the operator draft');
});
test('conflict reads current server version and requires explicit reload without losing form',async()=>{
 let reads=0;
 const calls=http((_path,init)=>init.method==='PUT'?response(409,'CAMPAIGN_VERSION_CONFLICT'):{...detail(),version:++reads===1?3:4,name:reads===1?'campaign-a draft':'Other operator'});
 render(<CampaignWorkspace campaignId="campaign-a"/>); await edit();
 fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await screen.findByText(/Server version 4/);
 expect(screen.getByLabelText('Draft name')).toHaveProperty('value','Edited draft');
 expect(screen.getByRole('button',{name:'Save draft'})).toHaveProperty('disabled',true);
 fireEvent.click(screen.getByRole('button',{name:'Reload server draft'}));
 await waitFor(()=>expect(screen.getByLabelText('Draft name')).toHaveProperty('value','Other operator'));
 expect(calls.filter(c=>c.init.method==='PUT')).toHaveLength(1);
});
test.each([true,false])('uncertain PUT reconciles through one GET and never repeats PUT (persisted %s)',async(persisted)=>{
 let reads=0;
 const calls=http((_path,init)=>{
  if(init.method==='PUT') throw new TypeError('network interrupted');
  return ++reads===1||!persisted?detail():{...detail(),name:'Edited draft',version:4};
 });
 render(<CampaignWorkspace campaignId="campaign-a"/>); await edit(); fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await screen.findByText(persisted?/Submitted fields are saved/:/Submitted fields differ/);
 expect(calls.filter(c=>c.init.method==='PUT')).toHaveLength(1);
 expect(calls.filter(c=>c.path==='/v1/campaigns/campaign-a')).toHaveLength(2);
 expect(screen.getByRole('button',{name:'Save draft'})).toHaveProperty('disabled',true);
});
test('late A save cannot update B or reconcile A after campaign switch',async()=>{
 const save=deferred<unknown>(); const calls=http((path,init)=>init.method==='PUT'?save.promise:detail(path.endsWith('campaign-b')?'campaign-b':'campaign-a'));
 const view=render(<CampaignWorkspace campaignId="campaign-a"/>); await edit();
 fireEvent.click(screen.getByRole('button',{name:'Save draft'})); await waitFor(()=>expect(calls.filter(c=>c.init.method==='PUT')).toHaveLength(1));
 view.rerender(<CampaignWorkspace campaignId="campaign-b"/>); await screen.findByLabelText('Draft name');
 await settle(save,{...detail(),name:'STALE_A',version:4});
 expect(screen.getByLabelText('Draft name')).toHaveProperty('value','campaign-b draft');
 expect(screen.queryByText('Draft saved.')).toBeNull();
});
test('permission loss and session replacement clear unsaved evidence and late saves',async()=>{
 const save=deferred<unknown>(); http((_path,init)=>init.method==='PUT'?save.promise:detail());
 const view=render(<CampaignWorkspace campaignId="campaign-a"/>); await edit(); fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 auth.session={id:'operator',sessionId:'session-b',permissions:['campaign.read']}; view.rerender(<CampaignWorkspace campaignId="campaign-a"/>);
 await screen.findByRole('heading',{name:'campaign-a draft'}); expect(screen.queryByRole('button',{name:'Save draft'})).toBeNull();
 await act(async()=>save.resolve({...detail(),name:'STALE_A',version:4}));
 expect(screen.queryByText('Draft saved.')).toBeNull();
});
test('non-DRAFT and existing Meta detail expose no draft save action',async()=>{
 http(()=>({...detail(),status:'CONSENT_REVIEW_PENDING'}));
 render(<CampaignWorkspace campaignId="campaign-a"/>); await screen.findByRole('heading',{name:'campaign-a draft'});
 expect(await screen.findByText(/Draft editing is locked/)).toBeTruthy(); expect(screen.queryByRole('button',{name:'Save draft'})).toBeNull();
});

test('DRAFT with frozen eligible audience evidence exposes a locked notice and no save action',async()=>{
 const calls=http(()=>({...detail(),eligibleAudienceCount:1}));
 render(<CampaignWorkspace campaignId="campaign-a"/>);
 await screen.findByText(/Draft editing is locked/);
 expect(screen.queryByRole('button',{name:'Save draft'})).toBeNull();
 expect(calls.some(c=>c.init.method==='PUT')).toBe(false);
});

test.each([500,503])('uncertain upstream HTTP %s reconciles once and blocks further PUT until explicit reload',async(status)=>{
 let reads=0;
 const calls=http((_path,init)=>{if(init.method==='PUT')return new Response(JSON.stringify(status===503?{code:'CONTROL_API_UNAVAILABLE',message:'The control API is temporarily unavailable.'}:{error:'INTERNAL_ERROR',message:'An unexpected error occurred.'}),{status,headers:{'content-type':'application/json'}});return ++reads===1?detail():{...detail(),name:'Edited draft',version:4};});
 render(<CampaignWorkspace campaignId="campaign-a"/>);await edit();fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await screen.findByText(/Submitted fields are saved on the server/);
 expect(screen.getByRole('button',{name:'Save draft'})).toHaveProperty('disabled',true);
 expect(calls.filter(c=>c.init.method==='PUT')).toHaveLength(1);expect(reads).toBe(2);
});

test('canonical governance unavailable response preserves input without uncertain submission claims',async()=>{
 let reads=0;const calls=http((_path,init)=>{if(init.method==='PUT')return response(503,'CAMPAIGN_DRAFT_GOVERNANCE_UNAVAILABLE');reads++;return detail();});
 render(<CampaignWorkspace campaignId="campaign-a"/>);await edit();fireEvent.click(screen.getByRole('button',{name:'Save draft'}));await screen.findByText('Safe save failure.');
 expect(screen.getByLabelText('Draft name')).toHaveProperty('value','Edited draft');expect(screen.getByLabelText('Save reason')).toHaveProperty('value','Correct the operator draft');
 expect(reads).toBe(1);expect(calls.filter(c=>c.init.method==='PUT')).toHaveLength(1);expect(screen.queryByText(/Save response was uncertain/)).toBeNull();
});

test.each([
 ['start microseconds differ', '2026-10-09T12:34:56.123456Z', '2026-10-09T12:34:56.123999Z', '2026-10-09T13:34:56.123456Z', '2026-10-09T13:34:56.123456Z', false],
 ['deadline microseconds differ', '2026-10-09T12:34:56.123456Z', '2026-10-09T12:34:56.123456Z', '2026-10-09T13:34:56.123456Z', '2026-10-09T13:34:56.123999Z', false],
 ['equivalent offsets and padded fractions', '2026-10-09T14:34:56.123456000+02:00', '2026-10-09T12:34:56.123456Z', '2026-10-09T08:34:56.1234560-05:00', '2026-10-09T13:34:56.123456Z', true],
 ['PostgreSQL microsecond rounding remains an explicit mismatch', '2026-10-09T12:34:56.123456789Z', '2026-10-09T12:34:56.123457Z', '2026-10-09T13:34:56.123456Z', '2026-10-09T13:34:56.123456Z', false],
] as const)('uncertain PUT compares the complete RFC3339 instant: %s',async(_label,start,savedStart,deadline,savedDeadline,matches)=>{
 let reads=0;
 const original={...detail(),requestedStartAt:start,completionDeadlineAt:deadline};
 const calls=http((_path,init)=>{
  if(init.method==='PUT')throw new TypeError('network interrupted');
  return ++reads===1?original:{...original,name:'Edited draft',version:4,requestedStartAt:savedStart,completionDeadlineAt:savedDeadline};
 });
 render(<CampaignWorkspace campaignId="campaign-a"/>);await edit();fireEvent.click(screen.getByRole('button',{name:'Save draft'}));
 await screen.findByText(matches?/Submitted fields are saved on the server/:/Submitted fields differ from the server/);
 expect(screen.getByLabelText('Draft name')).toHaveProperty('value','Edited draft');
 expect(screen.getByLabelText('Requested start (UTC)')).toHaveProperty('value',start);
 expect(screen.getByLabelText('Completion deadline (UTC)')).toHaveProperty('value',deadline);
 expect(screen.getByLabelText('Save reason')).toHaveProperty('value','Correct the operator draft');
 expect(screen.getByRole('button',{name:'Save draft'})).toHaveProperty('disabled',true);
 expect(calls.filter(c=>c.init.method==='PUT')).toHaveLength(1);
 expect(reads).toBe(2);
});
