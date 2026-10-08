import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { CampaignManager } from '../app/campaigns/campaign-manager';
vi.mock('../components/auth-provider', () => ({ useAuth: () => ({ session: { id: 'operator', sessionId: 'session', permissions: ['campaign.read', 'campaign.write'] } }) }));
afterEach(()=>{cleanup();vi.unstubAllGlobals();});
test('create permits incomplete schedule and exposes the returned campaign link beyond the inventory page',async()=>{
 const calls:Array<{path:string;init:RequestInit}>=[];
 vi.stubGlobal('fetch',async(input:RequestInfo|URL,init:RequestInit={})=>{
  const path=String(input);calls.push({path,init});
  return new Response(JSON.stringify(init.method==='POST'?{id:'returned-campaign',name:'Created',status:'DRAFT'}:{items:[]}),{status:200,headers:{'content-type':'application/json'}});
 });
 render(<CampaignManager/>);
 await screen.findByRole('heading',{name:'Create draft campaign'});
 expect(screen.getByLabelText('Requested start (UTC)')).toHaveProperty('required',false);
 expect(screen.getByLabelText('Completion deadline (UTC)')).toHaveProperty('required',false);
 for(const [label,value]of [['Campaign name','Created'],['Organisation ID','org'],['Consent purpose ID','purpose'],['Approved consent-review ID','review'],['Maximum unique recipients','10'],['Gateway pool ID','gateway'],['Sender pool ID','pool'],['Adapter version','v1'],['Routing policy version','r1'],['Capacity evidence version','c1']]){
  fireEvent.change(screen.getByLabelText(label),{target:{value}});
 }
 fireEvent.submit(screen.getByRole('button',{name:'Create governed draft'}).closest('form')!);
 const link=await screen.findByRole('link',{name:'Open created draft'});
 expect(link.getAttribute('href')).toBe('/campaigns/returned-campaign');
 await waitFor(()=>expect(calls.filter(c=>c.init.method==='POST')).toHaveLength(1));
 const payload=JSON.parse(String(calls.find(c=>c.init.method==='POST')?.init.body));
 expect(payload.requestedStartAt).toBeUndefined();expect(payload.completionDeadlineAt).toBeUndefined();
});

test('creation schedule fields explicitly use UTC instants independently of declared timezone',async()=>{
 const calls:Array<{init:RequestInit}>=[];
 vi.stubGlobal('fetch',async(_input:RequestInfo|URL,init:RequestInit={})=>{calls.push({init});return new Response(JSON.stringify(init.method==='POST'?{id:'created-utc',name:'Created',status:'DRAFT'}:{items:[]}),{status:200,headers:{'content-type':'application/json'}});});
 render(<CampaignManager/>);
 for(const [label,value]of [['Campaign name','Created'],['Organisation ID','org'],['Consent purpose ID','purpose'],['Approved consent-review ID','review'],['Maximum unique recipients','10'],['Gateway pool ID','gateway'],['Sender pool ID','pool'],['Adapter version','v1'],['Routing policy version','r1'],['Capacity evidence version','c1']])fireEvent.change(screen.getByLabelText(label),{target:{value}});
 fireEvent.change(screen.getByLabelText('Requested start (UTC)'),{target:{value:'2026-10-10T12:34'}});
 fireEvent.change(screen.getByLabelText('Campaign timezone'),{target:{value:'Africa/Lagos'}});
 fireEvent.submit(screen.getByRole('button',{name:'Create governed draft'}).closest('form')!);
 await screen.findByRole('link',{name:'Open created draft'});
 const body=JSON.parse(String(calls.find(c=>c.init.method==='POST')?.init.body));
 expect(body.requestedStartAt).toBe('2026-10-10T12:34:00.000Z');expect(body.timezone).toBe('Africa/Lagos');expect(body.completionDeadlineAt).toBeUndefined();
});
