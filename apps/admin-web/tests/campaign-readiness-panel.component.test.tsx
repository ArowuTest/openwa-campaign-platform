import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { CampaignReadinessPanel } from '../app/campaigns/[id]/campaign-readiness-panel';
import type { CampaignDetail } from '../lib/campaign-preparation-model';
import { deferred, settle } from './audience-component-test-helpers';

const auth = vi.hoisted(() => ({ session: { id: 'reader', sessionId: 's1', permissions: ['campaign.read'] } }));
vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));
beforeEach(() => { auth.session = { id: 'reader', sessionId: 's1', permissions: ['campaign.read'] }; });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });
const id = '10000000-0000-4000-8000-000000000001';
function campaign(value=id): CampaignDetail { return { id:value, name:'Preparation', organisationId:'org',purposeId:'purpose',consentReviewId:'review',status:'COMMERCIAL_APPROVED',timezone:'UTC',maximumUniqueRecipients:100,maximumMessagesPerRecipient:1,eligibleAudienceCount:85,createdBy:'maker',createdAt:'2026-10-08T00:00:00Z',updatedAt:'2026-10-08T00:00:00Z',version:9, transport:{channel:'WHATSAPP',provider:'OPENWA',engine:'BAILEYS',routingMode:'SENDER_POOL',senderPoolId:'saved-pool',gatewayPoolId:'saved-gateway',adapterVersion:'1.2.3',fallbackMode:'NONE',routingPolicyVersion:'r1',capacityEvidenceVersion:'c1'} }; }
const keys=['organisation','purpose','consent','audience','message','transport','schedule','capacity','pilot','commercial','maintenance','finalReview'];
function readiness(value=id) { return { campaignId:value,campaignVersion:9,campaignStatus:'COMMERCIAL_APPROVED',assessedAt:'2026-10-08T12:00:00Z',effectiveStartAt:'2026-10-08T13:00:00Z',state:'REQUIRES_REVIEW',readyForFinalReview:false,checks:keys.map(key=>({key,status:key==='commercial'?'PENDING_REVIEW':'PASS',code:key==='commercial'?'COMMERCIAL_APPROVAL_PENDING':'EVIDENCE_VALID',message:key==='commercial'?'Commercial approval is pending.':'Saved evidence verified.',remediation:key==='commercial'?'Obtain independent commercial approval.':'Review saved evidence.',evidence:[]})), capacity:{senderPoolId:'saved-pool',gatewayPoolId:'saved-gateway',healthySessions:2,healthyNodes:2,minimumHealthyNodes:1,availableMessagesPerMinute:10,availableHourlyUnits:600,availableDailyUnits:1700,safetyMarginPercent:15,basisRecipientCount:85,basis:'SNAPSHOT',measurementAsOf:'2026-10-08T12:00:00Z',classification:'ADVISORY_ONLY',reasons:['RESERVATION_WINDOW_FEASIBILITY_NOT_ASSESSED']}, limitations:['Readiness is advisory; final approval revalidates current evidence.','These live capacity figures do not assess competing campaign reservations over the saved window. They do not establish reserved-window or deadline feasibility.'] }; }
function http(fn: (path:string)=>unknown|Promise<unknown>) { const calls:Array<{path:string;method:string}>=[];vi.stubGlobal('fetch',async(input:RequestInfo|URL,init:RequestInit={})=>{calls.push({path:String(input),method:init.method??'GET'});const value=await fn(String(input));return value instanceof Response?value:new Response(JSON.stringify(value),{headers:{'content-type':'application/json'}})});return calls; }
test('refresh is GET only and renders remediation, separate pending commercial and measured advisory capacity',async()=>{
 const calls=http(()=>readiness());render(<CampaignReadinessPanel campaign={campaign()}/>);
 const refresh=screen.getByRole('button',{name:'Refresh readiness'});refresh.focus();expect(document.activeElement).toBe(refresh);fireEvent.click(refresh);
 expect(await screen.findByText('Commercial approval is pending.')).toBeTruthy();expect(screen.getByText('Obtain independent commercial approval.')).toBeTruthy();
 expect(screen.getByText(/2 healthy sessions/)).toBeTruthy();expect(screen.getByText(/Advisory gross capacity/)).toBeTruthy();expect(screen.getByText(/do not assess competing campaign reservations/)).toBeTruthy();
 expect(screen.getAllByText('saved-pool').length).toBeGreaterThan(0);expect(calls).toEqual([{path:'/api/v1/campaigns/'+id+'/readiness',method:'GET'}]);expect(document.activeElement).toBe(refresh);
});
test('an outage is explicit and contains no raw server details',async()=>{
 http(()=>new Response(JSON.stringify({error:'CAMPAIGN_READINESS_UNAVAILABLE',message:'postgres://private secret'}),{status:503}));
 render(<CampaignReadinessPanel campaign={campaign()}/>);fireEvent.click(screen.getByRole('button',{name:'Refresh readiness'}));
 expect(await screen.findByRole('alert')).toBeTruthy();expect(screen.queryByText(/postgres|private secret/)).toBeNull();
});
test.each(['unsafe count','missing evidence','extra field','null capacity','wrong version','array status','ready draft','invalid calendar date','missing reservation limitation'])('malformed response fails closed: %s',async(kind)=>{
 const value=readiness();if(kind==='unsafe count')value.capacity.healthySessions=Number.MAX_SAFE_INTEGER+1;
 if(kind==='missing evidence')delete (value.checks[0] as Partial<typeof value.checks[0]>).evidence;
 if(kind==='extra field')Object.assign(value,{launchToken:'not-allowed'});
 if(kind==='null capacity')Object.assign(value,{capacity:null});
 if(kind==='wrong version')value.campaignVersion=10;
 if(kind==='array status')Object.assign(value.checks[0],{status:['PASS']});
 if(kind==='ready draft'){value.campaignStatus='DRAFT';value.state='READY_FOR_FINAL_REVIEW';value.readyForFinalReview=true;value.checks.forEach(check=>check.status='PASS');}
 if(kind==='invalid calendar date')value.assessedAt='2026-02-31T12:00:00Z';
 if(kind==='missing reservation limitation')value.limitations=value.limitations.slice(0,1);
 const saved=campaign();if(kind==='ready draft')saved.status='DRAFT';
 http(()=>value);render(<CampaignReadinessPanel campaign={saved}/>);fireEvent.click(screen.getByRole('button',{name:'Refresh readiness'}));
 expect(await screen.findByRole('alert')).toBeTruthy();expect(screen.queryByText('Commercial approval is pending.')).toBeNull();
});
test('A/B response, same-ID session switch and permission loss cannot retain stale assessment',async()=>{
 const pending=deferred<unknown>();const calls=http(path=>path.includes(id)?pending.promise:readiness('10000000-0000-4000-8000-000000000002'));
 const view=render(<CampaignReadinessPanel campaign={campaign()}/>);fireEvent.click(screen.getByRole('button',{name:'Refresh readiness'}));await waitFor(()=>expect(calls.length).toBe(1));
 view.rerender(<CampaignReadinessPanel campaign={campaign('10000000-0000-4000-8000-000000000002')}/>);fireEvent.click(screen.getByRole('button',{name:'Refresh readiness'}));expect(await screen.findByText('Commercial approval is pending.')).toBeTruthy();
 await settle(pending,readiness());expect(screen.queryByText(/Assessment for.*000000000001/)).toBeNull();
 auth.session={...auth.session,sessionId:'s2'};view.rerender(<CampaignReadinessPanel campaign={campaign('10000000-0000-4000-8000-000000000002')}/>);expect(screen.queryByText('Commercial approval is pending.')).toBeNull();
 auth.session={...auth.session,permissions:[]};view.rerender(<CampaignReadinessPanel campaign={campaign()}/>);expect(screen.queryByRole('button',{name:'Refresh readiness'})).toBeNull();
});
test('saved version change immediately clears previous assessment',async()=>{
 http(()=>readiness());const view=render(<CampaignReadinessPanel campaign={campaign()}/>);fireEvent.click(screen.getByRole('button',{name:'Refresh readiness'}));await screen.findByText('Commercial approval is pending.');
 view.rerender(<CampaignReadinessPanel campaign={{...campaign(),version:10}}/>);expect(screen.queryByText('Commercial approval is pending.')).toBeNull();
});
